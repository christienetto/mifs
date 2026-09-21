import Foundation

/// The MIFS music server (see server/README.md): full songs with artwork, synced lyrics and
/// precomputed waveforms. Media URLs come from the server; API paths are fixed under /v1.
nonisolated struct MusicServer: Sendable {
    static let shared = MusicServer(baseURL: configuredURL)

    /// `-MIFSServerURL <url>` launch argument, else the Info.plist value set by Config/*.xcconfig.
    static var configuredURL: URL? {
        let value = UserDefaults.standard.string(forKey: "MIFSServerURL")
            ?? Bundle.main.object(forInfoDictionaryKey: "MIFSServerURL") as? String
        guard let value, let url = URL(string: value.trimmingCharacters(in: .whitespaces)),
              url.scheme == "http" || url.scheme == "https" else { return nil }
        return url
    }

    enum Failure: LocalizedError {
        case unreachable(URL)
        case notFound
        case badResponse(Int)
        case serverMessage(String)
        case unsupportedWaveform
        /// No audio source could supply the song.
        case songUnavailable
        case preparationFailed(String)
        /// The song is still being added after polling for a while.
        case stillAdding

        var errorDescription: String? {
            switch self {
            case .unreachable(let url):
                "Couldn't reach the MIFS server at \(url.host(percentEncoded: false) ?? url.absoluteString)\(url.port.map { ":\($0)" } ?? ""). Make sure it's running."
            case .notFound: "This song is no longer on the MIFS server."
            case .serverMessage(let message): message
            case .badResponse: "The MIFS server had a problem. Try again in a moment."
            case .unsupportedWaveform: "The MIFS server sent a waveform this version of MIFS can't read."
            case .songUnavailable: "MIFS can't get this song yet."
            case .preparationFailed(let message): message
            case .stillAdding: "MIFS is still adding this song. Try again in a minute."
            }
        }
    }

    let baseURL: URL?
    var session: URLSession = .shared

    var isConfigured: Bool { baseURL != nil }

    func songs(matching query: String = "", limit: Int = 50) async throws -> [Track] {
        var items = [URLQueryItem(name: "limit", value: String(limit))]
        if !query.isEmpty { items.append(URLQueryItem(name: "q", value: query)) }
        let response: SongList = try await get(["v1", "songs"], query: items)
        return response.songs.compactMap(\.track)
    }

    /// MIFS's songs plus the server's discovery providers (Spotify, Deezer, …), each marked
    /// with whether MIFS has it yet.
    func search(_ query: String, limit: Int = 25) async throws -> [ServerSearchResult] {
        let response: SearchResponse = try await get(["v1", "search"], query: [
            URLQueryItem(name: "q", value: query), URLQueryItem(name: "limit", value: String(limit)),
        ])
        return response.results
    }

    func prepare(ref: String) async throws -> SongList.Song {
        try await send(["v1", "songs"], body: ["ref": ref])
    }
    func song(id: String) async throws -> SongList.Song {
        try await get(["v1", "songs", id], fresh: true)
    }

    /// Makes MIFS have a search result's song, waiting while the server adds it (usually a
    /// few seconds: it fetches the audio, lyrics and artwork).
    func ready(_ result: ServerSearchResult) async throws -> Track {
        if let track = result.song?.track { return track }
        var song: SongList.Song = try await send(["v1", "songs"], body: ["ref": result.ref])
        let deadline = ContinuousClock.now + .seconds(120)
        while true {
            if let track = song.track { return track }
            if song.status == "unavailable" || song.status == "failed" {
                throw Failure.preparationFailed(song.statusMessage ?? "MIFS can't get this song yet.")
            }
            guard ContinuousClock.now < deadline else { throw Failure.stillAdding }
            try await Task.sleep(for: .seconds(1))
            song = try await get(["v1", "songs", song.id], fresh: true)
        }
    }

    /// Creates the mif for a moment of a server song and returns its share link, which plays
    /// on any device.
    func mif(songID: String, start: TimeInterval, duration: TimeInterval) async throws -> URL {
        let response: MifResponse = try await send(["v1", "songs", songID, "mifs"], body: [
            "startMs": Int((start * 1000).rounded()), "durationMs": Int((duration * 1000).rounded()),
        ])
        return response.url
    }

    func received(_ url: URL) async throws -> Snippet {
        guard let baseURL, url.scheme == baseURL.scheme, url.host() == baseURL.host(),
              url.port == baseURL.port, url.pathComponents.count == 3,
              url.pathComponents[1] == "m" else { throw Failure.notFound }
        let response: ReceivedMif = try await get(["v1", "mifs", url.lastPathComponent])
        guard let track = response.song.track else { throw Failure.songUnavailable }
        return Snippet(track: track, start: Double(response.startMs) / 1000,
            duration: Double(response.durationMs) / 1000,
            waveform: Array(repeating: 0.4, count: Snippet.waveformBars),
            lyrics: response.lyrics.map { LyricLine(start: Double($0.startMs) / 1000, end: Double($0.endMs) / 1000, text: $0.text) },
            shareURL: response.url)
    }

    /// Synced lyrics, or none when the song is instrumental.
    func lyrics(for songID: String) async throws -> [LyricLine] {
        do {
            let response: LyricsResponse = try await get(["v1", "songs", songID, "lyrics"])
            return response.lines.map {
                LyricLine(start: TimeInterval($0.startMs) / 1000, end: TimeInterval($0.endMs) / 1000, text: $0.text)
            }
        } catch Failure.notFound {
            return []
        }
    }

    /// The same envelope `WaveformAnalyzer` would compute, without downloading the song.
    func waveform(for songID: String) async throws -> Waveform {
        let response: WaveformResponse = try await get(["v1", "songs", songID, "waveform"])
        guard Double(response.pointsPerSecond) == Waveform.resolution, !response.rms.isEmpty else {
            throw Failure.unsupportedWaveform
        }
        return Waveform(
            levels: WaveformAnalyzer.contrast(response.rms),
            duration: TimeInterval(response.durationMs) / 1000,
            rms: response.rms
        )
    }

    // MARK: - Networking

    private func get<T: Decodable>(_ path: [String], query: [URLQueryItem] = [], fresh: Bool = false) async throws -> T {
        guard let baseURL else { throw URLError(.badURL) }
        var url = path.reduce(baseURL) { $0.appending(path: $1) }
        if !query.isEmpty { url.append(queryItems: query) }

        var request = URLRequest(url: url)
        // The server sends ETags with `no-cache`, so repeat requests are cheap 304s.
        request.cachePolicy = fresh ? .reloadIgnoringLocalCacheData : .useProtocolCachePolicy
        request.timeoutInterval = 10
        return try await perform(request)
    }

    private func send<T: Decodable>(_ path: [String], body: [String: any Sendable]) async throws -> T {
        guard let baseURL else { throw URLError(.badURL) }
        var request = URLRequest(url: path.reduce(baseURL) { $0.appending(path: $1) })
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        request.timeoutInterval = 20
        return try await perform(request)
    }

    private func perform<T: Decodable>(_ request: URLRequest) async throws -> T {
        guard let baseURL else { throw URLError(.badURL) }
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch let error as URLError where Self.unreachableCodes.contains(error.code) {
            throw Failure.unreachable(baseURL)
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if status == 404 { throw Failure.notFound }
        guard (200..<300).contains(status) else {
            if let failure = try? JSONDecoder().decode(ServerErrorResponse.self, from: data) {
                throw Failure.serverMessage(failure.error.message)
            }
            throw Failure.badResponse(status)
        }
        return try JSONDecoder().decode(T.self, from: data)
    }

    private static let unreachableCodes: Set<URLError.Code> = [
        .cannotConnectToHost, .cannotFindHost, .timedOut, .networkConnectionLost,
        .notConnectedToInternet, .dnsLookupFailed, .appTransportSecurityRequiresSecureConnection,
    ]
}

// MARK: - Wire format (server/internal/api)

nonisolated struct SongList: Decodable {
    let songs: [Song]

    struct Song: Decodable, Sendable {
        struct Audio: Decodable, Sendable { let url: URL }
        struct Artwork: Decodable, Sendable {
            let url: URL
            let thumbnailUrl: URL
        }

        let id: String
        /// "ready" once the song can be played; songs being added have no audio yet.
        let status: String?
        var statusMessage: String? = nil
        var downloadedBytes: Int64? = nil
        var downloadTotalBytes: Int64? = nil
        let title: String
        let artist: String
        let album: String?
        let explicit: Bool
        let highlightStartMs: Int?
        let audio: Audio?
        let artwork: Artwork?

        /// The playable track, once the song is ready.
        var track: Track? {
            guard let audio, status == nil || status == "ready" else { return nil }
            return Track(
                id: id,
                kind: .server,
                title: title,
                artist: artist,
                album: album,
                artworkURL: artwork?.url,
                previewURL: audio.url,
                isExplicit: explicit,
                thumbnailURL: artwork?.thumbnailUrl,
                highlightStart: highlightStartMs.map { TimeInterval($0) / 1000 }
            )
        }
    }
}

/// A search result: a song MIFS has, or a provider's track it can add (`status` "new").
nonisolated struct ServerSearchResult: Decodable, Identifiable, Sendable {
    /// What to pass back to add the song, e.g. "spotify:0VjIjW4GlUZAMYd2vXMi3b".
    let ref: String
    var previewUrl: URL? = nil
    var durationMs: Int? = nil
    let songId: String?
    /// "ready", "new", "pending", "processing", "unavailable" or "failed".
    let status: String
    /// Where it was found: "mifs", "spotify", "deezer", …
    let source: String
    let title: String
    let artist: String
    let album: String?
    let explicit: Bool
    let artwork: SongList.Song.Artwork?
    /// The playable song, when `status` is "ready".
    let song: SongList.Song?

    var id: String { ref }
    var isReady: Bool { song?.track != nil }
    var isUnavailable: Bool { status == "unavailable" || status == "failed" }

    var sourceName: String {
        switch source {
        case "mifs": "MIFS"
        case "spotify": "Spotify"
        case "deezer": "Deezer"
        default: source.capitalized
        }
    }
}

private nonisolated struct SearchResponse: Decodable {
    let results: [ServerSearchResult]
}

private nonisolated struct MifResponse: Decodable {
    let url: URL
}

private nonisolated struct LyricsResponse: Decodable {
    struct Line: Decodable {
        let startMs: Int
        let endMs: Int
        let text: String
    }

    let lines: [Line]
}

private nonisolated struct WaveformResponse: Decodable {
    let durationMs: Int
    let pointsPerSecond: Int
    let rms: [Float]
}

private nonisolated struct ReceivedMif: Decodable {
    let url: URL
    let song: SongList.Song
    let startMs: Int
    let durationMs: Int
    let lyrics: [LyricsResponse.Line]
}

private nonisolated struct ServerErrorResponse: Decodable {
    struct Detail: Decodable { let message: String }
    let error: Detail
}

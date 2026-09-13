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
        case unsupportedWaveform

        var errorDescription: String? {
            switch self {
            case .unreachable(let url):
                "Couldn't reach the MIFS server at \(url.host(percentEncoded: false) ?? url.absoluteString)\(url.port.map { ":\($0)" } ?? ""). Make sure it's running."
            case .notFound: "This song is no longer on the MIFS server."
            case .badResponse: "The MIFS server had a problem. Try again in a moment."
            case .unsupportedWaveform: "The MIFS server sent a waveform this version of MIFS can't read."
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
        return response.songs.map(\.track)
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

    private func get<T: Decodable>(_ path: [String], query: [URLQueryItem] = []) async throws -> T {
        guard let baseURL else { throw URLError(.badURL) }
        var url = path.reduce(baseURL) { $0.appending(path: $1) }
        if !query.isEmpty { url.append(queryItems: query) }

        var request = URLRequest(url: url)
        // The server sends ETags with `no-cache`, so repeat requests are cheap 304s.
        request.cachePolicy = .useProtocolCachePolicy
        request.timeoutInterval = 10
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch let error as URLError where Self.unreachableCodes.contains(error.code) {
            throw Failure.unreachable(baseURL)
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if status == 404 { throw Failure.notFound }
        guard (200..<300).contains(status) else { throw Failure.badResponse(status) }
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

    struct Song: Decodable {
        struct Audio: Decodable { let url: URL }
        struct Artwork: Decodable {
            let url: URL
            let thumbnailUrl: URL
        }

        let id: String
        let title: String
        let artist: String
        let album: String?
        let explicit: Bool
        let highlightStartMs: Int?
        let audio: Audio
        let artwork: Artwork?

        var track: Track {
            Track(
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

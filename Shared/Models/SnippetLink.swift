import Foundation

/// Encodes a streamable (catalog or MIFS server) snippet into the URL an `MSMessage` carries.
///
/// The base is the song's music.apple.com page — or, for MIFS server songs, the song's audio —
/// so devices without MIFS (e.g. a Mac) can still open the song. MIFS parameters ride along
/// as `mifs_*` query items.
nonisolated enum SnippetLink {
    private enum Key {
        static let version = "mifs"
        static let id = "mifs_id"
        static let kind = "mifs_k"
        static let start = "mifs_s"
        static let duration = "mifs_d"
        static let title = "mifs_t"
        static let artist = "mifs_a"
        static let artwork = "mifs_art"
        static let preview = "mifs_p"
        static let waveform = "mifs_w"
        static let explicit = "mifs_e"
        static let lyrics = "mifs_l"
    }

    /// Keeps the payload small whatever the snippet spans.
    private static let maxLyricLines = 6

    static func url(for snippet: Snippet) -> URL? {
        let track = snippet.track
        guard track.kind != .file, let preview = track.previewURL else { return nil }

        var components: URLComponents
        var items: [URLQueryItem]
        if track.kind == .server {
            components = URLComponents(url: preview, resolvingAgainstBaseURL: false)!
            items = [URLQueryItem(name: Key.kind, value: track.kind.rawValue)]
        } else {
            components = track.appleMusicURL.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false) }
                ?? URLComponents(string: "https://music.apple.com/search")!
            items = (components.queryItems ?? []).filter { $0.name != "uo" && !$0.name.hasPrefix("mifs") }
            if track.appleMusicURL == nil {
                items.append(URLQueryItem(name: "term", value: "\(track.title) \(track.artist)"))
            }
        }
        items += [
            URLQueryItem(name: Key.version, value: "1"),
            URLQueryItem(name: Key.id, value: track.id),
            URLQueryItem(name: Key.start, value: String(Int((snippet.start * 1000).rounded()))),
            URLQueryItem(name: Key.duration, value: String(Int((snippet.duration * 1000).rounded()))),
            URLQueryItem(name: Key.title, value: track.title),
            URLQueryItem(name: Key.artist, value: track.artist),
            URLQueryItem(name: Key.preview, value: preview.absoluteString),
            URLQueryItem(name: Key.waveform, value: encode(snippet.waveform)),
        ]
        if let artwork = track.artworkURL {
            items.append(URLQueryItem(name: Key.artwork, value: artwork.absoluteString))
        }
        if track.isExplicit {
            items.append(URLQueryItem(name: Key.explicit, value: "1"))
        }
        if let lyrics = snippet.lyrics, !lyrics.isEmpty {
            items.append(URLQueryItem(name: Key.lyrics, value: encode(Array(lyrics.prefix(maxLyricLines)))))
        }
        components.queryItems = items
        return components.url
    }

    static func snippet(from url: URL) -> Snippet? {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let items = components.queryItems else { return nil }
        let values = Dictionary(items.compactMap { item in item.value.map { (item.name, $0) } },
                                uniquingKeysWith: { first, _ in first })
        guard values[Key.version] != nil,
              let id = values[Key.id],
              let startMs = values[Key.start].flatMap(Int.init),
              let durationMs = values[Key.duration].flatMap(Int.init),
              let title = values[Key.title],
              let preview = values[Key.preview].flatMap(URL.init(string:)),
              durationMs > 0 else { return nil }

        let kind: Track.Kind = values[Key.kind] == Track.Kind.server.rawValue ? .server : .catalog
        var appleMusic = components
        appleMusic.queryItems = items.filter { !$0.name.hasPrefix("mifs") }
        if appleMusic.queryItems?.isEmpty == true { appleMusic.queryItems = nil }

        let track = Track(
            id: id,
            kind: kind,
            title: title,
            artist: values[Key.artist] ?? "",
            artworkURL: values[Key.artwork].flatMap(URL.init(string:)),
            previewURL: preview,
            appleMusicURL: kind == .catalog ? appleMusic.url : nil,
            isExplicit: values[Key.explicit] == "1"
        )
        let lyrics = decodeLyrics(values[Key.lyrics] ?? "")
        return Snippet(
            track: track,
            start: TimeInterval(startMs) / 1000,
            duration: TimeInterval(durationMs) / 1000,
            waveform: decode(values[Key.waveform] ?? ""),
            lyrics: lyrics.isEmpty ? nil : lyrics
        )
    }

    /// One line per lyric: "<startMs>-<endMs> <text>".
    static func encode(_ lyrics: [LyricLine]) -> String {
        lyrics.map { line in
            "\(Int((line.start * 1000).rounded()))-\(Int((line.end * 1000).rounded())) "
                + line.text.replacingOccurrences(of: "\n", with: " ")
        }
        .joined(separator: "\n")
    }

    static func decodeLyrics(_ string: String) -> [LyricLine] {
        string.split(separator: "\n").compactMap { entry in
            let parts = entry.split(separator: " ", maxSplits: 1)
            let times = parts.first?.split(separator: "-").compactMap { Int($0) } ?? []
            guard parts.count == 2, times.count == 2 else { return nil }
            return LyricLine(start: TimeInterval(times[0]) / 1000, end: TimeInterval(times[1]) / 1000, text: String(parts[1]))
        }
    }

    /// One hex digit per bar keeps the payload tiny (40 chars for 40 bars).
    static func encode(_ levels: [Float]) -> String {
        levels.map { String(Int(($0.clamped(to: 0...1) * 15).rounded()), radix: 16) }.joined()
    }

    static func decode(_ string: String) -> [Float] {
        string.compactMap { Int(String($0), radix: 16) }.map { Float($0) / 15 }
    }
}

extension Comparable {
    nonisolated func clamped(to range: ClosedRange<Self>) -> Self {
        min(max(self, range.lowerBound), range.upperBound)
    }
}

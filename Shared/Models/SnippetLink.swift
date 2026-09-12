import Foundation

/// Encodes a catalog snippet into the URL an `MSMessage` carries.
///
/// The base is the song's music.apple.com page, so devices without MIFS (e.g. a Mac)
/// open the song in Apple Music. MIFS parameters ride along as `mifs_*` query items.
nonisolated enum SnippetLink {
    private enum Key {
        static let version = "mifs"
        static let id = "mifs_id"
        static let start = "mifs_s"
        static let duration = "mifs_d"
        static let title = "mifs_t"
        static let artist = "mifs_a"
        static let artwork = "mifs_art"
        static let preview = "mifs_p"
        static let waveform = "mifs_w"
        static let explicit = "mifs_e"
    }

    static func url(for snippet: Snippet) -> URL? {
        let track = snippet.track
        guard track.kind == .catalog, let preview = track.previewURL else { return nil }

        var components = track.appleMusicURL.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false) }
            ?? URLComponents(string: "https://music.apple.com/search")!
        var items = (components.queryItems ?? []).filter { $0.name != "uo" && !$0.name.hasPrefix("mifs") }
        if track.appleMusicURL == nil {
            items.append(URLQueryItem(name: "term", value: "\(track.title) \(track.artist)"))
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

        var appleMusic = components
        appleMusic.queryItems = items.filter { !$0.name.hasPrefix("mifs") }
        if appleMusic.queryItems?.isEmpty == true { appleMusic.queryItems = nil }

        let track = Track(
            id: id,
            kind: .catalog,
            title: title,
            artist: values[Key.artist] ?? "",
            artworkURL: values[Key.artwork].flatMap(URL.init(string:)),
            previewURL: preview,
            appleMusicURL: appleMusic.url,
            isExplicit: values[Key.explicit] == "1"
        )
        return Snippet(
            track: track,
            start: TimeInterval(startMs) / 1000,
            duration: TimeInterval(durationMs) / 1000,
            waveform: decode(values[Key.waveform] ?? "")
        )
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

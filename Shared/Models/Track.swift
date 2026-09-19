import Foundation

/// Something the user can clip: a catalog song (Apple's 30s preview), a full song from the
/// MIFS music server, or audio they own.
nonisolated struct Track: Identifiable, Hashable, Codable, Sendable {
    enum Kind: String, Codable, Sendable {
        case catalog
        case file
        /// Streamed from the MIFS music server, with synced lyrics.
        case server
    }

    var id: String
    var kind: Kind
    var title: String
    var artist: String
    var album: String?
    var artworkURL: URL?
    /// App-group relative path of locally stored artwork (owned audio).
    var artworkFile: String?
    /// Streamable audio: Apple's 30 s preview, or the full song on the MIFS server.
    var previewURL: URL?
    var appleMusicURL: URL?
    var isExplicit: Bool = false
    /// Location of owned audio for the current editing session. Not meaningful across launches.
    var sourceURL: URL?
    /// Small artwork for lists, when the source provides one.
    var thumbnailURL: URL?
    /// Suggested snippet start (e.g. the chorus), when the source provides one.
    var highlightStart: TimeInterval?

    /// Spotify identity to prepare when this selection opens.
    var preparationRef: String?
    var expectedDuration: TimeInterval?

    var resolvedArtworkURL: URL? {
        if let artworkFile { return AppGroup.url(for: artworkFile) }
        return artworkURL
    }

    var spotifySearchURL: URL? {
        var components = URLComponents(string: "https://open.spotify.com/search")!
        let term = "\(title) \(artist)".addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? ""
        components.percentEncodedPath += "/" + term
        return components.url
    }
}

extension Track {
    /// Apple artwork URLs embed their size (".../100x100bb.jpg"); ask for a sharper one.
    nonisolated static func artwork(_ url: URL?, size: Int) -> URL? {
        guard let url else { return nil }
        let string = url.absoluteString.replacingOccurrences(
            of: #"/\d+x\d+bb\."#,
            with: "/\(size)x\(size)bb.",
            options: .regularExpression
        )
        return URL(string: string)
    }
}

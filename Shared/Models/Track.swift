import Foundation

/// Something the user can clip: a catalog song (Apple's 30s preview) or audio they own.
nonisolated struct Track: Identifiable, Hashable, Codable, Sendable {
    enum Kind: String, Codable, Sendable {
        case catalog
        case file
    }

    var id: String
    var kind: Kind
    var title: String
    var artist: String
    var album: String?
    var artworkURL: URL?
    /// App-group relative path of locally stored artwork (owned audio).
    var artworkFile: String?
    var previewURL: URL?
    var appleMusicURL: URL?
    var isExplicit: Bool = false
    /// Location of owned audio for the current editing session. Not meaningful across launches.
    var sourceURL: URL?

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

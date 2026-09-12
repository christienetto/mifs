import Foundation

/// Apple's public catalog: iTunes Search/Lookup for songs + previews, Apple Music RSS for charts.
/// No developer token, user account, or subscription is required.
nonisolated struct CatalogService: Sendable {
    static let shared = CatalogService()

    enum Failure: LocalizedError {
        case badResponse(Int)
        case rateLimited

        var errorDescription: String? {
            switch self {
            case .rateLimited: "Apple Music is busy right now. Try again in a moment."
            case .badResponse: "Couldn't reach Apple Music. Check your connection and try again."
            }
        }
    }

    var session: URLSession = .shared

    var storefront: String {
        (Locale.current.region?.identifier ?? "US").lowercased()
    }

    func search(_ term: String, limit: Int = 40) async throws -> [Track] {
        var components = URLComponents(string: "https://itunes.apple.com/search")!
        components.queryItems = [
            .init(name: "term", value: term),
            .init(name: "media", value: "music"),
            .init(name: "entity", value: "song"),
            .init(name: "limit", value: String(limit)),
            .init(name: "country", value: storefront),
        ]
        return try await fetchTracks(components.url!)
    }

    func lookup(ids: [String]) async throws -> [Track] {
        guard !ids.isEmpty else { return [] }
        var components = URLComponents(string: "https://itunes.apple.com/lookup")!
        components.queryItems = [
            .init(name: "id", value: ids.joined(separator: ",")),
            .init(name: "entity", value: "song"),
            .init(name: "country", value: storefront),
        ]
        let byID = Dictionary(try await fetchTracks(components.url!).map { ($0.id, $0) },
                              uniquingKeysWith: { first, _ in first })
        return ids.compactMap { byID[$0] }
    }

    func topSongs(limit: Int = 50) async throws -> [Track] {
        let url = URL(string: "https://rss.marketingtools.apple.com/api/v2/\(storefront)/music/most-played/\(limit)/songs.json")!
        let feed: ChartFeed = try await get(url)
        return try await lookup(ids: feed.feed.results.map(\.id))
    }

    /// Best catalog equivalent for a song the user owns but can't be read (DRM).
    func bestMatch(title: String, artist: String) async throws -> Track? {
        let results = try await search("\(title) \(artist)", limit: 10)
        let wanted = title.lowercased()
        return results.first { $0.title.lowercased() == wanted } ?? results.first
    }

    // MARK: - Networking

    private func fetchTracks(_ url: URL) async throws -> [Track] {
        let response: SearchResponse = try await get(url)
        return response.results.compactMap(\.track)
    }

    private func get<T: Decodable>(_ url: URL) async throws -> T {
        var request = URLRequest(url: url)
        request.cachePolicy = .useProtocolCachePolicy
        request.timeoutInterval = 15
        let (data, response) = try await session.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if status == 403 || status == 429 { throw Failure.rateLimited }
        guard (200..<300).contains(status) else { throw Failure.badResponse(status) }
        return try JSONDecoder().decode(T.self, from: data)
    }
}

private nonisolated struct SearchResponse: Decodable {
    let results: [Item]

    struct Item: Decodable {
        let wrapperType: String?
        let kind: String?
        let trackId: Int?
        let trackName: String?
        let artistName: String?
        let collectionName: String?
        let artworkUrl100: URL?
        let previewUrl: URL?
        let trackViewUrl: URL?
        let trackExplicitness: String?

        var track: Track? {
            guard kind == "song", let trackId, let trackName, let previewUrl else { return nil }
            return Track(
                id: String(trackId),
                kind: .catalog,
                title: trackName,
                artist: artistName ?? "",
                album: collectionName,
                artworkURL: Track.artwork(artworkUrl100, size: 600),
                previewURL: previewUrl,
                appleMusicURL: trackViewUrl,
                isExplicit: trackExplicitness == "explicit"
            )
        }
    }
}

private nonisolated struct ChartFeed: Decodable {
    struct Feed: Decodable { let results: [Entry] }
    struct Entry: Decodable { let id: String }
    let feed: Feed
}

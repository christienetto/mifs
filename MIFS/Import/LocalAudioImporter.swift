import AVFoundation
import MediaPlayer
import UIKit

/// Turns audio the user owns (Files or the Music library) into an editable `Track`.
enum LocalAudioImporter {
    enum Outcome {
        case track(Track)
        /// The library song is DRM protected; its catalog equivalent (preview) is offered instead.
        case catalogFallback(Track)
    }

    enum Failure: LocalizedError {
        case unreadable
        case notInCatalog

        var errorDescription: String? {
            switch self {
            case .unreadable: "MIFS couldn't read that file."
            case .notInCatalog: "This song is protected by Apple Music and couldn't be found in the catalog."
            }
        }
    }

    static func importFile(at url: URL) async throws -> Track {
        let accessing = url.startAccessingSecurityScopedResource()
        defer { if accessing { url.stopAccessingSecurityScopedResource() } }

        // Only one import is edited at a time; drop earlier full-length copies.
        let folder = URL.temporaryDirectory.appending(path: "Imports", directoryHint: .isDirectory)
        try? FileManager.default.removeItem(at: folder)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let local = folder.appending(path: "\(UUID().uuidString).\(url.pathExtension.isEmpty ? "m4a" : url.pathExtension)")
        do {
            try FileManager.default.copyItem(at: url, to: local)
        } catch {
            throw Failure.unreadable
        }

        let metadata = await Metadata.load(from: local)
        return Track(
            id: UUID().uuidString,
            kind: .file,
            title: metadata.title ?? url.deletingPathExtension().lastPathComponent,
            artist: metadata.artist ?? "Unknown Artist",
            album: metadata.album,
            artworkFile: metadata.artwork.flatMap(saveArtwork),
            sourceURL: local
        )
    }

    static func resolve(_ item: MPMediaItem) async throws -> Outcome {
        let title = item.title ?? "Untitled"
        let artist = item.artist ?? "Unknown Artist"
        if let assetURL = item.assetURL, !item.hasProtectedAsset {
            let artwork = item.artwork?.image(at: CGSize(width: 600, height: 600))
            return .track(Track(
                id: "library-\(item.persistentID)",
                kind: .file,
                title: title,
                artist: artist,
                album: item.albumTitle,
                artworkFile: artwork.flatMap(saveArtwork),
                sourceURL: assetURL
            ))
        }
        guard let match = try await CatalogService.shared.bestMatch(title: title, artist: artist) else {
            throw Failure.notInCatalog
        }
        return .catalogFallback(match)
    }

    private static func saveArtwork(_ image: UIImage) -> String? {
        let side: CGFloat = 600
        let scale = min(1, side / max(image.size.width, image.size.height))
        let size = CGSize(width: image.size.width * scale, height: image.size.height * scale)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let resized = UIGraphicsImageRenderer(size: size, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: size))
        }
        guard let data = resized.jpegData(compressionQuality: 0.85),
              let relative = try? AppGroup.newFile(in: "Artwork", extension: "jpg") else { return nil }
        do {
            try data.write(to: AppGroup.url(for: relative), options: .atomic)
            return relative
        } catch {
            return nil
        }
    }
}

private nonisolated struct Metadata: Sendable {
    var title: String?
    var artist: String?
    var album: String?
    var artworkData: Data?

    @MainActor var artwork: UIImage? { artworkData.flatMap(UIImage.init(data:)) }

    @concurrent
    static func load(from url: URL) async -> Metadata {
        let asset = AVURLAsset(url: url)
        guard let items = try? await asset.load(.commonMetadata) else { return Metadata() }

        func string(_ identifier: AVMetadataIdentifier) async -> String? {
            guard let item = AVMetadataItem.metadataItems(from: items, filteredByIdentifier: identifier).first else { return nil }
            let value = try? await item.load(.stringValue)
            return value?.isEmpty == false ? value : nil
        }

        var metadata = Metadata()
        metadata.title = await string(.commonIdentifierTitle)
        metadata.artist = await string(.commonIdentifierArtist)
        metadata.album = await string(.commonIdentifierAlbumName)
        if let artwork = AVMetadataItem.metadataItems(from: items, filteredByIdentifier: .commonIdentifierArtwork).first {
            metadata.artworkData = try? await artwork.load(.dataValue)
        }
        return metadata
    }
}

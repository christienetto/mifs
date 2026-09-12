import Foundation
import Observation
import OSLog

/// Snippet history shared between the app and the Messages extension (JSON in the app group).
@Observable
final class SnippetStore {
    static let shared = SnippetStore()
    static let limit = 200

    private(set) var snippets: [Snippet] = []

    private let fileURL: URL
    private let coordinator = NSFileCoordinator()

    init(fileURL: URL = AppGroup.url(for: "snippets.json")) {
        self.fileURL = fileURL
        reload()
    }

    /// Re-reads from disk; the other process may have written since.
    func reload() {
        var loaded: [Snippet] = []
        var coordinationError: NSError?
        coordinator.coordinate(readingItemAt: fileURL, options: [], error: &coordinationError) { url in
            guard let data = try? Data(contentsOf: url) else { return }
            do {
                loaded = try JSONDecoder().decode([Snippet].self, from: data)
            } catch {
                Logger.mifs.error("Could not decode snippet history: \(error.localizedDescription)")
            }
        }
        snippets = loaded.sorted { $0.createdAt > $1.createdAt }
    }

    func add(_ snippet: Snippet) {
        reload()
        snippets.removeAll { $0.id == snippet.id }
        snippets.insert(snippet, at: 0)
        let overflow = Array(snippets.dropFirst(Self.limit))
        snippets.removeLast(overflow.count)
        overflow.forEach(removeFiles)
        save()
    }

    func delete(_ snippet: Snippet) {
        reload()
        snippets.removeAll { $0.id == snippet.id }
        removeFiles(of: snippet)
        save()
    }

    private func removeFiles(of snippet: Snippet) {
        let stillReferenced = snippets.contains { $0.track.artworkFile == snippet.track.artworkFile }
        if let clip = snippet.clipURL { try? FileManager.default.removeItem(at: clip) }
        if let artwork = snippet.track.artworkFile, !stillReferenced {
            try? FileManager.default.removeItem(at: AppGroup.url(for: artwork))
        }
    }

    private func save() {
        let snippets = snippets
        var coordinationError: NSError?
        coordinator.coordinate(writingItemAt: fileURL, options: .forReplacing, error: &coordinationError) { url in
            do {
                let data = try JSONEncoder().encode(snippets)
                try data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
            } catch {
                Logger.mifs.error("Could not save snippet history: \(error.localizedDescription)")
            }
        }
    }
}

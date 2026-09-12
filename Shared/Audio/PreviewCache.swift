import CryptoKit
import Foundation

/// Downloads catalog previews to Caches so they can be analysed and scrubbed instantly.
nonisolated enum PreviewCache {
    private static let directory: URL = {
        let url = URL.cachesDirectory.appending(path: "Previews", directoryHint: .isDirectory)
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }()

    @concurrent
    static func localFile(for remote: URL) async throws -> URL {
        if remote.isFileURL { return remote }
        let digest = SHA256.hash(data: Data(remote.absoluteString.utf8))
        let name = digest.prefix(16).map { String(format: "%02x", $0) }.joined()
        let destination = directory.appending(path: "\(name).\(remote.pathExtension.isEmpty ? "m4a" : remote.pathExtension)")
        if FileManager.default.fileExists(atPath: destination.path) { return destination }

        let (temporary, response) = try await URLSession.shared.download(from: remote)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            throw URLError(.badServerResponse)
        }
        try? FileManager.default.removeItem(at: destination)
        try FileManager.default.moveItem(at: temporary, to: destination)
        return destination
    }
}

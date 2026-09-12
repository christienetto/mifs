import Foundation
import OSLog

/// Shared container used by both the app and the Messages extension.
nonisolated enum AppGroup {
    static let identifier = Bundle.main.object(forInfoDictionaryKey: "MIFSAppGroup") as? String ?? ""

    static let containerURL: URL = {
        if let url = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: identifier) {
            return url
        }
        Logger.mifs.fault("App group \(identifier, privacy: .public) unavailable; falling back to a private container")
        return URL.applicationSupportDirectory
    }()

    static func url(for relativePath: String) -> URL {
        containerURL.appending(path: relativePath, directoryHint: .notDirectory)
    }

    /// Creates `directory` inside the container and returns a relative path for a new file in it.
    static func newFile(in directory: String, extension ext: String) throws -> String {
        let folder = containerURL.appending(path: directory, directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return "\(directory)/\(UUID().uuidString).\(ext)"
    }
}

extension Logger {
    nonisolated static let mifs = Logger(subsystem: "dev.kuchta.mifs", category: "MIFS")
}

import Foundation

nonisolated struct Snippet: Identifiable, Hashable, Codable, Sendable {
    static let lengthRange: ClosedRange<TimeInterval> = 5...15
    static let waveformBars = 40

    var id: UUID = UUID()
    var track: Track
    var start: TimeInterval
    var duration: TimeInterval
    /// Normalised 0...1 levels covering just the snippet, `waveformBars` long.
    var waveform: [Float]
    var createdAt: Date = .now
    /// App-group relative path of the exported clip (owned audio only).
    var clipFile: String?
    /// Lyric lines heard in the snippet, in song time (MIFS server songs).
    var lyrics: [LyricLine]?

    var end: TimeInterval { start + duration }

    /// Where playback reads from and the range inside that file.
    var playback: (url: URL, start: TimeInterval, duration: TimeInterval)? {
        switch track.kind {
        case .catalog, .server:
            guard let url = track.previewURL else { return nil }
            return (url, start, duration)
        case .file:
            guard let clipFile else { return nil }
            return (AppGroup.url(for: clipFile), 0, duration)
        }
    }

    var clipURL: URL? { clipFile.map(AppGroup.url(for:)) }

    var attachmentName: String {
        let base = "\(track.title) – \(track.artist)"
            .replacingOccurrences(of: "/", with: "-")
            .replacingOccurrences(of: ":", with: "-")
        return base + ".m4a"
    }
}

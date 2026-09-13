import Foundation

/// One line of synced lyrics. Times are seconds into the song.
nonisolated struct LyricLine: Hashable, Codable, Sendable {
    var start: TimeInterval
    var end: TimeInterval
    var text: String
}

extension [LyricLine] {
    /// Lines heard during `start..<end`: at least half a second of them (or half of a short line).
    nonisolated func heard(from start: TimeInterval, to end: TimeInterval) -> [LyricLine] {
        filter { line in
            let overlap = Swift.min(end, line.end) - Swift.max(start, line.start)
            return overlap > 0 && overlap >= Swift.min(0.5, (line.end - line.start) / 2)
        }
    }

    /// Index of the line being sung at `time`.
    nonisolated func index(at time: TimeInterval) -> Int? {
        lastIndex { $0.start <= time && time < $0.end }
    }
}

import AVFoundation
import Accelerate

/// Loudness envelope of an audio asset, used to draw waveforms and pick a default moment.
nonisolated struct Waveform: Sendable, Equatable {
    /// Envelope points per second of audio.
    static let resolution: Double = 10

    /// Normalised 0...1 display levels, `resolution` per second.
    var levels: [Float]
    var duration: TimeInterval
    /// Raw RMS per point; drives the loudness-based suggestion. Falls back to `levels`.
    var rms: [Float]? = nil

    func levels(from start: TimeInterval, duration length: TimeInterval, bars: Int) -> [Float] {
        guard !levels.isEmpty, bars > 0 else { return Array(repeating: 0.1, count: bars) }
        let first = Int(start * Self.resolution)
        let last = max(first + 1, Int((start + length) * Self.resolution))
        let slice = Array(levels[first.clamped(to: 0...levels.count - 1)..<last.clamped(to: 1...levels.count)])
        guard !slice.isEmpty else { return Array(repeating: 0.1, count: bars) }
        let resampled = (0..<bars).map { bar -> Float in
            let lower = bar * slice.count / bars
            let upper = max(lower + 1, (bar + 1) * slice.count / bars)
            return slice[lower..<min(upper, slice.count)].max() ?? 0
        }
        let peak = resampled.max() ?? 1
        return resampled.map { peak > 0 ? max(0.08, $0 / peak) : 0.08 }
    }

    /// Match a preview's loudness envelope to the recording, allowing volume differences.
    /// Reject uncertain or repeated matches rather than silently sharing the wrong verse.
    func offset(of preview: Waveform) -> TimeInterval? {
        let full = rms ?? levels, sample = preview.rms ?? preview.levels
        let trim = 10 // ignore provider fades at both ends
        guard sample.count > 60, full.count >= sample.count else { return nil }
        let needle = Array(sample[trim..<(sample.count - trim)]).map(Double.init)
        let mean = needle.reduce(0, +) / Double(needle.count)
        let centered = needle.map { $0 - mean }
        let energy = centered.reduce(0) { $0 + $1 * $1 }
        guard energy > 1e-9 else { return nil }
        var scores: [(Int, Double)] = []
        for offset in 0...(full.count - sample.count) {
            var sum = 0.0, squares = 0.0, dot = 0.0
            for i in centered.indices {
                let value = Double(full[offset + trim + i])
                sum += value; squares += value * value; dot += value * centered[i]
            }
            let variance = squares - sum * sum / Double(needle.count)
            let score = variance > 1e-9 ? dot / sqrt(energy * variance) : 0
            scores.append((offset, score))
        }
        guard let best = scores.max(by: { $0.1 < $1.1 }), best.1 >= 0.8 else { return nil }
        let rival = scores.filter { abs($0.0 - best.0) > 20 }.map(\.1).max() ?? 0
        guard best.1 - rival > 0.08 else { return nil }
        return Double(best.0) / Self.resolution
    }

    /// Start of the most energetic `length`-second window — usually the hook or chorus.
    func loudestWindow(length: TimeInterval) -> TimeInterval {
        let source = rms ?? levels
        let window = Int(length * Self.resolution)
        guard source.count > window, window > 0 else { return 0 }
        let energy = source.map { $0 * $0 }
        var sum = energy[0..<window].reduce(0, +)
        var best = sum
        var bestIndex = 0
        for index in window..<energy.count {
            sum += energy[index] - energy[index - window]
            if sum > best {
                best = sum
                bestIndex = index - window + 1
            }
        }
        return Double(bestIndex) / Self.resolution
    }
}

nonisolated enum WaveformAnalyzer {
    enum Failure: LocalizedError {
        case noAudio
        case protectedContent

        var errorDescription: String? {
            switch self {
            case .noAudio: "This file doesn't contain any audio MIFS can read."
            case .protectedContent: "This song is protected, so MIFS can't read its audio."
            }
        }
    }

    @concurrent
    static func analyze(_ url: URL) async throws -> Waveform {
        let asset = AVURLAsset(url: url)
        if try await asset.load(.hasProtectedContent) { throw Failure.protectedContent }
        guard let track = try await asset.loadTracks(withMediaType: .audio).first else { throw Failure.noAudio }
        let duration = try await asset.load(.duration).seconds

        let sampleRate = 8_000.0
        let reader = try AVAssetReader(asset: asset)
        let output = AVAssetReaderTrackOutput(track: track, outputSettings: [
            AVFormatIDKey: kAudioFormatLinearPCM,
            AVSampleRateKey: sampleRate,
            AVNumberOfChannelsKey: 1,
            AVLinearPCMBitDepthKey: 32,
            AVLinearPCMIsFloatKey: true,
            AVLinearPCMIsBigEndianKey: false,
            AVLinearPCMIsNonInterleaved: false,
        ])
        output.alwaysCopiesSampleData = false
        guard reader.canAdd(output) else { throw Failure.noAudio }
        reader.add(output)
        guard reader.startReading() else { throw reader.error ?? Failure.noAudio }

        let samplesPerPoint = Int(sampleRate / Waveform.resolution)
        var levels: [Float] = []
        levels.reserveCapacity(Int(duration * Waveform.resolution) + 1)
        var pending: [Float] = []

        while let buffer = output.copyNextSampleBuffer() {
            try Task.checkCancellation()
            guard let block = CMSampleBufferGetDataBuffer(buffer) else { continue }
            let length = CMBlockBufferGetDataLength(block)
            var chunk = [Float](repeating: 0, count: length / MemoryLayout<Float>.size)
            chunk.withUnsafeMutableBytes { raw in
                _ = CMBlockBufferCopyDataBytes(block, atOffset: 0, dataLength: length, destination: raw.baseAddress!)
            }
            pending += chunk
            var offset = 0
            while pending.count - offset >= samplesPerPoint {
                levels.append(rms(pending[offset..<offset + samplesPerPoint]))
                offset += samplesPerPoint
            }
            pending.removeFirst(offset)
        }
        if reader.status == .failed { throw reader.error ?? Failure.noAudio }
        if !pending.isEmpty { levels.append(rms(pending[...])) }
        guard !levels.isEmpty else { throw Failure.noAudio }

        return Waveform(
            levels: contrast(levels),
            duration: duration.isFinite ? duration : Double(levels.count) / Waveform.resolution,
            rms: levels
        )
    }

    /// Maps RMS to 0...1 in decibels between the track's own quiet floor and its peak,
    /// so heavily mastered (uniformly loud) music still shows its rhythm.
    static func contrast(_ rms: [Float]) -> [Float] {
        let decibels = rms.map { 20 * log10(max($0, 1e-5)) }
        let sorted = decibels.sorted()
        guard let peak = sorted.last else { return [] }
        let floor = min(sorted[sorted.count / 20] - 2, peak - 6)
        return decibels.map { value in
            let position = ((value - floor) / (peak - floor)).clamped(to: 0...1)
            return 0.1 + 0.9 * pow(position, 1.4)
        }
    }

    private static func rms(_ samples: ArraySlice<Float>) -> Float {
        samples.withUnsafeBufferPointer { pointer in
            var value: Float = 0
            vDSP_rmsqv(pointer.baseAddress!, 1, &value, vDSP_Length(pointer.count))
            return value
        }
    }
}

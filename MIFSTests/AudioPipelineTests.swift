import AVFoundation
import Testing
@testable import MIFS

struct AudioPipelineTests {
    /// 20 s AAC file: quiet tone, with a loud section from 8 s to 14 s.
    private func makeTone() throws -> URL {
        let url = URL.temporaryDirectory.appending(path: "tone-\(UUID().uuidString).m4a")
        let sampleRate = 44_100.0
        let format = AVAudioFormat(standardFormatWithSampleRate: sampleRate, channels: 2)!
        let file = try AVAudioFile(forWriting: url, settings: [
            AVFormatIDKey: kAudioFormatMPEG4AAC,
            AVSampleRateKey: sampleRate,
            AVNumberOfChannelsKey: 2,
        ])
        let frames = AVAudioFrameCount(sampleRate)
        for second in 0..<20 {
            let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: frames)!
            buffer.frameLength = frames
            let amplitude: Float = (8..<14).contains(second) ? 0.9 : 0.05
            for channel in 0..<2 {
                let samples = buffer.floatChannelData![channel]
                for frame in 0..<Int(frames) {
                    samples[frame] = amplitude * sin(Float(frame) * 2 * .pi * 440 / Float(sampleRate))
                }
            }
            try file.write(from: buffer)
        }
        return url
    }

    @Test func analysesAndSuggestsTheLoudPart() async throws {
        let url = try makeTone()
        defer { try? FileManager.default.removeItem(at: url) }

        let waveform = try await WaveformAnalyzer.analyze(url)
        #expect(abs(waveform.duration - 20) < 0.2)
        #expect(abs(Double(waveform.levels.count) - 20 * Waveform.resolution) <= 3)
        let start = waveform.loudestWindow(length: 5)
        #expect((8...9.2).contains(start))
    }

    @Test func exportsClipOfRequestedLength() async throws {
        let url = try makeTone()
        defer { try? FileManager.default.removeItem(at: url) }

        let relative = try await SnippetExporter.export(
            from: url, start: 8, duration: 5, title: "Tone", artist: "MIFS Tests", artwork: nil
        )
        let clip = AppGroup.url(for: relative)
        defer { try? FileManager.default.removeItem(at: clip) }

        let duration = try await AVURLAsset(url: clip).load(.duration).seconds
        #expect(abs(duration - 5) < 0.15)
        let metadata = try await AVURLAsset(url: clip).load(.commonMetadata)
        let title = try await AVMetadataItem.metadataItems(from: metadata, filteredByIdentifier: .commonIdentifierTitle)
            .first?.load(.stringValue)
        #expect(title == "Tone")
    }
}

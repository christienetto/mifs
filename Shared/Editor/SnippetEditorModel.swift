import Foundation
import Observation

@Observable
final class SnippetEditorModel {
    enum Phase: Equatable {
        case loading
        case ready
        case failed(String)
    }

    static let lengthOptions: [TimeInterval] = [5, 10, 15]
    static let previewID = "snippet-editor"

    let track: Track
    private(set) var phase: Phase = .loading
    private(set) var waveform = Waveform(levels: [], duration: 0)
    private(set) var isSaving = false
    /// Start of the selection in seconds. Written continuously by the scrubber.
    var start: TimeInterval = 0
    private(set) var length: TimeInterval = 10
    /// Bumped when the model moves the selection itself, so the scrubber scrolls to `start`.
    private(set) var scrollRequest = 0

    private var audioURL: URL?
    private let player = SnippetPlayer.shared

    init(track: Track) {
        self.track = track
    }

    var duration: TimeInterval { waveform.duration }
    var maxStart: TimeInterval { max(0, duration - length) }
    var availableLengths: [TimeInterval] {
        let fitting = Self.lengthOptions.filter { $0 <= duration + 0.05 }
        return fitting.isEmpty ? [duration] : fitting
    }
    var isPreviewing: Bool { player.isActive(Self.previewID) }
    var previewState: SnippetPlayer.State { isPreviewing ? player.state : .idle }
    var previewProgress: Double { isPreviewing ? player.progress : 0 }

    func load() async {
        guard phase != .ready else { return }
        phase = .loading
        do {
            let local: URL
            switch track.kind {
            case .catalog:
                guard let preview = track.previewURL else { throw URLError(.fileDoesNotExist) }
                local = try await PreviewCache.localFile(for: preview)
            case .file:
                guard let source = track.sourceURL else { throw URLError(.fileDoesNotExist) }
                local = source
            }
            let analysed = try await WaveformAnalyzer.analyze(local)
            guard analysed.duration >= 1 else { throw WaveformAnalyzer.Failure.noAudio }
            audioURL = local
            waveform = analysed
            length = min(10, analysed.duration)
            start = analysed.loudestWindow(length: length)
            phase = .ready
            scrollRequest += 1
            playSelection()
        } catch is CancellationError {
        } catch {
            phase = .failed(Self.message(for: error))
        }
    }

    func setLength(_ newLength: TimeInterval) {
        guard newLength != length else { return }
        // Keep the selection centred on the same moment.
        let centre = start + length / 2
        length = min(newLength, duration)
        start = (centre - length / 2).clamped(to: 0...maxStart)
        scrollRequest += 1
        Haptics.selection()
        playSelection()
    }

    /// The selection was moved without scrolling (e.g. VoiceOver adjust).
    func nudged() {
        scrollRequest += 1
        playSelection()
    }

    func playSelection() {
        guard let audioURL else { return }
        player.play(id: Self.previewID, url: audioURL, start: start, duration: length)
    }

    func togglePreview() {
        isPreviewing ? stopPreview() : playSelection()
    }

    func stopPreview() {
        if isPreviewing { player.stop() }
    }

    /// Finalises the selection: exports owned audio if needed and records it in history.
    func makeSnippet() async throws -> Snippet {
        isSaving = true
        defer { isSaving = false }
        stopPreview()

        var savedTrack = track
        savedTrack.sourceURL = nil
        var snippet = Snippet(
            track: savedTrack,
            start: start,
            duration: length,
            waveform: waveform.levels(from: start, duration: length, bars: Snippet.waveformBars)
        )
        if track.kind == .file, let audioURL {
            let artwork = track.artworkFile.flatMap { try? Data(contentsOf: AppGroup.url(for: $0)) }
            snippet.clipFile = try await SnippetExporter.export(
                from: audioURL, start: start, duration: length,
                title: track.title, artist: track.artist, artwork: artwork
            )
        }
        SnippetStore.shared.add(snippet)
        return snippet
    }

    private static func message(for error: Error) -> String {
        if let error = error as? LocalizedError, let description = error.errorDescription { return description }
        if error is URLError { return "Couldn't download this song's preview. Check your connection and try again." }
        return "MIFS couldn't open this audio."
    }
}

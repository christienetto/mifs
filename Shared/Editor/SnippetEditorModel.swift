import Foundation
import Observation
import OSLog

@Observable
final class SnippetEditorModel {
    enum Phase: Equatable {
        case loading
        case ready
        case failed(String)
    }

    static let lengthOptions: [TimeInterval] = [5, 10, 15]
    static let previewID = "snippet-editor"
    /// Picking a lyric starts the snippet slightly early so the first word isn't faded in.
    static let lyricLeadIn: TimeInterval = 0.25

    private(set) var track: Track
    private(set) var phase: Phase = .loading
    private(set) var waveform = Waveform(levels: [], duration: 0)
    /// Synced lyrics (MIFS server songs); empty when there are none.
    private(set) var lyrics: [LyricLine] = []
    private(set) var isSaving = false
    private(set) var isPreparing = false
    private(set) var preparationNote: String?
    private(set) var downloadFraction: Double?
    private(set) var isFinishingDownload = false
    /// What the download bar shows: always moving forward, never behind `downloadFraction`.
    private(set) var downloadProgress: Double = 0
    var canSend: Bool { phase == .ready && !isPreparing && track.preparationRef == nil }

    /// Start of the selection in seconds. Written continuously by the scrubber.
    var start: TimeInterval = 0
    private(set) var length: TimeInterval = 10
    /// Bumped when the model moves the selection itself, so the scrubber scrolls to `start`.
    private(set) var scrollRequest = 0

    private var audioURL: URL?
    private let player = SnippetPlayer.shared
    private let server: MusicServer

    init(track: Track, server: MusicServer = .shared) {
        self.track = track
        self.server = server
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
    /// Song time being heard while the selection previews.
    var playhead: TimeInterval? { !isPreparing && isPreviewing && player.state == .playing ? start + previewProgress * length : nil }
    var selectedLyrics: [LyricLine] { lyrics.heard(from: start, to: start + length) }

    func load() async {
        guard phase != .ready || track.preparationRef != nil else { return }
        phase = .loading
        if let ref = track.preparationRef { await prepareSelection(ref: ref); return }
        do {
            let (audio, analysed) = try await prepareAudio()
            guard analysed.duration >= 1 else { throw WaveformAnalyzer.Failure.noAudio }
            audioURL = audio
            waveform = analysed
            length = min(10, analysed.duration)
            start = track.highlightStart.map { $0.clamped(to: 0...maxStart) } ?? analysed.loudestWindow(length: length)
            phase = .ready
            scrollRequest += 1
            playSelection()
        } catch is CancellationError {
        } catch {
            phase = .failed(Self.message(for: error))
        }
    }


    /// No preview audio: the timeline appears only when the complete recording is ready.
    private func prepareSelection(ref: String) async {
        isPreparing = true
        preparationNote = "Waiting to prepare your song…"
        downloadFraction = nil
        downloadProgress = 0
        defer { isPreparing = false }
        do {
            var song = try await server.prepare(ref: ref)
            let deadline = ContinuousClock.now + .seconds(360)
            while song.track == nil {
                try Task.checkCancellation()
                if song.status == "failed" || song.status == "unavailable" {
                    throw MusicServer.Failure.preparationFailed(song.statusMessage ?? "MIFS can't get this song yet.")
                }
                if ContinuousClock.now >= deadline { throw MusicServer.Failure.stillAdding }
                if let total = song.downloadTotalBytes, total > 0 {
                    downloadFraction = (Double(song.downloadedBytes ?? 0) / Double(total)).clamped(to: 0...1)
                    isFinishingDownload = downloadFraction == 1
                } else { downloadFraction = nil; isFinishingDownload = false }
                downloadProgress = downloadFraction ?? 0
                preparationNote = song.statusMessage ?? (isFinishingDownload ? "Finishing your song…" : downloadFraction != nil ? "Downloading your song…" : "Finding an audio source…")
                if lyrics.isEmpty { lyrics = (try? await server.lyrics(for: song.id)) ?? [] }
                try await Task.sleep(for: .milliseconds(500))
                song = try await server.song(id: song.id)
            }
            guard let fullTrack = song.track else { throw MusicServer.Failure.songUnavailable }
            track = fullTrack
            let (audio, envelope) = try await prepareAudio()
            try Task.checkCancellation()
            guard envelope.duration >= 1 else { throw WaveformAnalyzer.Failure.noAudio }
            audioURL = audio; waveform = envelope
            length = min(10, envelope.duration)
            start = fullTrack.highlightStart?.clamped(to: 0...maxStart) ?? envelope.loudestWindow(length: length)
            downloadFraction = 1; downloadProgress = 1; preparationNote = nil
            phase = .ready; scrollRequest += 1
            isPreparing = false
            playSelection()
        } catch is CancellationError {
        } catch {
            preparationNote = nil
            phase = .failed(Self.message(for: error))
        }
    }

    /// Resize one edge while holding the other fixed. Scrolling is deferred until the drag ends.
    func resizeStart(to value: TimeInterval) {
        let end = start + length
        start = value.clamped(to: max(0, end - 20)...max(0, end - 1))
        length = end - start
    }

    func resizeEnd(to value: TimeInterval) {
        let end = value.clamped(to: min(duration, start + 1)...min(duration, start + 20))
        length = end - start
    }

    func setRange(start newStart: TimeInterval, end: TimeInterval) {
        guard newStart.isFinite, end.isFinite, end > newStart else { return }
        length = (end - newStart).clamped(to: 1...min(20, max(1, duration)))
        start = newStart.clamped(to: 0...maxStart)
        nudged()
    }

    /// The URL to play from and the envelope to draw. Catalog previews are downloaded and
    /// analysed on device; server songs stream, with envelope and lyrics from the server.
    private func prepareAudio() async throws -> (URL, Waveform) {
        switch track.kind {
        case .catalog:
            guard let preview = track.previewURL else { throw URLError(.fileDoesNotExist) }
            let local = try await PreviewCache.localFile(for: preview)
            return (local, try await WaveformAnalyzer.analyze(local))
        case .file:
            guard let source = track.sourceURL else { throw URLError(.fileDoesNotExist) }
            return (source, try await WaveformAnalyzer.analyze(source))
        case .server:
            guard let stream = track.previewURL else { throw URLError(.fileDoesNotExist) }
            let server = server, id = track.id
            async let lines = server.lyrics(for: id)
            let envelope = try await server.waveform(for: id)
            do {
                lyrics = try await lines
            } catch {
                Logger.mifs.error("Lyrics unavailable: \(error.localizedDescription)")
            }
            return (stream, envelope)
        }
    }

    func setLength(_ newLength: TimeInterval) {
        guard newLength != length else { return }
        // Keep the selection centred on the same moment.
        let centre = start + length / 2
        length = newLength.clamped(to: 1...min(20, max(1, duration)))
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

    /// Starts the selection at a lyric line and previews it.
    func select(_ line: LyricLine) {
        guard !isPreparing else { return }
        start = (line.start - Self.lyricLeadIn).clamped(to: 0...maxStart)
        Haptics.selection()
        nudged()
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
        guard canSend else { throw MusicServer.Failure.stillAdding }
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
        if !selectedLyrics.isEmpty { snippet.lyrics = selectedLyrics }
        if track.kind == .server {
            snippet.shareURL = try await server.mif(songID: track.id, start: start, duration: length)
        }
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
        if error is URLError { return "Couldn't download this song's audio. Check your connection and try again." }
        return "MIFS couldn't open this audio."
    }
}

import AVFoundation
import Observation
import OSLog

/// Plays a time range of an audio file or preview stream. One snippet plays at a time per process.
@Observable
final class SnippetPlayer {
    static let shared = SnippetPlayer()

    enum State: Equatable {
        case idle
        case loading
        case playing
    }

    private(set) var state: State = .idle
    /// Identifies what is currently loaded, so each view can tell whether it owns playback.
    private(set) var itemID: String?
    /// 0...1 through the current range.
    private(set) var progress: Double = 0
    private(set) var lastError: String?

    private let player = AVPlayer()
    private var range: (start: TimeInterval, duration: TimeInterval) = (0, 0)
    private var timeObserver: Any?
    private var endObserver: NSObjectProtocol?
    private var loadTask: Task<Void, Never>?

    init() {
        player.automaticallyWaitsToMinimizeStalling = false
        timeObserver = player.addPeriodicTimeObserver(
            forInterval: CMTime(value: 1, timescale: 30), queue: .main
        ) { [weak self] time in
            MainActor.assumeIsolated { self?.tick(time) }
        }
    }

    func isPlaying(_ id: String) -> Bool { itemID == id && state == .playing }
    func isActive(_ id: String) -> Bool { itemID == id && state != .idle }

    func toggle(id: String, url: URL, start: TimeInterval, duration: TimeInterval) {
        if isActive(id) {
            stop()
        } else {
            play(id: id, url: url, start: start, duration: duration)
        }
    }

    func play(id: String, url: URL, start: TimeInterval, duration: TimeInterval) {
        loadTask?.cancel()
        player.pause()
        itemID = id
        range = (start, duration)
        progress = 0
        lastError = nil
        state = .loading

        loadTask = Task { [weak self] in
            do {
                let item = try await Self.makeItem(url: url, start: start, duration: duration)
                guard let self, !Task.isCancelled else { return }
                self.observeEnd(of: item)
                self.player.replaceCurrentItem(with: item)
                _ = await self.player.seek(to: .seconds(start), toleranceBefore: .zero, toleranceAfter: .zero)
                guard !Task.isCancelled, self.itemID == id else { return }
                Self.activateSession()
                self.player.play()
                self.state = .playing
            } catch is CancellationError {
            } catch {
                Logger.mifs.error("Playback failed: \(error.localizedDescription)")
                guard !Task.isCancelled, let self, self.itemID == id else { return }
                self.lastError = "Couldn't play this snippet."
                self.finish()
            }
        }
    }

    func stop() {
        loadTask?.cancel()
        player.pause()
        finish()
    }

    private func finish() {
        state = .idle
        progress = 0
        itemID = nil
        Task { [weak self] in
            // Let a follow-up play() claim the session before handing audio back to other apps.
            try? await Task.sleep(for: .milliseconds(400))
            guard let self, self.state == .idle else { return }
            Self.deactivateSession()
        }
    }

    private func tick(_ time: CMTime) {
        guard state == .playing, range.duration > 0 else { return }
        progress = ((time.seconds - range.start) / range.duration).clamped(to: 0...1)
    }

    private func observeEnd(of item: AVPlayerItem) {
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = NotificationCenter.default.addObserver(
            forName: AVPlayerItem.didPlayToEndTimeNotification, object: item, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.finish() }
        }
    }

    private static func makeItem(url: URL, start: TimeInterval, duration: TimeInterval) async throws -> AVPlayerItem {
        let asset = AVURLAsset(url: url)
        let track = try await asset.loadTracks(withMediaType: .audio).first
        let item = AVPlayerItem(asset: asset)
        item.forwardPlaybackEndTime = .seconds(start + duration)
        if let track {
            item.audioMix = SnippetFades.audioMix(for: track, start: start, duration: duration)
        }
        return item
    }

    private static func activateSession() {
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {
            Logger.mifs.error("Audio session activation failed: \(error.localizedDescription)")
        }
    }

    private static func deactivateSession() {
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
}

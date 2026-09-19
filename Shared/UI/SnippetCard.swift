import SwiftUI

/// The MIFS "bubble": artwork, title, live waveform and a play button.
struct SnippetCard: View {
    let snippet: Snippet

    @State private var player = SnippetPlayer.shared
    @State private var tint = Color(Theme.fallbackTint)

    private var id: String { snippet.id.uuidString }

    var body: some View {
        let isActive = player.isActive(id)
        HStack(spacing: 12) {
            ArtworkView(url: snippet.track.resolvedArtworkURL, cornerRadius: 10)
                .frame(width: 56)
                .shadow(color: .black.opacity(0.25), radius: 6, y: 3)

            VStack(alignment: .leading, spacing: 5) {
                HStack(spacing: 4) {
                    Text(snippet.track.title)
                        .font(.headline)
                        .lineLimit(1)
                    if snippet.track.isExplicit {
                        Image(systemName: "e.square.fill")
                            .font(.caption)
                            .opacity(0.7)
                            .accessibilityLabel("Explicit")
                    }
                }
                Text(snippet.track.artist)
                    .font(.subheadline)
                    .opacity(0.75)
                    .lineLimit(1)
                HStack(spacing: 8) {
                    WaveformBars(levels: snippet.waveform, progress: isActive ? player.progress : 0)
                        .frame(height: 20)
                    Text(isActive ? (snippet.duration * (1 - player.progress)).rounded(.up).clock : snippet.duration.clock)
                        .font(.caption.monospacedDigit().weight(.medium))
                        .opacity(0.8)
                        .contentTransition(.numericText(countsDown: true))
                }
            }

            PlayButton(state: isActive ? player.state : .idle, progress: isActive ? player.progress : 0) {
                togglePlayback()
            }
        }
        .padding(12)
        .foregroundStyle(.white)
        .background {
            ZStack {
                tint
                LinearGradient(colors: [.white.opacity(0.08), .black.opacity(0.15)], startPoint: .top, endPoint: .bottom)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(snippet.track.title) by \(snippet.track.artist), \(Int(snippet.duration)) second snippet")
        .accessibilityAddTraits(.startsMediaSession)
        .accessibilityAction(named: isActive ? "Stop" : "Play") { togglePlayback() }
        .task(id: snippet.track.resolvedArtworkURL) {
            guard let url = snippet.track.resolvedArtworkURL,
                  let color = await ArtworkLoader.shared.tint(for: url) else { return }
            tint = Color(color)
        }
    }

    private func togglePlayback() {
        guard let playback = snippet.playback else { return }
        Haptics.tap()
        player.toggle(id: id, url: playback.url, start: playback.start, duration: playback.duration)
    }
}

struct PlayButton: View {
    let state: SnippetPlayer.State
    var progress: Double = 0
    var size: CGFloat = 40
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Circle().fill(.white.opacity(0.22))
                Circle()
                    .trim(from: 0, to: progress)
                    .stroke(.white, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .padding(1.25)
                switch state {
                case .loading:
                    ProgressView().tint(.white)
                case .playing:
                    Image(systemName: "stop.fill").font(.system(size: size * 0.36, weight: .bold))
                case .idle:
                    Image(systemName: "play.fill").font(.system(size: size * 0.4, weight: .bold)).offset(x: size * 0.04)
                }
            }
            .frame(width: size, height: size)
            .contentTransition(.symbolEffect(.replace))
        }
        .buttonStyle(.plain)
        .foregroundStyle(.white)
        .accessibilityLabel(state == .idle ? "Play" : "Stop")
    }
}

/// Shared player for Recent and incoming links. Artwork yields to lyrics during playback.
struct SnippetPlaybackView: View {
    let snippet: Snippet
    var showLyricsInitially = false
    @State private var player = SnippetPlayer.shared
    @State private var lines: [LyricLine] = []
    private var id: String { snippet.id.uuidString }
    var body: some View {
        let active = player.isActive(id)
        VStack(spacing: 20) {
            Text(snippet.track.title).font(.title2.bold()).multilineTextAlignment(.center)
            Text(snippet.track.artist).foregroundStyle(.secondary)
            if (player.isPlaying(id) || showLyricsInitially), !lines.isEmpty {
                LyricsPanel(lines: lines, selected: lines,
                    playhead: active ? snippet.start + player.progress * snippet.duration : nil,
                    selectionStart: snippet.start, onSelect: { _ in })
            } else {
                ArtworkView(url: snippet.track.resolvedArtworkURL, cornerRadius: 20)
                    .frame(maxWidth: 300, maxHeight: 300)
                if active || showLyricsInitially { Text("Lyrics aren't available for this moment.").font(.callout) }
            }
            Spacer(minLength: 0)
            PlayButton(state: active ? player.state : .idle, progress: active ? player.progress : 0, size: 64) {
                guard let playback = snippet.playback else { return }
                player.toggle(id: id, url: playback.url, start: playback.start, duration: playback.duration)
            }
            if let error = player.lastError { Text(error).font(.caption) }
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background { ArtworkBackdrop(url: snippet.track.resolvedArtworkURL) }
        .foregroundStyle(.white)
        .environment(\.colorScheme, .dark)
        .task(id: snippet.id) {
            if let playback = snippet.playback {
                player.play(id: id, url: playback.url, start: playback.start, duration: playback.duration)
            }
            lines = snippet.lyrics ?? []
            if snippet.track.kind == .server {
                if let shareURL = snippet.shareURL,
                   let resolved = try? await MusicServer.shared.received(shareURL) {
                    lines = resolved.lyrics ?? lines
                } else if let full = try? await MusicServer.shared.lyrics(for: snippet.track.id) {
                    lines = full.heard(from: snippet.start, to: snippet.end)
                }
            }
        }
        .onDisappear { if player.isActive(id) { player.stop() } }
    }
}

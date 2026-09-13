import SwiftUI

struct ExtensionRootView: View {
    @Bindable var state: ExtensionState

    var body: some View {
        switch state.screen {
        case .received(let snippet):
            ReceivedSnippetView(snippet: snippet, state: state)
        case .browse:
            ComposeHomeView(state: state)
        }
    }
}

// MARK: - Recipient

private struct ReceivedSnippetView: View {
    let snippet: Snippet
    let state: ExtensionState

    @State private var player = SnippetPlayer.shared

    private var id: String { snippet.id.uuidString }

    var body: some View {
        let active = player.isActive(id)
        ZStack {
            ArtworkBackdrop(url: snippet.track.artworkURL)
            VStack(spacing: 22) {
                ArtworkView(url: snippet.track.artworkURL, cornerRadius: 20)
                    .frame(maxWidth: 300, maxHeight: 300)
                    .shadow(color: .black.opacity(0.4), radius: 28, y: 14)
                    .scaleEffect(player.isPlaying(id) ? 1 : 0.94)
                    .animation(.spring(duration: 0.5, bounce: 0.3), value: player.isPlaying(id))
                    .frame(maxHeight: .infinity)

                VStack(spacing: 4) {
                    Text(snippet.track.title).font(.title2.weight(.bold)).multilineTextAlignment(.center).lineLimit(2)
                    Text(snippet.track.artist).font(.body).opacity(0.75).lineLimit(1)
                }

                if let lyrics = snippet.lyrics, !lyrics.isEmpty {
                    LyricsExcerpt(lines: lyrics, playhead: active ? snippet.start + player.progress * snippet.duration : nil)
                }

                VStack(spacing: 8) {
                    WaveformBars(levels: snippet.waveform, progress: active ? player.progress : 0, spacing: 3)
                        .frame(height: 44)
                    HStack {
                        Text((active ? player.progress * snippet.duration : 0).clock)
                        Spacer()
                        Text(snippet.duration.clock)
                    }
                    .font(.caption.monospacedDigit())
                    .opacity(0.7)
                }

                PlayButton(state: active ? player.state : .idle, progress: active ? player.progress : 0, size: 72) {
                    Haptics.tap()
                    play()
                }

                if let error = player.lastError, !active {
                    Text(error).font(.footnote).opacity(0.8)
                }

                HStack(spacing: 10) {
                    if let appleMusic = snippet.track.appleMusicURL {
                        ListenButton(title: "Apple Music", symbol: "music.note") { state.open(appleMusic) }
                    }
                    // MIFS server songs aren't on streaming services.
                    if snippet.track.kind != .server, let spotify = snippet.track.spotifySearchURL {
                        ListenButton(title: "Spotify", symbol: "headphones") { state.open(spotify) }
                    }
                }

                Button {
                    player.stop()
                    state.screen = .browse
                } label: {
                    Label("Reply with a Snippet", systemImage: "arrowshape.turn.up.left.fill")
                        .font(.subheadline.weight(.semibold))
                }
                .buttonStyle(.plain)
                .opacity(0.9)
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
        }
        .foregroundStyle(.white)
        .environment(\.colorScheme, .dark)
        .onAppear(perform: play)
        .onDisappear { player.stop() }
    }

    private func play() {
        guard let playback = snippet.playback else { return }
        player.toggle(id: id, url: playback.url, start: playback.start, duration: playback.duration)
    }
}

/// The lyrics a snippet contains, lit line by line as it plays.
private struct LyricsExcerpt: View {
    let lines: [LyricLine]
    let playhead: TimeInterval?

    var body: some View {
        let sung = playhead.flatMap { lines.index(at: $0) }
        VStack(spacing: 6) {
            ForEach(lines.indices, id: \.self) { index in
                Text(lines[index].text)
                    .font(.headline)
                    .multilineTextAlignment(.center)
                    .opacity(sung == nil ? 0.85 : sung == index ? 1 : 0.4)
            }
        }
        .animation(.smooth(duration: 0.25), value: sung)
        .accessibilityElement(children: .combine)
    }
}

private struct ListenButton: View {
    let title: String
    let symbol: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label(title, systemImage: symbol)
                .font(.subheadline.weight(.semibold))
                .frame(maxWidth: .infinity, minHeight: 44)
                .background(.white.opacity(0.16), in: .capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Open in \(title)")
    }
}

// MARK: - Sender

private struct ComposeHomeView: View {
    @Bindable var state: ExtensionState
    @State private var store = SnippetStore.shared
    @FocusState private var searchFocused: Bool

    var body: some View {
        @Bindable var catalog = state.catalog
        NavigationStack(path: $state.path) {
            CatalogBrowser(model: state.catalog, onSelect: select) {
                if !store.snippets.isEmpty {
                    Section("Your Snippets") {
                        RecentSnippetsStrip(snippets: Array(store.snippets.prefix(20))) { snippet in
                            Task { try? await state.send(snippet) }
                        }
                        .listRowInsets(EdgeInsets())
                        .listRowBackground(Color.clear)
                    }
                }
                Section {
                    Button {
                        if let url = URL(string: "mifs://import") { state.open(url) }
                    } label: {
                        Label("Clip a song you own", systemImage: "waveform.badge.plus")
                    }
                } footer: {
                    Text("Opens MIFS to clip any part of audio files or DRM-free songs on your iPhone.")
                }
            }
            .navigationTitle("MIFS")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $catalog.query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search songs")
            .searchFocused($searchFocused)
            .onChange(of: searchFocused) { _, focused in if focused { state.expand() } }
            .navigationDestination(for: Track.self) { track in
                SnippetEditorView(track: track, sendTitle: "Add to Message", onSend: state.send)
                    .toolbarBackground(.hidden, for: .navigationBar)
            }
        }
    }

    private func select(_ track: Track) {
        SnippetPlayer.shared.stop()
        state.expand()
        state.path.append(track)
    }
}

private struct RecentSnippetsStrip: View {
    let snippets: [Snippet]
    let onSend: (Snippet) -> Void

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 12) {
                ForEach(snippets) { snippet in
                    Button { onSend(snippet) } label: {
                        VStack(alignment: .leading, spacing: 6) {
                            ArtworkView(url: snippet.track.resolvedArtworkURL, cornerRadius: 12)
                                .frame(width: 96)
                                .overlay(alignment: .bottomTrailing) {
                                    Text(snippet.duration.shortSeconds)
                                        .font(.caption2.weight(.bold).monospacedDigit())
                                        .padding(.horizontal, 6)
                                        .padding(.vertical, 3)
                                        .background(.black.opacity(0.55), in: .capsule)
                                        .foregroundStyle(.white)
                                        .padding(6)
                                }
                            Text(snippet.track.title).font(.caption.weight(.semibold)).lineLimit(1)
                            Text(snippet.track.artist).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                        }
                        .frame(width: 96)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Send \(snippet.track.title) snippet")
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
        }
        .scrollIndicators(.hidden)
    }
}

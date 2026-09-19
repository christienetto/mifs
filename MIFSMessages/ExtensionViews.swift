import SwiftUI

struct ExtensionRootView: View {
    @Bindable var state: ExtensionState

    var body: some View {
        switch state.screen {
        case .received(let snippet):
            VStack {
                SnippetPlaybackView(snippet: snippet)
                Button("Reply with a Mif") { state.screen = .browse }.padding()
            }
        case .browse:
            ComposeHomeView(state: state)
        }
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
                    Section("Recent") {
                        RecentSnippetsStrip(snippets: Array(store.snippets.prefix(20))) { snippet in
                            state.screen = .received(snippet)
                        }
                        .listRowInsets(EdgeInsets())
                        .listRowBackground(Color.clear)
                    }
                }

            }
            .navigationTitle("MIFS")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $catalog.query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search songs")
            .searchFocused($searchFocused)
            .onChange(of: searchFocused) { _, focused in if focused { state.expand() } }
            .navigationDestination(for: Track.self) { track in
                SnippetEditorView(track: track, sendTitle: "Share", onSend: state.send)
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

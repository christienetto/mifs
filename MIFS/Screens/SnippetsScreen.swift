import SwiftUI

struct SnippetsScreen: View {
    @Environment(AppRouter.self) private var router
    @Environment(ComposeCenter.self) private var composer
    @State private var store = SnippetStore.shared
    @State private var sharing: Snippet?
    @State private var selected: Snippet?

    var body: some View {
        NavigationStack {
            Group {
                if store.snippets.isEmpty {
                    ContentUnavailableView {
                        Label("No Recent Mifs", systemImage: "waveform")
                    } description: {
                        Text("Snippets you make appear here so you can play and send them again.")
                    } actions: {
                        Button("Find a Song") { router.tab = .discover }
                            .buttonStyle(.borderedProminent)
                    }
                } else {
                    list
                }
            }
            .navigationTitle("Recent")
            .navigationDestination(item: $selected) { snippet in
                SnippetPlaybackView(snippet: snippet, showLyricsInitially: true)
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbarColorScheme(.dark, for: .navigationBar)
            }
        }
        .sheet(item: $sharing) { snippet in
            ShareSheet(items: ShareItems.items(for: snippet)) { sharing = nil }
                .presentationDetents([.medium, .large])
        }
    }

    private var list: some View {
        List {
            ForEach(store.snippets) { snippet in
                SnippetCard(snippet: snippet)
                    .accessibilityIdentifier("recent-mif-\(snippet.track.id)")
                    .onTapGesture { selected = snippet }
                    .accessibilityAction(named: "Show lyrics") { selected = snippet }
                    .clipShape(.rect(cornerRadius: 20, style: .continuous))
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                    .swipeActions(edge: .leading) {
                        Button("Send", systemImage: "arrow.up.message.fill") { send(snippet) }
                            .tint(Theme.violet)
                        if TelegramLink.isConfigured {
                            Button("Telegram", systemImage: "paperplane.fill") { sendToTelegram(snippet) }
                                .tint(Theme.telegram)
                        }
                    }
                    .swipeActions(edge: .trailing) {
                        Button("Delete", systemImage: "trash", role: .destructive) { store.delete(snippet) }
                    }
                    .contextMenu {
                        Button("Send in Messages", systemImage: "arrow.up.message") { send(snippet) }
                        if TelegramLink.isConfigured {
                            Button("Send in Telegram", systemImage: "paperplane") { sendToTelegram(snippet) }
                        }
                        Button("Share…", systemImage: "square.and.arrow.up") { composer.share(snippet) }
                        if snippet.track.kind != .file {
                            Button("Make Another Snippet", systemImage: "scissors") { router.edit(snippet.track) }
                        }
                        Divider()
                        Button("Delete", systemImage: "trash", role: .destructive) { store.delete(snippet) }
                    }
            }
        }
        .listStyle(.plain)
    }

    private func send(_ snippet: Snippet) {
        Task { await composer.send(snippet) }
    }

    private func sendToTelegram(_ snippet: Snippet) {
        Task { await composer.sendToTelegram(snippet) }
    }
}

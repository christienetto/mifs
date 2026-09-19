import SwiftUI

@Observable
final class CatalogModel {
    var query = "" { didSet { if query != oldValue { scheduleSearch() } } }
    private(set) var serverResults: [ServerSearchResult] = []
    private(set) var library: [Track] = []
    private(set) var hasLoadedLibrary = false
    private(set) var isSearching = false
    private(set) var searchError: String?
    private(set) var libraryError: String?
    let server: MusicServer
    private var searchTask: Task<Void, Never>?
    private var isLoadingLibrary = false
    init(catalog: CatalogService = .shared, server: MusicServer = .shared) { self.server = server }
    var trimmedQuery: String { query.trimmingCharacters(in: .whitespacesAndNewlines) }
    func loadLibraryIfNeeded() async {
        guard !hasLoadedLibrary, !isLoadingLibrary else { return }
        isLoadingLibrary = true
        defer { isLoadingLibrary = false; hasLoadedLibrary = true }
        do { library = try await server.songs(); libraryError = nil }
        catch { libraryError = error.localizedDescription }
    }
    func retryLibrary() async { hasLoadedLibrary = false; await loadLibraryIfNeeded() }
    private func scheduleSearch() {
        searchTask?.cancel()
        serverResults = []; searchError = nil
        let term = trimmedQuery
        isSearching = !term.isEmpty
        guard !term.isEmpty else { return }
        searchTask = Task {
            do {
                try await Task.sleep(for: .milliseconds(300))
                let found = try await server.search(term)
                try Task.checkCancellation()
                serverResults = found
            } catch {
                guard !Task.isCancelled else { return }
                searchError = error.localizedDescription
            }
            isSearching = false
        }
    }
    func select(_ result: ServerSearchResult, open: @escaping (Track) -> Void) {
        if let track = result.song?.track { open(track); return }
        open(Track(id: result.ref, kind: .catalog, title: result.title, artist: result.artist,
            album: result.album, artworkURL: result.artwork?.url,
            isExplicit: result.explicit, thumbnailURL: result.artwork?.thumbnailUrl,
            preparationRef: result.ref, expectedDuration: result.durationMs.map { Double($0) / 1000 }))
    }
}

struct CatalogBrowser<Header: View>: View {
    @Bindable var model: CatalogModel
    let onSelect: (Track) -> Void
    @ViewBuilder var header: Header
    var body: some View {
        List {
            if model.trimmedQuery.isEmpty {
                header
                Section("Top Songs") {
                    if let error = model.libraryError {
                        MessageRow(symbol: "server.rack", text: error, actionTitle: "Retry") { Task { await model.retryLibrary() } }
                    } else if !model.hasLoadedLibrary {
                        ForEach(0..<3, id: \.self) { _ in PlaceholderRow() }
                    } else if model.library.isEmpty {
                        MessageRow(symbol: "music.note", text: "Songs you prepare will appear here.")
                    } else {
                        ForEach(Array(model.library.enumerated()), id: \.element.id) { index, track in
                            Button { onSelect(track) } label: { TrackRow(track: track, rank: index + 1) }.tint(.primary)
                        }
                    }
                }
            } else {
                Section("Spotify") {
                    if model.isSearching {
                        ForEach(0..<3, id: \.self) { _ in PlaceholderRow() }
                    } else if let error = model.searchError {
                        MessageRow(symbol: "wifi.exclamationmark", text: error)
                    } else if model.serverResults.isEmpty {
                        MessageRow(symbol: "magnifyingglass", text: "No songs found.")
                    } else {
                        ForEach(model.serverResults) { result in
                            Button { model.select(result, open: onSelect) } label: {
                                HStack(spacing: 12) {
                                    ArtworkView(url: result.artwork?.thumbnailUrl, cornerRadius: 8).frame(width: 52)
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text(result.title).font(.headline).lineLimit(1)
                                        Text(result.artist).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                                        Text(result.isReady ? "Ready to play" : "Prepare song").font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                            }.tint(.primary)
                            .accessibilityLabel("\(result.title) by \(result.artist), Spotify, \(result.isReady ? "ready to play" : "prepare song")")
                        }
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .scrollDismissesKeyboard(.immediately)
        .task { await model.retryLibrary() }
    }
}

private struct PlaceholderRow: View {
    var body: some View {
        HStack(spacing: 12) {
            RoundedRectangle(cornerRadius: 8).frame(width: 52, height: 52)
            VStack(alignment: .leading, spacing: 6) {
                Capsule().frame(width: 150, height: 12)
                Capsule().frame(width: 90, height: 10)
            }
        }
        .foregroundStyle(.quaternary)
        .accessibilityHidden(true)
    }
}

struct MessageRow: View {
    let symbol: String
    let text: String
    var actionTitle: String?
    var action: (() -> Void)?

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: symbol).font(.title2).foregroundStyle(.secondary)
            Text(text).font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
            if let actionTitle, let action {
                Button(actionTitle, action: action).buttonStyle(.bordered)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 24)
    }
}

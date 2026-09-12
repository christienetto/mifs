import SwiftUI

@Observable
final class CatalogModel {
    var query = "" {
        didSet { if query != oldValue { scheduleSearch() } }
    }
    private(set) var results: [Track] = []
    private(set) var topSongs: [Track] = []
    private(set) var isSearching = false
    private(set) var isLoadingCharts = false
    private(set) var searchError: String?
    private(set) var chartsError: String?

    private let catalog: CatalogService
    private var searchTask: Task<Void, Never>?

    init(catalog: CatalogService = .shared) {
        self.catalog = catalog
    }

    var trimmedQuery: String { query.trimmingCharacters(in: .whitespacesAndNewlines) }

    func loadChartsIfNeeded() async {
        guard topSongs.isEmpty, !isLoadingCharts else { return }
        isLoadingCharts = true
        defer { isLoadingCharts = false }
        do {
            topSongs = try await catalog.topSongs()
            chartsError = nil
        } catch {
            chartsError = error.localizedDescription
        }
    }

    func retryCharts() async {
        topSongs = []
        await loadChartsIfNeeded()
    }

    private func scheduleSearch() {
        searchTask?.cancel()
        let term = trimmedQuery
        guard !term.isEmpty else {
            results = []
            isSearching = false
            searchError = nil
            return
        }
        isSearching = true
        searchTask = Task {
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            do {
                let found = try await catalog.search(term)
                guard !Task.isCancelled else { return }
                results = found
                searchError = nil
            } catch is CancellationError {
                return
            } catch let error as URLError where error.code == .cancelled {
                return
            } catch {
                searchError = error.localizedDescription
            }
            isSearching = false
        }
    }
}

/// Search results when there's a query, otherwise `header` followed by Apple Music's top songs.
struct CatalogBrowser<Header: View>: View {
    @Bindable var model: CatalogModel
    let onSelect: (Track) -> Void
    @ViewBuilder var header: Header

    var body: some View {
        List {
            if model.trimmedQuery.isEmpty {
                header
                chartSection
            } else {
                searchSection
            }
        }
        .listStyle(.insetGrouped)
        .scrollDismissesKeyboard(.immediately)
        .task { await model.loadChartsIfNeeded() }
    }

    @ViewBuilder
    private var chartSection: some View {
        Section {
            if model.topSongs.isEmpty {
                if let error = model.chartsError {
                    MessageRow(symbol: "wifi.exclamationmark", text: error, actionTitle: "Retry") {
                        Task { await model.retryCharts() }
                    }
                } else {
                    ForEach(0..<6, id: \.self) { _ in PlaceholderRow() }
                }
            } else {
                ForEach(Array(model.topSongs.enumerated()), id: \.element.id) { index, track in
                    Button { onSelect(track) } label: { TrackRow(track: track, rank: index + 1) }
                        .tint(.primary)
                }
            }
        } header: {
            Text("Top Songs")
        }
    }

    @ViewBuilder
    private var searchSection: some View {
        Section {
            if model.results.isEmpty {
                if model.isSearching {
                    ForEach(0..<6, id: \.self) { _ in PlaceholderRow() }
                } else if let error = model.searchError {
                    MessageRow(symbol: "wifi.exclamationmark", text: error)
                } else {
                    MessageRow(symbol: "magnifyingglass", text: "No songs found for “\(model.trimmedQuery)”.")
                }
            } else {
                ForEach(model.results) { track in
                    Button { onSelect(track) } label: { TrackRow(track: track) }
                        .tint(.primary)
                }
            }
        }
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

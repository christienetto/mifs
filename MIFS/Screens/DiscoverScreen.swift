import MediaPlayer
import SwiftUI

struct DiscoverScreen: View {
    @Environment(AppRouter.self) private var router
    @Environment(ComposeCenter.self) private var composer
    @Environment(\.openURL) private var openURL

    @State private var catalog = CatalogModel()
    @State private var isImportingFile = false
    @State private var isPickingFromLibrary = false
    @State private var isPreparing = false
    @State private var notice: Notice?

    var body: some View {
        @Bindable var router = router
        NavigationStack(path: $router.discoverPath) {
            CatalogBrowser(model: catalog, onSelect: open) {
                Section {
                    OwnMusicCard(
                        onFiles: { isImportingFile = true },
                        onLibrary: pickFromLibrary
                    )
                    .listRowInsets(EdgeInsets())
                }
            }
            .navigationTitle("MIFS")
            .searchable(text: $catalog.query, prompt: "Songs or artists")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button("Audio Files", systemImage: "folder") { isImportingFile = true }
                        Button("Music Library", systemImage: "music.note.house", action: pickFromLibrary)
                    } label: {
                        Label("Clip your own audio", systemImage: "plus")
                    }
                }
            }
            .navigationDestination(for: Track.self) { track in
                SnippetEditorView(track: track, destinations: composer.destinations)
                .toolbarBackground(.hidden, for: .navigationBar)
                .toolbar(.hidden, for: .tabBar)
            }
        }
        .fileImporter(isPresented: $isImportingFile, allowedContentTypes: [.audio]) { result in
            switch result {
            case .success(let url): importFile(url)
            case .failure(let error): notice = Notice(title: "Couldn't open file", message: error.localizedDescription)
            }
        }
        .sheet(isPresented: $isPickingFromLibrary) {
            MediaLibraryPicker { item in
                isPickingFromLibrary = false
                if let item { resolve(item) }
            }
            .ignoresSafeArea()
        }
        .confirmationDialog("Clip your own audio", isPresented: $router.isShowingImportOptions, titleVisibility: .visible) {
            Button("Audio Files") { isImportingFile = true }
            Button("Music Library", action: pickFromLibrary)
        }
        .overlay {
            if isPreparing {
                ProgressView("Preparing song…")
                    .padding(24)
                    .background(.regularMaterial, in: .rect(cornerRadius: 16))
            }
        }
        .alert(notice?.title ?? "", isPresented: Binding(get: { notice != nil }, set: { if !$0 { notice = nil } }), presenting: notice) { notice in
            if let track = notice.continueWith {
                Button("Use Preview") { open(track) }
                Button("Cancel", role: .cancel) {}
            } else if notice.offersSettings {
                Button("Open Settings") { openURL(URL(string: UIApplication.openSettingsURLString)!) }
                Button("Cancel", role: .cancel) {}
            } else {
                Button("OK", role: .cancel) {}
            }
        } message: { notice in
            Text(notice.message)
        }
    }

    private func open(_ track: Track) {
        SnippetPlayer.shared.stop()
        router.discoverPath.append(track)
    }

    private func importFile(_ url: URL) {
        isPreparing = true
        Task {
            defer { isPreparing = false }
            do {
                open(try await LocalAudioImporter.importFile(at: url))
            } catch {
                notice = Notice(title: "Couldn't open file", message: error.localizedDescription)
            }
        }
    }

    private func pickFromLibrary() {
        switch MPMediaLibrary.authorizationStatus() {
        case .authorized:
            isPickingFromLibrary = true
        case .notDetermined:
            MPMediaLibrary.requestAuthorization { status in
                Task { @MainActor in
                    if status == .authorized { isPickingFromLibrary = true }
                }
            }
        default:
            notice = Notice(
                title: "Music Library Access",
                message: "Allow MIFS to access your music library in Settings to clip songs you own.",
                offersSettings: true
            )
        }
    }

    private func resolve(_ item: MPMediaItem) {
        isPreparing = true
        Task {
            defer { isPreparing = false }
            do {
                switch try await LocalAudioImporter.resolve(item) {
                case .track(let track):
                    open(track)
                case .catalogFallback(let track):
                    notice = Notice(
                        title: "Protected Song",
                        message: "“\(track.title)” is protected by Apple Music, so no app can clip the full song. You can still send a moment from its 30‑second preview.",
                        continueWith: track
                    )
                }
            } catch {
                notice = Notice(title: "Couldn't use this song", message: error.localizedDescription)
            }
        }
    }
}

private struct Notice {
    var title: String
    var message: String
    var continueWith: Track?
    var offersSettings = false
}

private struct OwnMusicCard: View {
    let onFiles: () -> Void
    let onLibrary: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                Label("Clip songs you own", systemImage: "waveform.badge.plus")
                    .font(.headline)
                Text("Pick any 5–15 seconds of audio files or DRM-free library songs and send it as audio.")
                    .font(.subheadline)
                    .opacity(0.85)
            }
            HStack(spacing: 10) {
                CardButton(title: "Files", symbol: "folder.fill", action: onFiles)
                CardButton(title: "Music Library", symbol: "music.note.house.fill", action: onLibrary)
            }
        }
        .foregroundStyle(.white)
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.brandGradient)
    }
}

private struct CardButton: View {
    let title: String
    let symbol: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label(title, systemImage: symbol)
                .font(.subheadline.weight(.semibold))
                .frame(maxWidth: .infinity, minHeight: 40)
                .background(.white.opacity(0.22), in: .capsule)
        }
        .buttonStyle(.borderless)
        .foregroundStyle(.white)
    }
}

import SwiftUI

@main
struct MIFSApp: App {
    @State private var router = AppRouter()
    @State private var composer = ComposeCenter()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(router)
                .environment(composer)
                .onOpenURL { url in
                    if url.scheme == "mifs", url.host() == "import" {
                        router.showImportOptions()
                    } else if TelegramLink.isReturnFromSend(url) {
                        composer.telegramSendFinished()
                    }
                }
                .onChange(of: scenePhase) { _, phase in
                    if phase == .active { SnippetStore.shared.reload() }
                }
        }
    }
}

@Observable
final class AppRouter {
    enum Tab: Hashable {
        case discover
        case snippets
    }

    var tab: Tab = .discover
    var discoverPath: [Track] = []
    var isShowingImportOptions = false

    func showImportOptions() {
        tab = .discover
        discoverPath = []
        isShowingImportOptions = true
    }

    func edit(_ track: Track) {
        tab = .discover
        discoverPath = [track]
    }
}

struct RootView: View {
    @Environment(AppRouter.self) private var router
    @Environment(ComposeCenter.self) private var composer

    var body: some View {
        @Bindable var router = router
        @Bindable var composer = composer
        TabView(selection: $router.tab) {
            Tab("Discover", systemImage: "music.note.list", value: AppRouter.Tab.discover) {
                DiscoverScreen()
            }
            Tab("Snippets", systemImage: "waveform", value: AppRouter.Tab.snippets) {
                SnippetsScreen()
            }
        }
        .sheet(item: $composer.request) { request in
            ComposeSheet(request: request)
                .ignoresSafeArea()
        }
        .overlay(alignment: .top) {
            if let confirmation = composer.confirmation {
                ConfirmationBanner(text: confirmation)
                    .transition(.move(edge: .top).combined(with: .opacity))
                    .task {
                        try? await Task.sleep(for: .seconds(2.5))
                        composer.confirmation = nil
                    }
            }
        }
        .animation(.snappy, value: composer.confirmation)
    }
}

private struct ConfirmationBanner: View {
    let text: String

    var body: some View {
        Label(text, systemImage: "checkmark.circle.fill")
            .font(.subheadline.weight(.semibold))
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(.regularMaterial, in: .capsule)
            .shadow(color: .black.opacity(0.15), radius: 12, y: 4)
            .padding(.top, 8)
            .accessibilityAddTraits(.isStaticText)
    }
}

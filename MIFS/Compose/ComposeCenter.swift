import MessageUI
import Messages
import SwiftUI

/// Hands a finished snippet to Messages (or the share sheet when Messages isn't available) or Telegram.
@Observable
final class ComposeCenter {
    struct Request: Identifiable {
        let id = UUID()
        let snippet: Snippet
        let message: MSMessage?
        let usesMessages: Bool
    }

    var request: Request?
    var sharing: Snippet?
    /// A brief confirmation shown over the app, e.g. after Telegram hands back from a send.
    var confirmation: String?

    /// Where the app can send snippets, in the order they're offered.
    var destinations: [SendDestination] {
        [SendDestination(id: "share", name: "Share", title: "Share", symbol: "square.and.arrow.up") { [weak self] in
            SnippetPlayer.shared.stop()
            self?.sharing = $0
        }]
    }

    func share(_ snippet: Snippet) { SnippetPlayer.shared.stop(); sharing = snippet }

    func send(_ snippet: Snippet) async {
        SnippetPlayer.shared.stop()
        let message = snippet.track.kind == .file ? nil : await MessageFactory.message(for: snippet)
        request = Request(snippet: snippet, message: message, usesMessages: MFMessageComposeViewController.canSendText())
    }

    /// Catalog snippets open the MIFS Mini App in Telegram, which sends them to a chat as a card that plays
    /// right in the conversation. Clips of the user's own audio go through Telegram's share extension and
    /// arrive as a native audio message with the clip's title, artist and artwork.
    func sendToTelegram(_ snippet: Snippet) async {
        SnippetPlayer.shared.stop()
        guard let appURL = TelegramLink.appURL(for: snippet), let webURL = TelegramLink.webURL(for: snippet) else {
            request = Request(snippet: snippet, message: nil, usesMessages: false)
            return
        }
        let application = UIApplication.shared
        if application.canOpenURL(appURL), await application.open(appURL) { return }
        await application.open(webURL)
    }

    /// The MIFS Mini App sent the snippet and handed back to the app.
    func telegramSendFinished() {
        Haptics.success()
        confirmation = "Sent in Telegram"
    }
}

struct ComposeSheet: View {
    let request: ComposeCenter.Request
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        if request.usesMessages {
            MessageComposeView(request: request) { dismiss() }
        } else {
            ShareSheet(items: ShareItems.items(for: request.snippet)) { dismiss() }
        }
    }
}

private struct MessageComposeView: UIViewControllerRepresentable {
    let request: ComposeCenter.Request
    let onFinish: () -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onFinish: onFinish) }

    func makeUIViewController(context: Context) -> MFMessageComposeViewController {
        let controller = MFMessageComposeViewController()
        controller.messageComposeDelegate = context.coordinator
        if let message = request.message {
            controller.message = message
        } else if let clip = request.snippet.clipURL {
            controller.addAttachmentURL(clip, withAlternateFilename: request.snippet.attachmentName)
        }
        return controller
    }

    func updateUIViewController(_ controller: MFMessageComposeViewController, context: Context) {}

    final class Coordinator: NSObject, MFMessageComposeViewControllerDelegate {
        let onFinish: () -> Void

        init(onFinish: @escaping () -> Void) { self.onFinish = onFinish }

        nonisolated func messageComposeViewController(_ controller: MFMessageComposeViewController, didFinishWith result: MessageComposeResult) {
            MainActor.assumeIsolated {
                if result == .sent { Haptics.success() }
                onFinish()
            }
        }
    }
}

enum ShareItems {
    /// A clip file with a friendly name, the mif's page (plays anywhere), or a link to the song
    /// (Apple Music, or the MIFS server's audio).
    static func items(for snippet: Snippet) -> [Any] {
        if let clip = snippet.clipURL {
            let named = URL.temporaryDirectory.appending(path: snippet.attachmentName)
            try? FileManager.default.removeItem(at: named)
            if (try? FileManager.default.copyItem(at: clip, to: named)) != nil { return [named] }
            return [clip]
        }
        let caption = "🎵 \(snippet.track.title) – \(snippet.track.artist)"
        if let page = snippet.shareURL {
            return [caption, page]
        }
        // A bare media URL would be previewed as a file named by its content hash.
        if snippet.track.kind == .server, let audio = snippet.track.previewURL {
            return ["\(caption)\n\(audio.absoluteString)"]
        }
        var items: [Any] = [caption]
        if let link = snippet.track.appleMusicURL { items.append(link) }
        return items
    }
}

struct ShareSheet: UIViewControllerRepresentable {
    let items: [Any]
    var onFinish: () -> Void = {}

    func makeUIViewController(context: Context) -> UIActivityViewController {
        let controller = UIActivityViewController(activityItems: items, applicationActivities: nil)
        controller.completionWithItemsHandler = { _, _, _, _ in onFinish() }
        return controller
    }

    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}

/// One share entry point; each supported destination gets its native MIFS representation.
struct MifSharePicker: View {
    let snippet: Snippet
    @Environment(ComposeCenter.self) private var composer
    @Environment(\.dismiss) private var dismiss
    @State private var selected: Destination?
    enum Destination { case messages, telegram, other }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Label("\(snippet.track.title) · \(snippet.duration.shortSeconds)", systemImage: "waveform")
                }
                Button { choose(.messages) } label: { Label("Messages", systemImage: "message.fill") }
                if TelegramLink.isConfigured {
                    Button { choose(.telegram) } label: { Label("Telegram", systemImage: "paperplane.fill") }
                }
                Button { choose(.other) } label: { Label("Other Apps", systemImage: "square.and.arrow.up") }
                if let url = snippet.shareURL {
                    Button { UIPasteboard.general.url = url; dismiss() } label: { Label("Copy Link", systemImage: "link") }
                }
            }
            .navigationTitle("Share Mif")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
        }
        .presentationDetents([.medium])
        .onDisappear {
            guard let selected else { return }
            Task {
                // Wait for the destination picker to finish dismissal before presenting a composer.
                try? await Task.sleep(for: .milliseconds(300))
                switch selected {
                case .messages: await composer.send(snippet)
                case .telegram: await composer.sendToTelegram(snippet)
                case .other: composer.request = ComposeCenter.Request(snippet: snippet, message: nil, usesMessages: false)
                }
            }
        }
    }
    private func choose(_ destination: Destination) { selected = destination; dismiss() }
}

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
    /// A brief confirmation shown over the app, e.g. after Telegram hands back from a send.
    var confirmation: String?

    /// Where the app can send snippets, in the order they're offered.
    var destinations: [SendDestination] {
        var destinations = [
            SendDestination(id: "messages", name: "Messages", title: "Send in Messages", symbol: "arrow.up.message.fill") { [weak self] in
                await self?.send($0)
            },
        ]
        if TelegramLink.isConfigured {
            destinations.append(SendDestination(id: "telegram", name: "Telegram", title: "Send in Telegram", symbol: "paperplane.fill") { [weak self] in
                await self?.sendToTelegram($0)
            })
        }
        return destinations
    }

    func send(_ snippet: Snippet) async {
        SnippetPlayer.shared.stop()
        let message = snippet.track.kind == .catalog ? await MessageFactory.message(for: snippet) : nil
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
    /// A clip file with a friendly name, or the Apple Music link for catalog snippets.
    static func items(for snippet: Snippet) -> [Any] {
        if let clip = snippet.clipURL {
            let named = URL.temporaryDirectory.appending(path: snippet.attachmentName)
            try? FileManager.default.removeItem(at: named)
            if (try? FileManager.default.copyItem(at: clip, to: named)) != nil { return [named] }
            return [clip]
        }
        var items: [Any] = ["🎵 \(snippet.track.title) – \(snippet.track.artist)"]
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

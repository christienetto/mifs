import MessageUI
import Messages
import SwiftUI

/// Hands a finished snippet to Messages or Telegram, the only places MIFS shares to.
@Observable
final class ComposeCenter {
    struct Request: Identifiable {
        let id = UUID()
        let snippet: Snippet
        let message: MSMessage?
    }

    var request: Request?
    var sharing: Snippet?
    /// A brief confirmation shown over the app, e.g. after Telegram hands back from a send.
    var confirmation: String?
    var shareError: String?

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
        guard MFMessageComposeViewController.canSendText() else {
            shareError = "Messages isn't set up on this device. Turn on iMessage in Settings, or send your mif in Telegram."
            return
        }
        let message = snippet.track.kind == .file ? nil : await MessageFactory.message(for: snippet)
        request = Request(snippet: snippet, message: message)
    }

    /// Mifs open the MIFS Mini App in Telegram, which goes straight to Telegram's share sheet: a preview of the
    /// card, sent as soon as a chat is picked. Its Play button opens the Mini App right in the conversation,
    /// streaming the mif from the music server. Other snippets, such as clips of the user's own audio, can
    /// only be sent in Messages.
    func sendToTelegram(_ snippet: Snippet) async {
        SnippetPlayer.shared.stop()
        guard let appURL = TelegramLink.appURL(for: snippet), let webURL = TelegramLink.webURL(for: snippet) else {
            shareError = "Only mifs from the MIFS music server can be sent in Telegram. Send this one in Messages."
            return
        }
        let application = UIApplication.shared
        if application.canOpenURL(appURL), await application.open(appURL) { return }
        if !(await application.open(webURL)) {
            shareError = "Couldn't open Telegram. Install Telegram to send your mif there."
        }
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
        MessageComposeView(request: request) { dismiss() }
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

/// One share entry point: Messages, and Telegram for mifs the MIFS bot can play.
struct MifSharePicker: View {
    let snippet: Snippet
    @Environment(ComposeCenter.self) private var composer
    @Environment(\.dismiss) private var dismiss
    @State private var selected: Destination?
    enum Destination { case messages, telegram }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Label("\(snippet.track.title) · \(snippet.duration.shortSeconds)", systemImage: "waveform")
                }
                Button { choose(.messages) } label: { Label("Messages", systemImage: "message.fill") }
                if TelegramLink.canSend(snippet) {
                    Button { choose(.telegram) } label: { Label("Telegram", systemImage: "paperplane.fill") }
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
                }
            }
        }
    }
    private func choose(_ destination: Destination) { selected = destination; dismiss() }
}

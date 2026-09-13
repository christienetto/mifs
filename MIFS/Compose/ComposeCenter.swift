import MessageUI
import Messages
import SwiftUI

/// Hands a finished snippet to Messages (or the share sheet when Messages isn't available).
@Observable
final class ComposeCenter {
    struct Request: Identifiable {
        let id = UUID()
        let snippet: Snippet
        let message: MSMessage?
        let usesMessages: Bool
    }

    var request: Request?

    func send(_ snippet: Snippet) async {
        SnippetPlayer.shared.stop()
        let message = snippet.track.kind == .file ? nil : await MessageFactory.message(for: snippet)
        request = Request(snippet: snippet, message: message, usesMessages: MFMessageComposeViewController.canSendText())
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
    /// A clip file with a friendly name, or a link to the song (Apple Music, or the MIFS server's audio).
    static func items(for snippet: Snippet) -> [Any] {
        if let clip = snippet.clipURL {
            let named = URL.temporaryDirectory.appending(path: snippet.attachmentName)
            try? FileManager.default.removeItem(at: named)
            if (try? FileManager.default.copyItem(at: clip, to: named)) != nil { return [named] }
            return [clip]
        }
        let caption = "🎵 \(snippet.track.title) – \(snippet.track.artist)"
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

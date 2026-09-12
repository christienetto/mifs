import Messages
import SwiftUI

final class MessagesViewController: MSMessagesAppViewController {
    private let state = ExtensionState()
    private lazy var host = UIHostingController(rootView: ExtensionRootView(state: state))

    override func viewDidLoad() {
        super.viewDidLoad()
        state.send = { [weak self] snippet in try await self?.send(snippet) }
        state.expand = { [weak self] in
            guard let self, presentationStyle == .compact else { return }
            requestPresentationStyle(.expanded)
        }
        state.open = { [weak self] url in self?.extensionContext?.open(url) }

        host.view.backgroundColor = .clear
        addChild(host)
        view.addSubview(host.view)
        host.view.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            host.view.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            host.view.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            host.view.topAnchor.constraint(equalTo: view.topAnchor),
            host.view.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
        host.didMove(toParent: self)
    }

    // MARK: - Lifecycle

    override func willBecomeActive(with conversation: MSConversation) {
        super.willBecomeActive(with: conversation)
        SnippetStore.shared.reload()
        route(for: conversation.selectedMessage)
    }

    override func didResignActive(with conversation: MSConversation) {
        super.didResignActive(with: conversation)
        SnippetPlayer.shared.stop()
    }

    override func didSelect(_ message: MSMessage, conversation: MSConversation) {
        super.didSelect(message, conversation: conversation)
        route(for: message)
    }

    override func didTransition(to presentationStyle: MSMessagesAppPresentationStyle) {
        super.didTransition(to: presentationStyle)
        if presentationStyle == .compact, case .received = state.screen {
            state.screen = .browse
        }
    }

    /// A tapped MIFS bubble opens the player; otherwise show search to create one.
    private func route(for message: MSMessage?) {
        if let message, !message.isPending, let url = message.url, let snippet = SnippetLink.snippet(from: url) {
            state.screen = .received(snippet)
        } else {
            state.screen = .browse
        }
    }

    // MARK: - Sending

    private func send(_ snippet: Snippet) async throws {
        guard let conversation = activeConversation else { throw ExtensionError.noConversation }
        switch snippet.track.kind {
        case .catalog:
            guard let message = await MessageFactory.message(for: snippet) else { throw ExtensionError.unsendable }
            try await conversation.insert(message)
        case .file:
            guard let clip = snippet.clipURL else { throw ExtensionError.unsendable }
            try await conversation.insertAttachment(clip, withAlternateFilename: snippet.attachmentName)
        }
        Haptics.success()
        state.path = []
        state.catalog.query = ""
        requestPresentationStyle(.compact)
    }
}

enum ExtensionError: LocalizedError {
    case noConversation
    case unsendable

    var errorDescription: String? {
        switch self {
        case .noConversation: "Open a conversation to send a snippet."
        case .unsendable: "This snippet can't be sent anymore."
        }
    }
}

import Foundation

/// Carries a mif into Telegram, where the MIFS bot (see `telegram/`) sends it as a card and the Mini App plays it.
///
/// Only the mif's ID travels; the bot loads the song, moment, artwork and audio from the MIFS music server.
///
/// Format (mirrored by `telegram/public/snippet-code.js`): `<intent>2_<mifID>`, e.g. `p2_abcdefghijkl`.
nonisolated enum TelegramLink {
    enum Intent: String, Sendable {
        /// Opened by the sender from MIFS: the Mini App offers to send the snippet to a chat.
        case send = "s"
        /// Opened from a snippet card in a chat: the Mini App plays it.
        case play = "p"
    }

    static let musicServerHost = "mifs.cgn.fi"

    /// The bot whose Main Mini App is MIFS, from the `MIFS_TELEGRAM_BOT` build setting.
    static var botUsername: String {
        Bundle.main.object(forInfoDictionaryKey: "MIFSTelegramBot") as? String ?? ""
    }

    static var isConfigured: Bool {
        botUsername.wholeMatch(of: /[A-Za-z0-9_]{5,32}/) != nil
    }

    /// The MIFS Telegram server, from the `MIFS_TELEGRAM_HOST` build setting.
    static var serverHost: String {
        Bundle.main.object(forInfoDictionaryKey: "MIFSTelegramHost") as? String ?? ""
    }

    /// The Mini App hands back to MIFS after a send with `https://<server>/app/sent` (a universal link),
    /// or `mifs://telegram/sent` from the server's fallback page.
    static func isReturnFromSend(_ url: URL) -> Bool {
        switch url.scheme {
        case "https": !serverHost.isEmpty && url.host() == serverHost && url.path() == "/app/sent"
        case "mifs": url.host() == "telegram" && url.path() == "/sent"
        default: false
        }
    }

    /// The code for a mif shared from the bot's music server; nil for anything else, which Telegram can't play.
    static func startParameter(for snippet: Snippet, intent: Intent) -> String? {
        guard snippet.track.kind == .server, let page = snippet.shareURL,
              page.scheme == "https", page.host() == musicServerHost,
              page.port == nil || page.port == 443, page.user() == nil,
              page.pathComponents.count == 3, page.pathComponents[1] == "m",
              page.lastPathComponent.wholeMatch(of: /[a-z2-7]{12}/) != nil else { return nil }
        return "\(intent.rawValue)2_\(page.lastPathComponent)"
    }

    /// Whether the snippet can be sent in Telegram: a mif the MIFS bot can play.
    static func canSend(_ snippet: Snippet) -> Bool { appURL(for: snippet) != nil }

    /// Opens the MIFS Mini App in the Telegram app, which goes straight to Telegram's share sheet: a preview of
    /// the mif's card, sent as soon as a chat is picked.
    static func appURL(for snippet: Snippet) -> URL? {
        guard isConfigured, let parameter = startParameter(for: snippet, intent: .send) else { return nil }
        var components = URLComponents()
        components.scheme = "tg"
        components.host = "resolve"
        components.queryItems = [
            URLQueryItem(name: "domain", value: botUsername),
            URLQueryItem(name: "startapp", value: parameter),
            URLQueryItem(name: "mode", value: "compact"),
        ]
        return components.url
    }

    /// The same, as a t.me link for devices without Telegram (offers the app or Telegram Web).
    static func webURL(for snippet: Snippet) -> URL? {
        guard isConfigured, let parameter = startParameter(for: snippet, intent: .send) else { return nil }
        var components = URLComponents(string: "https://t.me/")!
        components.path = "/\(botUsername)"
        components.queryItems = [
            URLQueryItem(name: "startapp", value: parameter),
            URLQueryItem(name: "mode", value: "compact"),
        ]
        return components.url
    }

}

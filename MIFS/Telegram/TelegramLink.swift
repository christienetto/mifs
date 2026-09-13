import Foundation

/// Carries a catalog snippet into Telegram, where the MIFS Mini App (see `telegram/`) plays and sends it.
///
/// Telegram passes a link's `startapp` value to the Mini App but allows only 512 characters of
/// `A-Z a-z 0-9 _ -`, so just enough to rebuild the snippet travels: the song's catalog ID and
/// storefront, the moment, and the waveform. The Mini App looks everything else up in Apple's catalog.
///
/// Format (mirrored by `telegram/public/snippet-code.js`):
/// `<intent>1_<trackID>_<storefront>_<startMs>_<durationMs>_<waveform>`, e.g. `s1_1499378607_us_12345_10000_7fa3…`.
nonisolated enum TelegramLink {
    enum Intent: String, Sendable {
        /// Opened by the sender from MIFS: the Mini App offers to send the snippet to a chat.
        case send = "s"
        /// Opened from a snippet card in a chat: the Mini App plays it.
        case play = "p"
    }

    struct Payload: Equatable, Sendable {
        var intent: Intent
        var trackID: String
        var storefront: String
        var startMilliseconds: Int
        var durationMilliseconds: Int
        var waveform: [Float]
    }

    static let version = 1

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

    static func startParameter(for snippet: Snippet, intent: Intent) -> String? {
        let track = snippet.track
        guard track.kind == .catalog, track.previewURL != nil,
              track.id.wholeMatch(of: /[0-9]{1,15}/) != nil else { return nil }
        return [
            "\(intent.rawValue)\(version)",
            track.id,
            storefront(for: track),
            String(Int((snippet.start * 1000).rounded())),
            String(Int((snippet.duration * 1000).rounded())),
            SnippetLink.encode(Array(snippet.waveform.prefix(64))),
        ].joined(separator: "_")
    }

    static func payload(from parameter: String) -> Payload? {
        let parts = parameter.split(separator: "_", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 6,
              parts[0].count == 2, parts[0].hasSuffix(String(version)),
              let intent = Intent(rawValue: String(parts[0].prefix(1))),
              parts[1].wholeMatch(of: /[0-9]{1,15}/) != nil,
              parts[2].wholeMatch(of: /[a-z]{2}/) != nil,
              parts[3].wholeMatch(of: /[0-9]{1,6}/) != nil, parts[4].wholeMatch(of: /[0-9]{1,5}/) != nil,
              let start = Int(parts[3]), let duration = Int(parts[4]), duration > 0,
              parts[5].wholeMatch(of: /[0-9a-f]{0,64}/) != nil else { return nil }
        return Payload(
            intent: intent,
            trackID: parts[1],
            storefront: parts[2],
            startMilliseconds: start,
            durationMilliseconds: duration,
            waveform: SnippetLink.decode(parts[5])
        )
    }

    /// Opens the MIFS Mini App inside the Telegram app.
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

    /// The storefront the song was found in, so the recipient's lookup finds the same catalog entry.
    private static func storefront(for track: Track) -> String {
        if let first = track.appleMusicURL?.pathComponents.dropFirst().first?.lowercased(),
           first.wholeMatch(of: /[a-z]{2}/) != nil {
            return first
        }
        let current = CatalogService.shared.storefront
        return current.wholeMatch(of: /[a-z]{2}/) != nil ? current : "us"
    }
}

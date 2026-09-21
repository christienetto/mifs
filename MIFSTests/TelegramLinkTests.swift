import Foundation
import Testing
@testable import MIFS

struct TelegramLinkTests {
    private func serverSnippet() -> Snippet {
        var snippet = Snippet(
            track: Track(
                id: "6tm2k45nfdvm",
                kind: .server,
                title: "God Is",
                artist: "Kanye West",
                artworkURL: URL(string: "https://mifs.cgn.fi/media/artwork/e1/e1.jpg"),
                previewURL: URL(string: "https://mifs.cgn.fi/media/audio/ab/ab.m4a")
            ),
            start: 60,
            duration: 10,
            waveform: [0, 0.5, 1, 0.25]
        )
        snippet.shareURL = URL(string: "https://mifs.cgn.fi/m/abcdefghijkl")
        return snippet
    }

    /// The same codes are asserted in telegram/test/snippet-code.test.js, keeping both encoders in step.
    @Test func serverMifUsesBotCardCode() throws {
        let snippet = serverSnippet()
        #expect(TelegramLink.startParameter(for: snippet, intent: .send) == "s2_abcdefghijkl")
        #expect(TelegramLink.startParameter(for: snippet, intent: .play) == "p2_abcdefghijkl")
    }

    @Test func serverMifsMustUseTheBotsMusicServer() {
        var snippet = serverSnippet()
        for page in ["https://localhost/m/abcdefghijkl", "http://mifs.cgn.fi/m/abcdefghijkl",
                     "https://mifs.cgn.fi:8080/m/abcdefghijkl", "https://mifs.cgn.fi/m/nope"] {
            snippet.shareURL = URL(string: page)
            #expect(TelegramLink.startParameter(for: snippet, intent: .send) == nil)
        }
        snippet.shareURL = nil
        #expect(TelegramLink.startParameter(for: snippet, intent: .send) == nil)
    }

    /// The bot only plays mifs from the music server; anything else goes through the share sheet.
    @Test func onlyServerMifsHaveATelegramLink() {
        for kind in [Track.Kind.catalog, .file] {
            var snippet = serverSnippet()
            snippet.track.kind = kind
            #expect(TelegramLink.startParameter(for: snippet, intent: .send) == nil)
            #expect(TelegramLink.appURL(for: snippet) == nil)
            #expect(!TelegramLink.canSend(snippet))
        }
        if TelegramLink.isConfigured { #expect(TelegramLink.canSend(serverSnippet())) }
    }

    @Test func recognisesTheHandBackFromTelegram() throws {
        let host = TelegramLink.serverHost
        try #require(!host.isEmpty)
        #expect(TelegramLink.isReturnFromSend(URL(string: "https://\(host)/app/sent")!))
        #expect(TelegramLink.isReturnFromSend(URL(string: "mifs://telegram/sent")!))
        #expect(!TelegramLink.isReturnFromSend(URL(string: "https://\(host)/")!))
        #expect(!TelegramLink.isReturnFromSend(URL(string: "https://example.com/app/sent")!))
        #expect(!TelegramLink.isReturnFromSend(URL(string: "mifs://import")!))
    }

    /// The Mini App opens Telegram's share sheet for `s2_` launches (telegram/public/app.js).
    @Test func opensTheMiniAppToShare() throws {
        try #require(TelegramLink.isConfigured)
        let app = try #require(TelegramLink.appURL(for: serverSnippet()))
        #expect(app.absoluteString == "tg://resolve?domain=\(TelegramLink.botUsername)&startapp=s2_abcdefghijkl&mode=compact")
    }

    @Test func fallsBackToTheMiniAppOnTheWeb() throws {
        try #require(TelegramLink.isConfigured)
        let web = try #require(TelegramLink.webURL(for: serverSnippet()))
        #expect(web.absoluteString == "https://t.me/\(TelegramLink.botUsername)?startapp=s2_abcdefghijkl&mode=compact")
    }
}

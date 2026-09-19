import Foundation
import Testing
@testable import MIFS

struct TelegramLinkTests {
    private func catalogSnippet() -> Snippet {
        Snippet(
            track: Track(
                id: "1499378607",
                kind: .catalog,
                title: "Blinding Lights",
                artist: "The Weeknd",
                artworkURL: URL(string: "https://is1-ssl.mzstatic.com/image/thumb/a/600x600bb.jpg"),
                previewURL: URL(string: "https://audio-ssl.itunes.apple.com/itunes-assets/x/mzaf_1.plus.aac.p.m4a"),
                appleMusicURL: URL(string: "https://music.apple.com/gb/album/blinding-lights/1499378108?i=1499378607&uo=4")
            ),
            start: 12.345,
            duration: 10,
            waveform: [0, 0.5, 1, 0.25]
        )
    }

    /// The same string is asserted in telegram/test/snippet-code.test.js, keeping both encoders in step.
    @Test func matchesTheMiniAppFormat() throws {
        let parameter = try #require(TelegramLink.startParameter(for: catalogSnippet(), intent: .send))
        #expect(parameter == "s1_1499378607_gb_12345_10000_08f4")
    }

    @Test func roundTrips() throws {
        var snippet = catalogSnippet()
        snippet.waveform = (0..<Snippet.waveformBars).map { Float($0 % 16) / 15 }
        let parameter = try #require(TelegramLink.startParameter(for: snippet, intent: .play))
        let payload = try #require(TelegramLink.payload(from: parameter))

        #expect(payload.intent == .play)
        #expect(payload.trackID == "1499378607")
        #expect(payload.storefront == "gb")
        #expect(payload.startMilliseconds == 12_345)
        #expect(payload.durationMilliseconds == 10_000)
        #expect(payload.waveform.count == Snippet.waveformBars)
        #expect(zip(payload.waveform, snippet.waveform).allSatisfy { abs($0 - $1) < 0.05 })
    }

    /// Telegram only passes start parameters of up to 512 characters of A-Z a-z 0-9 _ -.
    @Test func fitsTelegramStartParameterRules() throws {
        var snippet = catalogSnippet()
        snippet.waveform = Array(repeating: 1, count: Snippet.waveformBars)
        snippet.start = 29.999
        snippet.duration = 15
        let parameter = try #require(TelegramLink.startParameter(for: snippet, intent: .send))
        #expect(parameter.count <= 512)
        #expect(parameter.wholeMatch(of: /[A-Za-z0-9_-]+/) != nil)
    }

    @Test func rejectsMalformedParameters() {
        for parameter in [
            "", "s1", "x1_1_us_0_10000_", "s2_1499378607_us_0_10000_ff",
            "s1_abc_us_0_10000_ff", "s1_1_USA_0_10000_ff", "s1_1_us_0_0_ff", "s1_1_us_0_10000_zz",
            "s1_1_us_0_10000_ff_extra",
        ] {
            #expect(TelegramLink.payload(from: parameter) == nil, "\(parameter)")
        }
    }

    @Test func serverMifUsesBotCardCode() throws {
        var snippet = catalogSnippet()
        snippet.track.kind = .server
        snippet.shareURL = URL(string: "https://music.example/m/abcdefghijkl")
        #expect(TelegramLink.startParameter(for: snippet, intent: .send) == "s2_abcdefghijkl")
        #expect(TelegramLink.startParameter(for: snippet, intent: .play) == "p2_abcdefghijkl")
        #expect(TelegramLink.appURL(for: snippet)?.absoluteString.contains("startapp=s2_abcdefghijkl") == true)
    }

    @Test func ownedAudioHasNoTelegramLink() {
        var snippet = catalogSnippet()
        snippet.track.kind = .file
        #expect(TelegramLink.startParameter(for: snippet, intent: .send) == nil)
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

    @Test func buildsAppAndWebLinksForTheConfiguredBot() throws {
        try #require(TelegramLink.isConfigured)
        let bot = TelegramLink.botUsername
        let app = try #require(TelegramLink.appURL(for: catalogSnippet()))
        #expect(app.absoluteString == "tg://resolve?domain=\(bot)&startapp=s1_1499378607_gb_12345_10000_08f4&mode=compact")
        let web = try #require(TelegramLink.webURL(for: catalogSnippet()))
        #expect(web.absoluteString == "https://t.me/\(bot)?startapp=s1_1499378607_gb_12345_10000_08f4&mode=compact")
    }
}

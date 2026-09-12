import Foundation
import Testing
@testable import MIFS

struct SnippetLinkTests {
    private func catalogSnippet() -> Snippet {
        Snippet(
            track: Track(
                id: "1499378607",
                kind: .catalog,
                title: "Blinding Lights & More",
                artist: "The Weeknd",
                artworkURL: URL(string: "https://is1-ssl.mzstatic.com/image/thumb/a/600x600bb.jpg"),
                previewURL: URL(string: "https://audio-ssl.itunes.apple.com/itunes-assets/x/mzaf_1.plus.aac.p.m4a"),
                appleMusicURL: URL(string: "https://music.apple.com/us/album/blinding-lights/1499378108?i=1499378607&uo=4"),
                isExplicit: true
            ),
            start: 12.345,
            duration: 10,
            waveform: [0, 0.5, 1, 0.25]
        )
    }

    @Test func roundTripsCatalogSnippet() throws {
        let original = catalogSnippet()
        let url = try #require(SnippetLink.url(for: original))
        let decoded = try #require(SnippetLink.snippet(from: url))

        #expect(decoded.track.id == original.track.id)
        #expect(decoded.track.title == original.track.title)
        #expect(decoded.track.artist == original.track.artist)
        #expect(decoded.track.previewURL == original.track.previewURL)
        #expect(decoded.track.artworkURL == original.track.artworkURL)
        #expect(decoded.track.isExplicit)
        #expect(abs(decoded.start - 12.345) < 0.001)
        #expect(decoded.duration == 10)
        #expect(decoded.waveform.count == 4)
        #expect(abs(decoded.waveform[1] - 0.5) < 0.05)
    }

    @Test func fallbackURLOpensSongInAppleMusic() throws {
        let url = try #require(SnippetLink.url(for: catalogSnippet()))
        #expect(url.host() == "music.apple.com")
        #expect(url.path() == "/us/album/blinding-lights/1499378108")
        let decoded = try #require(SnippetLink.snippet(from: url))
        let appleMusic = try #require(decoded.track.appleMusicURL)
        #expect(appleMusic.absoluteString == "https://music.apple.com/us/album/blinding-lights/1499378108?i=1499378607")
    }

    @Test func payloadStaysSmall() throws {
        var snippet = catalogSnippet()
        snippet.waveform = Array(repeating: 0.7, count: Snippet.waveformBars)
        let url = try #require(SnippetLink.url(for: snippet))
        #expect(url.absoluteString.utf8.count < 1_500)
    }

    @Test func ignoresForeignURLs() {
        #expect(SnippetLink.snippet(from: URL(string: "https://music.apple.com/us/album/x/1?i=2")!) == nil)
    }

    @Test func fileSnippetsHaveNoLink() {
        var snippet = catalogSnippet()
        snippet.track.kind = .file
        #expect(SnippetLink.url(for: snippet) == nil)
    }
}

struct WaveformTests {
    @Test func findsLoudestWindow() {
        var levels = Array(repeating: Float(0.1), count: 300)
        for index in 150..<250 { levels[index] = 1 }
        let waveform = Waveform(levels: levels, duration: 30)
        #expect(abs(waveform.loudestWindow(length: 10) - 15) < 0.01)
    }

    @Test func resamplesSnippetLevels() {
        let waveform = Waveform(levels: (0..<300).map { Float($0 % 10) / 10 }, duration: 30)
        let bars = waveform.levels(from: 5, duration: 10, bars: Snippet.waveformBars)
        #expect(bars.count == Snippet.waveformBars)
        #expect(bars.allSatisfy { (0...1).contains($0) })
        #expect(bars.max() == 1)
    }

    @Test func handlesSelectionPastEnd() {
        let waveform = Waveform(levels: Array(repeating: 0.5, count: 50), duration: 5)
        #expect(waveform.levels(from: 4, duration: 10, bars: 8).count == 8)
    }
}

import Foundation
import Testing
@testable import MIFS

struct MusicServerTests {
    /// Shape of GET /v1/songs (server/internal/api).
    private let songList = """
    {"songs": [
      {"id": "neon-harbor", "title": "Neon Harbor", "artist": "Orchid Relay", "album": "Night Signals",
       "trackNumber": 1, "releaseYear": 2026, "genre": "Synthwave", "explicit": false, "durationMs": 141360,
       "highlightStartMs": 52500, "hasLyrics": true,
       "audio": {"url": "http://localhost:8080/media/audio/ab/ab.m4a", "contentType": "audio/mp4", "bitrate": 256000, "size": 4500000},
       "artwork": {"url": "http://localhost:8080/media/artwork/cd/cd.jpg", "thumbnailUrl": "http://localhost:8080/media/artwork/ef/ef.jpg"},
       "license": {"name": "CC0-1.0"}},
      {"id": "paper-satellites", "title": "Paper Satellites", "artist": "Juniper Fold", "explicit": true,
       "durationMs": 120000, "hasLyrics": false,
       "audio": {"url": "http://localhost:8080/media/audio/12/12.m4a", "contentType": "audio/mp4", "size": 1}}
    ]}
    """

    @Test func decodesSongsIntoServerTracks() throws {
        let tracks = try JSONDecoder().decode(SongList.self, from: Data(songList.utf8)).songs.compactMap(\.track)
        #expect(tracks.count == 2)

        let neon = tracks[0]
        #expect(neon.kind == .server)
        #expect(neon.id == "neon-harbor")
        #expect(neon.album == "Night Signals")
        #expect(neon.previewURL?.absoluteString == "http://localhost:8080/media/audio/ab/ab.m4a")
        #expect(neon.artworkURL?.lastPathComponent == "cd.jpg")
        #expect(neon.thumbnailURL?.lastPathComponent == "ef.jpg")
        #expect(neon.highlightStart == 52.5)

        let single = tracks[1]
        #expect(single.album == nil && single.artworkURL == nil && single.highlightStart == nil)
        #expect(single.isExplicit)
    }

    /// Shape of GET /v1/search: a ready MIFS song and a Spotify track MIFS doesn't have yet.
    @Test func decodesSearchResults() throws {
        let json = """
        {"results": [
          {"ref": "mifs:neon-harbor", "songId": "neon-harbor", "status": "ready", "source": "spotify",
           "title": "Neon Harbor", "artist": "Orchid Relay", "explicit": false,
           "song": {"id": "neon-harbor", "status": "ready", "title": "Neon Harbor", "artist": "Orchid Relay",
                    "explicit": false, "durationMs": 1000, "hasLyrics": true,
                    "audio": {"url": "http://localhost:8080/media/audio/ab/ab.m4a", "contentType": "audio/mp4", "size": 1}}},
          {"ref": "spotify:0VjIjW4GlUZAMYd2vXMi3b", "status": "new", "source": "spotify",
           "title": "Blinding Lights", "artist": "The Weeknd", "album": "After Hours", "durationMs": 200040,
           "explicit": false, "isrc": "USUG11904206",
           "artwork": {"url": "https://i.scdn.co/640.jpg", "thumbnailUrl": "https://i.scdn.co/300.jpg"}}
        ], "incomplete": true}
        """
        let results = try JSONDecoder().decode(SearchResponseProbe.self, from: Data(json.utf8)).results
        #expect(results.count == 2)
        #expect(results[0].isReady && results[0].song?.track?.id == "neon-harbor")
        #expect(!results[1].isReady && results[1].sourceName == "Spotify" && results[1].id == "spotify:0VjIjW4GlUZAMYd2vXMi3b")
        #expect(results[1].artwork?.thumbnailUrl.lastPathComponent == "300.jpg")
    }

    /// A song still being added has no audio, so it isn't a playable track yet.
    @Test func pendingSongsAreNotTracks() throws {
        let json = """
        {"id": "vnac4yqo3rml", "status": "processing", "title": "Blinding Lights", "artist": "The Weeknd",
         "explicit": false, "durationMs": 200000, "hasLyrics": false}
        """
        let song = try JSONDecoder().decode(SongList.Song.self, from: Data(json.utf8))
        #expect(song.track == nil)
    }

    @Test func readsConfiguredURLFromInfoPlist() {
        #expect(MusicServer.configuredURL?.scheme == "http" || MusicServer.configuredURL?.scheme == "https")
    }
}

struct LyricLineTests {
    private let lines = [
        LyricLine(start: 10, end: 12, text: "one"),
        LyricLine(start: 12, end: 15, text: "two"),
        LyricLine(start: 20, end: 20.6, text: "short"),
    ]

    @Test func selectsLinesHeardInRange() {
        #expect(lines.heard(from: 11, to: 16).map(\.text) == ["one", "two"])
        // Only the last 0.3 s of "one" is heard: not enough to count.
        #expect(lines.heard(from: 11.7, to: 16).map(\.text) == ["two"])
        // Half of a short line counts.
        #expect(lines.heard(from: 20.3, to: 25).map(\.text) == ["short"])
        #expect(lines.heard(from: 15, to: 20).isEmpty)
    }

    @Test func findsLineBeingSung() {
        #expect(lines.index(at: 12) == 1)
        #expect(lines.index(at: 9.9) == nil)
        #expect(lines.index(at: 17) == nil)
    }
}

struct ServerSnippetLinkTests {
    private func serverSnippet() -> Snippet {
        Snippet(
            track: Track(
                id: "neon-harbor",
                kind: .server,
                title: "Neon Harbor",
                artist: "Orchid Relay",
                artworkURL: URL(string: "http://192.168.1.20:8080/media/artwork/cd/cd.jpg"),
                previewURL: URL(string: "http://192.168.1.20:8080/media/audio/ab/ab.m4a")
            ),
            start: 52.25,
            duration: 10,
            waveform: [0.2, 0.9],
            lyrics: [
                LyricLine(start: 52.5, end: 55.1, text: "Lights on the harbor"),
                LyricLine(start: 55.1, end: 58, text: "Send me the part - where it all begins"),
            ]
        )
    }

    @Test func roundTripsServerSnippetWithLyrics() throws {
        let original = serverSnippet()
        let url = try #require(SnippetLink.url(for: original))
        // Devices without MIFS open the song's audio.
        #expect(url.host() == "192.168.1.20" && url.path() == "/media/audio/ab/ab.m4a")

        let decoded = try #require(SnippetLink.snippet(from: url))
        #expect(decoded.track.kind == .server)
        #expect(decoded.track.id == "neon-harbor")
        #expect(decoded.track.previewURL == original.track.previewURL)
        #expect(decoded.track.appleMusicURL == nil)
        #expect(decoded.start == 52.25)
        #expect(decoded.lyrics == original.lyrics)
        #expect(decoded.playback?.url == original.track.previewURL)
    }

    /// With a mif, devices without MIFS open its page; the app still reads the snippet from
    /// the `mifs_*` items, as older versions do.
    @Test func serverSnippetLinksToItsMifPage() throws {
        var original = serverSnippet()
        original.shareURL = URL(string: "http://192.168.1.20:8080/m/xsgdjktq7w66")
        let url = try #require(SnippetLink.url(for: original))
        #expect(url.path() == "/m/xsgdjktq7w66")

        let decoded = try #require(SnippetLink.snippet(from: url))
        #expect(decoded.shareURL == original.shareURL)
        #expect(decoded.track.previewURL == original.track.previewURL)
        #expect(decoded.playback?.url.path() == "/v1/mifs/xsgdjktq7w66/audio")
        #expect(decoded.playback?.start == 0)
        #expect(decoded.lyrics == original.lyrics)
    }

    @Test func catalogSnippetsStayCatalog() throws {
        var snippet = serverSnippet()
        snippet.track.kind = .catalog
        snippet.lyrics = nil
        let decoded = try #require(SnippetLink.url(for: snippet).flatMap(SnippetLink.snippet(from:)))
        #expect(decoded.track.kind == .catalog)
        #expect(decoded.lyrics == nil)
    }

    @Test func payloadStaysSmallWithLyrics() throws {
        var snippet = serverSnippet()
        snippet.waveform = Array(repeating: 0.7, count: Snippet.waveformBars)
        snippet.lyrics = (0..<10).map { LyricLine(start: Double($0), end: Double($0) + 1, text: String(repeating: "word ", count: 9)) }
        let url = try #require(SnippetLink.url(for: snippet))
        #expect(url.absoluteString.utf8.count < 1_500)
        #expect(SnippetLink.snippet(from: url)?.lyrics?.count == 6)
    }

    /// Snippet history written before server songs existed must still load.
    @Test func decodesHistoryWithoutNewFields() throws {
        let legacy = """
        [{"id": "6F9619FF-8B86-D011-B42D-00C04FC964FF", "start": 1, "duration": 5, "waveform": [0.5],
          "createdAt": 700000000,
          "track": {"id": "1", "kind": "catalog", "title": "T", "artist": "A", "isExplicit": false,
                    "previewURL": "https://example.com/p.m4a"}}]
        """
        let snippets = try JSONDecoder().decode([Snippet].self, from: Data(legacy.utf8))
        #expect(snippets.first?.lyrics == nil)
        #expect(snippets.first?.track.thumbnailURL == nil)
    }
}

/// Mirrors the private wire type so tests can decode a whole search response.
private struct SearchResponseProbe: Decodable {
    let results: [ServerSearchResult]
}

@MainActor
struct PreparationFlowTests {
    @Test func downloadProgressReplacesTimelineUntilReady() async throws {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [PreparationProtocol.self]
        let session = URLSession(configuration: config)
        defer { session.invalidateAndCancel() }
        let server = MusicServer(baseURL: URL(string: "https://mifs.test"), session: session)
        let track = Track(id: "spotify:test", kind: .catalog, title: "Test", artist: "Artist",
            preparationRef: "spotify:test", expectedDuration: 60)
        let editor = SnippetEditorModel(track: track, server: server)
        let loading = Task { await editor.load() }
        defer { loading.cancel() }
        let deadline = ContinuousClock.now + .seconds(3)
        while editor.downloadFraction == nil && ContinuousClock.now < deadline { await Task.yield() }
        #expect(editor.isPreparing)
        #expect(editor.phase == .loading)
        #expect(editor.downloadFraction == 0.5)
        #expect(!editor.canSend)
        await loading.value
        defer { editor.stopPreview() }
        #expect(editor.track.kind == .server)
        editor.setRange(start: 17.25, end: 24.5)
        editor.resizeStart(to: 19)
        #expect(editor.start == 19)
        #expect(editor.start + editor.length == 24.5)
        editor.resizeEnd(to: 80)
        #expect(editor.length == 20)
        editor.resizeStart(to: 0)
        #expect(editor.length == 20)
        editor.resizeEnd(to: 18)
        #expect(editor.length == 1)
        #expect(editor.canSend)
        #expect(editor.lyrics.first?.text == "Test lyric")
        editor.setRange(start: 58, end: 90)
        #expect(editor.length == 20)
        #expect(editor.start == 40)
    }
}

private nonisolated final class PreparationProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "mifs.test" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let path = request.url!.path
        let json: String
        if path.hasSuffix("/lyrics") {
            json = #"{"lines":[{"startMs":17000,"endMs":25000,"text":"Test lyric"}]}"#
        } else if path.hasSuffix("/waveform") {
            json = #"{"durationMs":60000,"pointsPerSecond":10,"rms":[0.1,0.8,0.4]}"#
        } else if request.httpMethod == "POST" {
            json = #"{"id":"song","status":"processing","downloadedBytes":5000,"downloadTotalBytes":10000,"title":"Test","artist":"Artist","explicit":false}"#
        } else {
            json = #"{"id":"song","status":"ready","title":"Test","artist":"Artist","explicit":false,"audio":{"url":"https://mifs.test/media/audio.m4a"}}"#
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type":"application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(json.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

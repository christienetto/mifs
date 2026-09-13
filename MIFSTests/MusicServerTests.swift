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
        let tracks = try JSONDecoder().decode(SongList.self, from: Data(songList.utf8)).songs.map(\.track)
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

import XCTest

/// Search → a song MIFS doesn't have (found on Spotify/Deezer by the server) → MIFS adds it →
/// editor with synced lyrics. Needs the local server with an audio source, e.g.
/// `MIFS_AUDIO_COMMAND=scripts/test-tone.sh make -C server run`; skipped when unreachable.
final class DiscoveryFlowUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    func testAddSongFromProviderSearch() throws {
        let app = XCUIApplication()
        app.launch()

        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        search.tap()
        search.typeText("Blinding Lights The Weeknd")

        // A provider result, e.g. "The Weeknd · Deezer".
        let result = app.buttons.containing(NSPredicate(format: "label CONTAINS[c] 'Blinding Lights' AND (label CONTAINS 'Deezer' OR label CONTAINS 'Spotify')")).firstMatch
        let found = result.waitForExistence(timeout: 20)
        try XCTSkipUnless(found, "No provider results; start the server with discovery on (`make -C server run`)")
        capture(app, "discovery-results")
        result.tap()
        capture(app, "discovery-adding")

        // MIFS adds the song (audio, waveform, lyrics), then the editor opens.
        let send = app.buttons["Send in Messages"]
        let unavailable = app.staticTexts["Unavailable"]
        let deadline = Date().addingTimeInterval(90)
        while !send.exists, !unavailable.exists, Date() < deadline {
            RunLoop.current.run(until: Date().addingTimeInterval(0.5))
        }
        try XCTSkipIf(unavailable.exists, "The server has no audio source for this song (set MIFS_AUDIO_COMMAND or MIFS_LIBRARY_DIR)")
        XCTAssertTrue(send.exists, "The editor should open once MIFS has added the song")
        let lines = app.buttons.matching(identifier: "lyric-line")
        XCTAssertTrue(lines.firstMatch.waitForExistence(timeout: 10), "Synced lyrics should come with the added song")
        sleep(2)
        capture(app, "discovery-editor")
    }

    private func capture(_ app: XCUIApplication, _ name: String) {
        let shot = app.screenshot()
        let attachment = XCTAttachment(screenshot: shot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let directory = ProcessInfo.processInfo.environment["MIFS_SCREENSHOT_DIR"] {
            try? shot.pngRepresentation.write(to: URL(fileURLWithPath: directory).appendingPathComponent("\(name).png"))
        }
    }
}

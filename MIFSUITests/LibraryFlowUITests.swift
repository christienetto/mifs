import XCTest

/// MIFS Library → song → pick a lyric → send. Needs the local server (`make -C server run`);
/// skipped when the app can't reach it.
final class LibraryFlowUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    func testClipServerSongByLyric() throws {
        let app = XCUIApplication()
        app.launch()

        let song = app.buttons.containing(NSPredicate(format: "label CONTAINS[c] 'Neon Harbor'")).firstMatch
        let unreachable = app.staticTexts.containing(NSPredicate(format: "label CONTAINS[c] 'reach the MIFS server'")).firstMatch
        let deadline = Date().addingTimeInterval(15)
        while !song.exists, !unreachable.exists, Date() < deadline {
            RunLoop.current.run(until: Date().addingTimeInterval(0.25))
        }
        try XCTSkipUnless(song.exists, "MIFS server isn't reachable; start it with `make -C server run`")
        capture(app, "library")
        song.tap()

        let send = app.buttons["Send in Messages"]
        XCTAssertTrue(send.waitForExistence(timeout: 20))
        let lines = app.buttons.matching(identifier: "lyric-line")
        XCTAssertTrue(lines.firstMatch.waitForExistence(timeout: 5), "Synced lyrics should load from the server")
        XCTAssertGreaterThan(lines.count, 4)
        sleep(2)
        capture(app, "library-editor")

        // Tapping a lyric moves the selection to it.
        let scrubber = app.otherElements["Snippet start"]
        let before = scrubber.value as? String
        lines.element(boundBy: 2).tap()
        sleep(2)
        XCTAssertNotEqual(scrubber.value as? String, before, "Selection should move to the tapped lyric")
        capture(app, "library-lyric-selected")

        send.tap()
        sleep(3)
        capture(app, "library-after-send")

        // The search also finds server songs.
        app.terminate()
        app.launch()
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        search.tap()
        search.typeText("Juniper")
        XCTAssertTrue(app.buttons.containing(NSPredicate(format: "label CONTAINS[c] 'Paper Satellites'")).firstMatch
            .waitForExistence(timeout: 10))
        capture(app, "library-search")
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

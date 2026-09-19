import XCTest

/// MIFS Library → song → pick a lyric → send. Needs the local server (`make -C server run`);
/// skipped when the app can't reach it.
final class LibraryFlowUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    func testClipServerSongByLyric() throws {
        let app = XCUIApplication()
        if let server = ProcessInfo.processInfo.environment["MIFS_SERVER_URL"] {
            app.launchArguments += ["-MIFSServerURL", server]
        }
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

        let send = app.buttons["Share"]
        XCTAssertTrue(send.waitForExistence(timeout: 20))
        let lines = app.buttons.matching(identifier: "lyric-line")
        XCTAssertTrue(lines.firstMatch.waitForExistence(timeout: 5), "Synced lyrics should load from the server")
        XCTAssertGreaterThan(lines.count, 4)
        sleep(2)
        capture(app, "library-editor")
        XCTAssertFalse(app.buttons["Custom Range…"].exists)
        XCTAssertTrue(app.otherElements["Selection start handle"].exists)
        XCTAssertTrue(app.otherElements["Selection end handle"].exists)
        let handle = app.otherElements["Selection end handle"]
        let from = handle.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        from.press(forDuration: 0.1, thenDragTo: from.withOffset(CGVector(dx: 30, dy: 0)))

        // Tapping a lyric moves the selection to it.
        let scrubber = app.otherElements["Snippet start"]
        let before = scrubber.value as? String
        lines.element(boundBy: 2).tap()
        sleep(2)
        XCTAssertNotEqual(scrubber.value as? String, before, "Selection should move to the tapped lyric")
        capture(app, "library-lyric-selected")

        send.tap()
        XCTAssertTrue(app.navigationBars["Share Mif"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Messages"].exists)
        XCTAssertTrue(app.buttons["Telegram"].exists)
        app.buttons["Cancel"].tap()
        capture(app, "library-after-send")

        // Recent replaces the old Snippets section.
        app.terminate()
        app.launch()
        XCTAssertTrue(app.tabBars.buttons["Recent"].exists)
        app.tabBars.buttons["Recent"].tap()
        let recent = app.descendants(matching: .any).matching(identifier: "recent-mif-neon-harbor").firstMatch
        XCTAssertTrue(recent.waitForExistence(timeout: 5))
        recent.tap()
        XCTAssertTrue(app.buttons.matching(identifier: "lyric-line").firstMatch.waitForExistence(timeout: 5))
        capture(app, "recent-mif-playback")
        app.navigationBars.buttons["Recent"].tap()
        XCTAssertTrue(recent.waitForExistence(timeout: 5))
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

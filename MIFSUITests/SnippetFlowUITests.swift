import XCTest

final class SnippetFlowUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    /// Search → open a song → the editor loads a waveform and offers sending.
    func testSearchAndEditSnippet() throws {
        let app = XCUIApplication()
        app.launch()

        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        search.tap()
        search.typeText("Blinding Lights")
        capture(app, "search")

        let result = app.buttons.containing(NSPredicate(format: "label CONTAINS[c] 'Blinding Lights'")).firstMatch
        XCTAssertTrue(result.waitForExistence(timeout: 15))
        result.tap()

        let send = app.buttons["Send in Messages"]
        XCTAssertTrue(send.waitForExistence(timeout: 20))
        sleep(2)
        capture(app, "editor")

        app.buttons["15 seconds"].tap()
        let scrubber = app.otherElements["Snippet start"]
        if scrubber.exists {
            scrubber.swipeLeft()
        }
        sleep(2)
        capture(app, "editor-15s")

        send.tap()
        sleep(3)
        capture(app, "after-send")

        app.terminate()
        app.launch()
        app.tabBars.buttons["Snippets"].firstMatch.tap()
        sleep(2)
        capture(app, "snippets")
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

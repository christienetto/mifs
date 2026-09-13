import XCTest

/// Drives the simulator's Messages app: open MIFS from the app drawer, make and send a snippet,
/// then tap the bubble and expect the player. Messages' UI varies by OS, so it's opt-in
/// (TEST_RUNNER_MIFS_MESSAGES_UI=1) and expects a freshly installed build.
final class MessagesExtensionUITests: XCTestCase {
    private var directory: String? { ProcessInfo.processInfo.environment["MIFS_SCREENSHOT_DIR"] }
    private var screenshotPrefix = ""

    func testOpenMIFSInMessages() throws {
        try sendSnippetAndOpenBubble(search: "Blinding Lights Weeknd", title: "Blinding Lights", prefix: "")
    }

    /// A MIFS server song (needs `make -C server run`): the bubble opens its player with lyrics.
    func testSendServerSongInMessages() throws {
        try sendSnippetAndOpenBubble(search: "Neon Harbor", title: "Neon Harbor", prefix: "server-")
    }

    private func sendSnippetAndOpenBubble(search term: String, title: String, prefix: String) throws {
        try XCTSkipIf(ProcessInfo.processInfo.environment["MIFS_MESSAGES_UI"] == nil, "Set MIFS_MESSAGES_UI to run")
        screenshotPrefix = prefix
        let messages = XCUIApplication(bundleIdentifier: "com.apple.MobileSMS")
        messages.launch()
        sleep(3)
        dump(messages, "messages-0")

        messages.cells.firstMatch.tap()
        sleep(2)
        dump(messages, "messages-1")

        messages.buttons["add"].tap()
        sleep(2)
        dump(messages, "messages-2")

        let mifs = messages.cells.matching(NSPredicate(format: "identifier CONTAINS 'dev.kuchta.mifs'")).firstMatch
        for _ in 0..<4 where !mifs.isHittable {
            messages.cells.firstMatch.swipeUp()
            sleep(1)
        }
        dump(messages, "messages-3")
        XCTAssertTrue(mifs.exists, "MIFS not listed in the Messages app drawer")
        mifs.tap()
        sleep(4)
        dump(messages, "ext-compact")

        let search = messages.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        search.tap()
        sleep(2)
        search.typeText(term)
        sleep(4)
        dump(messages, "ext-search")

        let result = messages.buttons.containing(NSPredicate(format: "label CONTAINS[c] %@", title)).firstMatch
        XCTAssertTrue(result.waitForExistence(timeout: 15))
        result.tap()

        let add = messages.buttons["Add to Message"]
        XCTAssertTrue(add.waitForExistence(timeout: 20))
        sleep(2)
        dump(messages, "ext-editor")
        add.tap()
        sleep(4)
        dump(messages, "ext-staged")

        let send = messages.buttons.matching(NSPredicate(format: "identifier == 'sendButton' OR label == 'Send'")).firstMatch
        if send.waitForExistence(timeout: 5) { send.tap() }
        sleep(5)
        dump(messages, "ext-sent")

        // Reopen the conversation so the bubble is rendered fresh, as a recipient would see it.
        messages.buttons["BackButton"].tap()
        sleep(2)
        messages.cells.firstMatch.tap()
        sleep(8)
        dump(messages, "bubble-reopened")

        let bubble = messages.otherElements.matching(NSPredicate(format: "label CONTAINS[c] %@ OR label CONTAINS[c] 'snippet'", title)).firstMatch
        if bubble.exists { bubble.tap() } else { messages.coordinate(withNormalizedOffset: CGVector(dx: 0.7, dy: 0.3)).tap() }
        XCTAssertTrue(messages.buttons["Reply with a Snippet"].waitForExistence(timeout: 10), "Tapping the bubble should open the MIFS player")
        sleep(3)
        dump(messages, "bubble-opened")
    }

    private func dump(_ app: XCUIApplication, _ name: String) {
        let name = screenshotPrefix + name
        let shot = app.screenshot()
        let attachment = XCTAttachment(screenshot: shot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        guard let directory else { return }
        try? shot.pngRepresentation.write(to: URL(fileURLWithPath: directory).appendingPathComponent("\(name).png"))
        try? app.debugDescription.write(toFile: "\(directory)/\(name).txt", atomically: true, encoding: .utf8)
    }
}

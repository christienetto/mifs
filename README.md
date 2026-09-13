# MIFS

MIFS is for sending the best few seconds of a song through iMessage. Pick a song, scroll the
waveform (or tap a lyric) to choose a 5–15 second moment, and send it as an interactive
bubble. The recipient taps the bubble to play that moment.

Songs come from three places:

- **MIFS Library.** Full songs, artwork and synced lyrics from the MIFS music server in
  [`server/`](server/README.md), streamed on demand.
- **Apple Music.** Search and charts from Apple's public catalog, clipped from 30-second
  previews.
- **Your own audio.** Files or DRM-free library songs, sent as an audio attachment.

## Layout

| Path | |
| --- | --- |
| `MIFS/` | The iPhone app: Discover, Snippets, compose |
| `MIFSMessages/` | The iMessage extension: browse, clip and send, play received bubbles |
| `Shared/` | Code used by both: models, catalog and server clients, audio, editor, UI |
| `server/` | Go music server: API, SQLite catalog, ingest pipeline, demo songs |
| `Config/` | Build settings, including the server URL |
| `project.yml` | XcodeGen spec that `MIFS.xcodeproj` is generated from |

## Run the whole system locally

You need Xcode 26 with an iOS 18+ simulator, Go 1.25+ and ffmpeg (`brew install go ffmpeg`).

1. **Start the server** (it keeps running in the terminal):

   ```sh
   make -C server run
   ```

   This imports the three demo songs into `server/data/` and serves them on
   http://localhost:8080.

2. **Run the app.** Open `MIFS.xcodeproj`, choose the **MIFS** scheme and an iPhone
   simulator, then Run. **Discover › MIFS Library** lists the server's songs. Open one to
   see its lyrics beside the waveform, tap a line or drag the waveform to pick the moment,
   and send it.

3. **Try iMessage.** In the simulator's Messages app, open a conversation, tap **+**, then
   **MIFS**. Search or pick a MIFS Library song, choose **Add to Message**, send it, and tap
   the bubble to play the snippet with its lyrics.

### On a physical iPhone

The phone can't reach the Mac as `localhost`:

1. Run the server on all interfaces with `MIFS_ADDR=:8080 make -C server run`.
2. Copy `Config/Local.xcconfig.example` to `Config/Local.xcconfig` (git-ignored) and set
   `MIFS_SERVER_URL` to the Mac's LAN address, e.g. `http:/$()/192.168.1.20:8080`.
3. Build to the phone. Allow **Local Network** access when iOS asks.

For a quick test without rebuilding, pass the launch argument
`-MIFSServerURL http://<host>:8080` in the scheme. This affects the app only; the Messages
extension always uses the build setting.

## Tests

```sh
make -C server test       # Go: LRC parser, catalog/search, ingest (uses ffmpeg), HTTP API

xcodebuild test -project MIFS.xcodeproj -scheme MIFS \
  -destination 'platform=iOS Simulator,name=iPhone 17'
```

`LibraryFlowUITests` exercises the server flow and skips itself when the server isn't
running. The Messages UI tests are opt-in: run them with `TEST_RUNNER_MIFS_MESSAGES_UI=1`.

After editing `project.yml`, regenerate the project with `xcodegen generate`.

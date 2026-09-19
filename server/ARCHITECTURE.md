# MIFS architecture

## Discovery and preparation

```
Spotify search → result + existing server availability
                        │ tap
                        ├─ editor: download bar (reported bytes)
                        └─ POST /v1/songs → SQLite queue → spotDL → full AAC recording
                                                                   │
                                     waveform + artwork + lyrics ───┘
                                                                   │ ready
                                   full waveform + lyrics ◄───┘
```

`GET /v1/search` queries only Spotify. Provider links and ISRCs map results back to existing
canonical songs. Other provider adapters remain for legacy identities, but don't participate
in search. The empty Discover screen lists ready server recordings under Top Songs.

`POST /v1/songs` resolves identity and queues one job per song. Repeated selections reuse the
same job. Workers claim pending rows atomically, retry transient failures, and recover interrupted
jobs on restart. spotDL is the default audio source, with an optional local library first and an
explicit command override for tests or another acquisition method.

The worker runs spotDL with a validated track URL and an argument array. It checks duration,
normalizes audio, computes a 10 Hz RMS waveform, stores artwork and publishes the recording as
ready. Lyrics fetch concurrently and are exposed before the audio finishes. Final lyric timing
is fitted to the actual recording. Acquisition files are temporary; delivery audio is one
content-addressed AAC/MP4 recording with faststart metadata.

## Editor timeline

Selecting a missing Spotify song opens one editor and polls the server while downloading.
No preview is fetched or played. The waveform and selection controls stay hidden until the
full recording is ready. The spotDL wrapper reports yt-dlp byte progress, persisted in SQLite
and exposed as `downloadedBytes` / `downloadTotalBytes`. No percentage is invented when the
size is unknown. Transfer completion is followed by a finishing state until processing completes.

The existing scrolling waveform now has draggable start and end handles. Each edge holds the
opposite endpoint fixed and clamps the selection to 1–20 seconds inside the recording. Presets
remain available. One Share action opens a destination picker; Messages uses its interactive
bubble. Received and Recent mifs begin playback on open.

Telegram code changes are staged locally, but further Telegram work and deployment are paused
until the separate public music server is available.

## Sharing and playback

```
POST /v1/songs/{id}/mifs {startMs, durationMs}
       │
       └─ metadata row: song + immutable audio key + timestamps + lyric snapshot
               │
               ├─ /m/{id}                    browser share page
               ├─ /v1/mifs/{id}              metadata, no audio processing
               └─ /v1/mifs/{id}/audio        only the selected interval
```

Creating or opening a share is a database operation. It never renders audio or stores a clip.
The deterministic mif ID deduplicates repeated shares of the exact same recording and interval.
Pinning the immutable audio key and lyrics protects existing shares if a song is re-ingested.
Full recordings referenced by mifs must be retained.

On the first playback, ffmpeg seeks directly to the range and produces AAC in fragmented MP4
in memory. The client receives only that short interval. The response supports byte ranges,
ETags and immutable caching. Concurrent identical requests share one render, encoding concurrency
is bounded, and completed responses occupy at most 32 MiB per process. The memory cache is
evictable and disappears on restart. No clip files or clip blobs are written.

This trades a small first-play encoding cost for minimal persistent storage and instant share
creation. A CDN can cache the audio endpoint for popular shares. Full-song media can separately
use `MIFS_MEDIA_URL`; rendering still needs access to the pinned source recording.

## Clients and links

Recent stores metadata and links, not audio copies. Tapping an item opens its lyrics/player.
Incoming playback initially shows artwork, replacing it with lyrics when playing. Both the
Messages extension and the main app use the interval endpoint, with playback starting at zero;
lyrics retain absolute song timestamps.

Messages retain the backwards-compatible `mifs_*` URL payload. The canonical share page works
without an installed app. For native opening, configure a public HTTPS host in `MIFS_LINK_HOST`
and `MIFS_SERVER_URL`, and serve its Apple association file using `MIFS_IOS_APP_ID` on the server.
Localhost cannot verify Universal Links. The Telegram mini app is a separate client; sharing a
server mif uses its canonical URL.

## Validation and limitations

Tests cover Spotify-only search and reuse of known songs, queued ingestion, timestamp validation,
metadata-only sharing, playback duration, partial/cached audio responses, memory-only rendering,
download progress, selection edge limits, preparation states and legacy link compatibility.

Live Spotify/spotDL acquisition needs configured credentials, installed executables and upstream
availability. Preview and lyric availability are not guaranteed. No deployment or public-domain
association is assumed by the code. Existing legacy clip files are left in place for old URLs;
new shares create none.

## Local validation (2026-09-19)

A live Spotify search and spotDL 4.5.2 acquisition completed successfully with the configured
credentials. The test recording became ready in about 30 seconds, with 35 lyric lines available
during preparation. Creating a 7.25-second mif at 43.125 seconds took approximately 0.9 ms locally;
playback transferred 179,973 bytes and wrote no clip files. The delivered AAC container measured
7.268 seconds because of encoder padding; clients stop at the selected duration. These are local
measurements, not latency guarantees. The tested Spotify result did not include a preview URL.

iOS: 31 unit tests and the server-song UI flow passed on the iPhone 17 simulator, including
custom-range controls and opening lyrics from Recent. Go tests, race checks and vet passed.
Public Universal Links still require the deployment domain and signing configuration.

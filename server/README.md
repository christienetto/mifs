# MIFS music server

A small Go service that gives MIFS full songs, artwork, precomputed waveforms and
synchronised lyrics, so the app can clip any moment of a song and show the lyrics
alongside the timeline. It searches Spotify, downloads missing songs with spotDL when selected, and shares a
**mif**: a link referencing the full recording and a selected time range. No mif audio files
are stored; playback sends only the interval requested.

[ARCHITECTURE.md](ARCHITECTURE.md) explains the design: identity, on-demand ingestion,
storage estimates and the scaling path.

```
seed/<song>/                      mifs-server ingest            data/
  song.json  audio.*      ──────►  ffmpeg: AAC 256k, faststart ─►  media/audio/…/<sha256>.m4a
  artwork.*  lyrics.lrc            waveform (RMS, 10/s)            media/artwork/…/<sha256>.jpg
                                   artwork 1200 px + 300 px        mifs.db (SQLite + FTS5)
                                   LRC → lines with ends
                                                                   │
iOS app / Messages extension ◄── JSON + media over HTTP ◄── mifs-server serve
```

## Running it

Requirements: Go 1.25+ and `ffmpeg`/`ffprobe` (`brew install ffmpeg`) for importing songs and
playback. Install spotDL as well (`make setup`, or `pipx install spotdl`) for songs selected from Spotify.

```sh
cd server
make run      # imports seed/ into data/, then serves http://localhost:8080
```

`make seed` re-imports without serving, `make test` runs vet and the tests, and
`make clean` deletes `data/`. `data/` also holds downloaded songs and shared mif metadata. Back it up; deleting it
breaks existing links.

Check it's up with `curl localhost:8080/healthz`.

## Design

- **One static binary, no runtime dependencies.** The pure-Go SQLite driver
  (`modernc.org/sqlite`) avoids cgo, so the server cross-compiles for any Linux host.
  Ingestion and mif clips shell out to ffmpeg.
- **Discovery is separate from the catalog.** Search covers Spotify only, with each result marked with whether MIFS has it. Songs have MIFS's own IDs; provider tracks are
  links to them, matched by ISRC. Picking a song MIFS lacks creates it as `pending`, and a
  background worker gets its audio from the configured sources (see
  [Songs on demand](#songs-on-demand)).
- **SQLite holds metadata, lyrics and waveforms**, with an FTS5 index over title, artist,
  album and lyrics. Searching is a ranked prefix match (title beats artist beats album
  beats lyrics), so "send me the" finds a song by a line. WAL mode lets you ingest while
  the server is running. Thousands, or even hundreds of thousands, of songs need no
  changes. If you ever need several writers, the store is one package
  (`internal/catalog`) to port to Postgres.
- **Media is content-addressed and immutable.** Blobs are named by their SHA-256
  (`media/audio/3f/3fa9….m4a`), served with `Cache-Control: immutable` and range support,
  and never change in place. Re-importing a changed file produces a new URL. That makes
  it trivial to move media to S3/R2 plus a CDN: sync `data/media/` to the bucket and set
  `MIFS_MEDIA_URL`.
- **Streaming, not downloading.** Audio is AAC-in-MP4 with the index at the front, so
  AVPlayer seeks straight to the selected moment with HTTP range requests. The app never
  downloads a whole song to preview a 10-second clip.
- **The waveform is precomputed** with the same analysis the app runs on device (RMS of
  8 kHz mono at 10 points/s). The editor draws the whole song immediately. A missing waveform is reported as an error rather than downloading the whole recording.
- **Lyrics are authored as LRC, served as JSON.** LRC is the de facto format for
  line-synced lyrics and easy to edit by hand. Ingest parses it once, including `[offset]`,
  repeated timestamps and enhanced word stamps, and gives every line an explicit end. An
  empty timestamp line marks the end of a line before an instrumental break. Clients get
  typed `{startMs, endMs, text}` lines and never parse LRC.
- **Caching.** API responses carry a weak ETag with `Cache-Control: public, no-cache`, so
  URLSession's cache revalidates cheaply (304) and new imports show up immediately. Media
  is cacheable forever.
- **Absolute URLs.** Responses contain full media URLs. They're built from the request's
  host, so a simulator (`localhost`) and a phone (the Mac's LAN IP) both get working
  links. Set `MIFS_PUBLIC_URL` when deployed behind a proxy.

## API

All endpoints are public. `GET` endpoints also answer `HEAD`.

| Endpoint | Returns |
| --- | --- |
| `GET /healthz` | `{"status":"ok","songs":3}` (ready songs) |
| `GET /v1/search?q=&limit=` | `{"results":[Result…],"incomplete":false}`: Spotify tracks, with MIFS availability. `limit` is 1–50 (default 20). `incomplete` means a provider failed. |
| `GET /v1/songs?q=&limit=&cursor=` | `{"songs":[Song…],"nextCursor":"…"}`: **ready** songs only. Without `q`, all by title. `limit` is 1–100 (default 50). Pass `nextCursor` back as `cursor`. |
| `POST /v1/songs` | Body `{"ref":"spotify:0VjIjW4GlUZAMYd2vXMi3b"}` (a search result's `ref`). Returns the `Song`, `201` if MIFS just added it (status `pending`), else `200`. Idempotent. |
| `GET /v1/songs/{id}` | `Song`, with `links` to the song on providers. Poll it until `status` is `ready`. |
| `GET /v1/songs/{id}/lyrics` | `{"songId","synced":true,"lines":[{"startMs","endMs","text"}]}`, or 404 when the song has no lyrics |
| `GET /v1/songs/{id}/waveform` | `{"songId","durationMs","pointsPerSecond":10,"rms":[…]}`, or 409 until the song is ready |
| `POST /v1/songs/{id}/mifs` | Body `{"startMs":82300,"durationMs":10000}` (1–20 s). Returns a `Mif`, `201` if new, `200` if that exact moment was already a mif. |
| `GET /v1/mifs/{id}` | `Mif` (metadata only) |
| `GET /v1/mifs/{id}/audio` | Selected interval only; AAC/MP4 with HTTP range requests and immutable caching |
| `GET /.well-known/apple-app-site-association` | Universal Links configuration when `MIFS_IOS_APP_ID` is set |
| `GET /m/{id}` | The mif's share page: plays the clip with its lyrics in any browser, with link previews for messengers |
| `GET /media/{key}` | Full audio (`audio/mp4`) or artwork (`image/jpeg`), with range requests |

```json
{
  "id": "neon-harbor",
  "title": "Neon Harbor",
  "artist": "Orchid Relay",
  "album": "Night Signals",
  "trackNumber": 1,
  "releaseYear": 2026,
  "genre": "Synthwave",
  "explicit": false,
  "durationMs": 127315,
  "highlightStartMs": 38100,
  "hasLyrics": true,
  "audio": { "url": "http://localhost:8080/media/audio/3f/3fa9….m4a", "contentType": "audio/mp4", "bitrate": 256000, "size": 4523108 },
  "artwork": { "url": "http://localhost:8080/media/artwork/…jpg", "thumbnailUrl": "http://localhost:8080/media/artwork/…jpg" },
  "license": { "name": "CC0-1.0", "url": "https://creativecommons.org/publicdomain/zero/1.0/", "attribution": "…" }
}
```

Every song also has `status`: `pending` or `processing` while MIFS adds it, `ready` once it
can be played and clipped, `unavailable` when no audio source had it, or `failed`. Only
ready songs have `audio`. Optional fields (`isrc`, `album`, `trackNumber`, `releaseYear`,
`genre`, `highlightStartMs`, `artwork`, `license`, `links`) are omitted when unknown. Until a
song is ready, its `artwork` is the provider's.

A search `Result` is `{"ref","songId","status","source","title","artist","album","durationMs","explicit","isrc","artwork","song"}`.
`status` is `new` when MIFS doesn't have the song yet; `song` is the full `Song` when it's
ready.

A `Mif` is:

```json
{
  "id": "xsgdjktq7w66",
  "url": "http://localhost:8080/m/xsgdjktq7w66",
  "songId": "vnac4yqo3rml", "startMs": 27000, "durationMs": 10000,
  "lyrics": [{"startMs": 27160, "endMs": 29960, "text": "I've been tryna call"}],
  "audio": { "url": "http://localhost:8080/v1/mifs/xsgdjktq7w66/audio", "contentType": "audio/mp4" },
  "song": { "id": "vnac4yqo3rml", "status": "ready", "title": "Blinding Lights", "links": [{"provider": "spotify", "url": "…"}], "…": "…" },
  "createdAt": "2026-09-13T12:56:39Z"
}
```

Share `url`. `audio` is the moment on its own, a short AAC clip any platform can play.

Errors look like `{"error":{"code":"not_found","message":"…"}}`.

## Adding songs

Make a folder per song and run `go run ./cmd/mifs-server ingest <folder or parent folder>`.
Re-running it updates songs in place and is idempotent.

```
my-song/
  song.json     required
  audio.*       required; anything ffmpeg reads (FLAC/WAV masters are best, AAC .m4a is used as-is)
  artwork.*     optional; any image, centre-cropped to a square
  lyrics.lrc    optional; line-synced LRC
```

`song.json` must use only these keys, so a typo is an error rather than silently ignored:

| Key | |
| --- | --- |
| `id` | Required. Stable URL-safe ID (`a-z`, `0-9`, `-`). Snippets refer to it, so don't change it. |
| `title`, `artist` | Required |
| `isrc` | Optional. Lets search recognise provider results as this song |
| `album`, `trackNumber`, `releaseYear`, `genre`, `explicit` | Optional metadata |
| `highlightStart` | Seconds. The suggested snippet start, e.g. the chorus. |
| `license` (`name`, `url`), `attribution` | Where the audio comes from. Only import audio you have the rights to distribute. |
| `audio`, `artwork`, `lyrics` | File names, when they're not `audio.*` / `artwork.*` / `lyrics.lrc` |

## Try it in the app

1. Install `ffmpeg`/`ffprobe`, then run `make -C server setup` to install spotDL in an isolated `.venv`.
2. Copy `server/.env.example` to `server/.env`. Supply `MIFS_SPOTIFY_CLIENT_ID` and
   `MIFS_SPOTIFY_CLIENT_SECRET` from your [Spotify developer app](https://developer.spotify.com/dashboard).
3. Run `make -C server run`. Search in the app or Messages extension: only Spotify results
   appear. **Top Songs** lists ready songs already on the server.
4. Tap a result. The editor opens with a download bar while spotDL prepares the recording.
   It reports actual downloaded bytes when the source supplies a total, then finishes processing.
   The full waveform and lyrics appear when ready; no preview audio is used.
5. Select a preset, drag either edge of the waveform selection (maximum 20 seconds), or tap a lyric.
   Tap **Share** and choose a destination. **Recent** opens saved moments and lyrics with playback.

The song response includes `downloadedBytes` and `downloadTotalBytes`. A zero total means the
upstream size is not yet known. Counters reset on retry; completed downloads may still need
transcoding, artwork, and waveform processing before the song becomes ready.

## Songs on demand

The SQLite queue resolves Spotify identity before downloading, so simultaneous selections
reuse one song. Workers fetch lyrics while acquiring audio, verify recording duration,
transcode, compute a waveform, then publish readiness. Temporary acquisition files are removed.

- **spotDL** is the default source. `MIFS_SPOTDL` overrides its executable. It receives an
  argument array with a validated Spotify track URL; track metadata is never shell code.
- **`MIFS_LIBRARY_DIR`** optionally supplies matching local recordings before spotDL.
- **`MIFS_AUDIO_COMMAND`** explicitly replaces spotDL, e.g. `scripts/test-tone.sh` for
  deterministic local testing. It receives song metadata through environment variables.

Sharing writes only a mif metadata row, pins the full recording's audio key, and snapshots
its lyric lines. Playback seeks into that recording with ffmpeg and produces only the selected
interval in memory. Concurrent identical requests share one render; a 32 MiB process cache
and immutable HTTP caching avoid repeat work. No `media/clips` files are created.
Existing clip files from older versions are left untouched, and older message links still play.

## Configuration

| Variable (flag) | Default | |
| --- | --- | --- |
| `MIFS_ADDR` (`-addr`) | `127.0.0.1:8080` | Use `:8080` so a phone on your Wi-Fi can connect |
| `MIFS_DATA_DIR` (`-data`) | `data` | Holds `mifs.db` and `media/` |
| `MIFS_PUBLIC_URL` (`-public-url`) | from each request | External origin when deployed, e.g. `https://mifs.example.com`. Mif share links use it |
| `MIFS_MEDIA_URL` (`-media-url`) | `<public URL>/media` | Where media is published, e.g. a CDN |
| `MIFS_FFMPEG`, `MIFS_FFPROBE` | from `PATH` | For ingestion and mif clips |
| `MIFS_DISCOVERY` (`-discovery`) | `spotify` | Search requires Spotify credentials; other providers are only for legacy identities |
| `MIFS_SPOTDL` | `spotdl` | Downloader executable |
| `MIFS_SPOTDL_COOKIE_FILE` | | Private Netscape-format cookies file for the audio provider; use its container path when deployed |
| `MIFS_IOS_APP_ID` | | Apple team ID + bundle ID for Universal Links |
| `MIFS_SPOTIFY_CLIENT_ID`, `MIFS_SPOTIFY_CLIENT_SECRET`, `MIFS_SPOTIFY_MARKET` | | Spotify app credentials, optional market (e.g. `US`) |
| `MIFS_LYRICS` (`-lyrics`) | `lrclib` | Synced lyrics providers. Empty turns lyric lookups off |
| `MIFS_LIBRARY_DIR` (`-library`) | | Audio source: a folder of audio you own |
| `MIFS_AUDIO_COMMAND` (`-audio-command`) | | Audio source: a command that fetches audio |
| `MIFS_WORKERS` (`-workers`) | `2` | Songs ingested at once. `0` turns ingestion off in this process |

Spotify credentials and audio-provider cookies are secret. The write endpoints (`POST`) have no authentication
yet, so put the server behind authentication or rate limiting before exposing it publicly.

`statusMessage` describes whether a song is queued, locating audio, downloading, processing,
or waiting for a retry. Only actual transfer bytes produce a download percentage. When the
audio provider requires authentication, the song fails immediately instead of retrying silently;
the public response contains a safe explanation, while subprocess diagnostics stay in server logs.
Spotify credentials authorize discovery, not YouTube downloads. The container includes Deno for
yt-dlp, but a provider can still require an authenticated session on the server.

## Deploying

For the Debian server at `mifs.cgn.fi`, use the rootless Podman container and Quadlet in
[`deploy/`](deploy/README.md). They include persistent storage, the public origin, and nginx
configuration. The general manual deployment steps below remain useful for other hosts.

1. `GOOS=linux GOARCH=amd64 make build` produces `bin/mifs-server`.
2. Run `ingest` on the host, or copy a `data/` directory built elsewhere.
3. Run `mifs-server serve` behind a TLS-terminating proxy (Caddy, nginx, Fly.io, …) that
   also gzips JSON. Set `MIFS_PUBLIC_URL` to the public origin.
4. Optionally sync `data/media/` to object storage and set `MIFS_MEDIA_URL` to its CDN
   URL. Only new keys ever need uploading.
5. Point the app at it with `MIFS_SERVER_URL` in `Config/Local.xcconfig`, or change the
   default in `Config/MIFS.xcconfig` for release builds.
6. Set `MIFS_LINK_HOST` in the same config to the public HTTPS hostname and
   `MIFS_IOS_APP_ID=TEAMID.bundle.id` on the server. The app claims `/m/*`, and the server
   serves the corresponding Apple association file. A real HTTPS domain and a signed build
   are needed to verify automatic app opening; localhost links play in the browser.

## Demo songs

`seed/` holds three original songs made for this prototype: audio, artwork and synced
lyrics, all released as CC0. [seed/CREDITS.md](seed/CREDITS.md) says how they were made,
and `tools/demogen` is the generator.

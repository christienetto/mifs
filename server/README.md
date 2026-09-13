# MIFS music server

A small Go service that gives MIFS full songs, artwork, precomputed waveforms and
synchronised lyrics, so the app can clip any moment of a song and show the lyrics
alongside the timeline.

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

Requirements: Go 1.25+ and, for importing songs only, `ffmpeg`/`ffprobe`
(`brew install ffmpeg`).

```sh
cd server
make run      # imports seed/ into data/, then serves http://localhost:8080
```

`make seed` re-imports without serving, `make test` runs vet and the tests, and
`make clean` deletes `data/`. Everything in `data/` is generated from `seed/`, so it's
git-ignored and safe to delete.

Check it's up with `curl localhost:8080/healthz`.

## Design

- **One static binary, no runtime dependencies.** The pure-Go SQLite driver
  (`modernc.org/sqlite`) avoids cgo, so the server cross-compiles for any Linux host.
  Only ingest shells out to ffmpeg.
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
  8 kHz mono at 10 points/s). The editor draws the whole song immediately. The app falls
  back to analysing the audio itself if the waveform is ever missing.
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

All endpoints are `GET` (and `HEAD`), public and read-only.

| Endpoint | Returns |
| --- | --- |
| `/healthz` | `{"status":"ok","songs":3}` |
| `/v1/songs?q=&limit=&cursor=` | `{"songs":[Song…],"nextCursor":"…"}`. Without `q`, all songs by title. `limit` is 1–100 (default 50). Pass `nextCursor` back as `cursor` for the next page. |
| `/v1/songs/{id}` | `Song` |
| `/v1/songs/{id}/lyrics` | `{"songId","synced":true,"lines":[{"startMs","endMs","text"}]}`, or 404 when the song has no lyrics |
| `/v1/songs/{id}/waveform` | `{"songId","durationMs","pointsPerSecond":10,"rms":[…]}` |
| `/media/{key}` | Audio (`audio/mp4`) or artwork (`image/jpeg`), with range requests |

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

Optional fields (`album`, `trackNumber`, `releaseYear`, `genre`, `highlightStartMs`,
`artwork`, `license`) are omitted when unknown. Errors look like
`{"error":{"code":"not_found","message":"…"}}`.

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
| `album`, `trackNumber`, `releaseYear`, `genre`, `explicit` | Optional metadata |
| `highlightStart` | Seconds. The suggested snippet start, e.g. the chorus. |
| `license` (`name`, `url`), `attribution` | Where the audio comes from. Only import audio you have the rights to distribute. |
| `audio`, `artwork`, `lyrics` | File names, when they're not `audio.*` / `artwork.*` / `lyrics.lrc` |

## Configuration

| Variable (flag) | Default | |
| --- | --- | --- |
| `MIFS_ADDR` (`-addr`) | `127.0.0.1:8080` | Use `:8080` so a phone on your Wi-Fi can connect |
| `MIFS_DATA_DIR` (`-data`) | `data` | Holds `mifs.db` and `media/` |
| `MIFS_PUBLIC_URL` (`-public-url`) | from each request | External origin when deployed, e.g. `https://mifs.example.com` |
| `MIFS_MEDIA_URL` (`-media-url`) | `<public URL>/media` | Where media is published, e.g. a CDN |
| `MIFS_FFMPEG`, `MIFS_FFPROBE` | from `PATH` | Used by `ingest` only |

No secrets are needed. The catalog is public and read-only.

## Deploying

1. `GOOS=linux GOARCH=amd64 make build` produces `bin/mifs-server`.
2. Run `ingest` on the host, or copy a `data/` directory built elsewhere.
3. Run `mifs-server serve` behind a TLS-terminating proxy (Caddy, nginx, Fly.io, …) that
   also gzips JSON. Set `MIFS_PUBLIC_URL` to the public origin.
4. Optionally sync `data/media/` to object storage and set `MIFS_MEDIA_URL` to its CDN
   URL. Only new keys ever need uploading.
5. Point the app at it with `MIFS_SERVER_URL` in `Config/Local.xcconfig`, or change the
   default in `Config/MIFS.xcconfig` for release builds.

## Demo songs

`seed/` holds three original songs made for this prototype: audio, artwork and synced
lyrics, all released as CC0. [seed/CREDITS.md](seed/CREDITS.md) says how they were made,
and `tools/demogen` is the generator.

# Debian deployment: mifs.cgn.fi

Deployed on 2026-09-20 to `root@62.238.47.100` (`personal`, Debian 13, Podman 5.4.2).
MIFS runs as a rootless Podman service owned by `mifs`, created with the existing
`/usr/local/bin/useraddservice` helper. The host needed `uidmap` and `passt` installed.
The deployment follows `notka`'s separate user, user systemd manager, loopback listener,
and nginx TLS setup; MIFS uses a Quadlet instead of a direct binary service.

Public health: <https://mifs.cgn.fi/healthz>. Demo share: <https://mifs.cgn.fi/m/fcoovqjq7gof>.
The three demo songs are imported. Spotify credentials are configured in the private server
environment file; a live Spotify search returned results successfully on 2026-09-20.

## Operating the installed service

From a root SSH session:

```sh
systemctl --user --machine=mifs@.host status mifs.service
systemctl --user --machine=mifs@.host restart mifs.service
```

Or enter the account with `machinectl shell mifs@` (the existing `sc` helper also offers it),
then use `systemctl --user ...` and `journalctl --user -u mifs.service`.
The account has linger enabled, and the generated service is attached to `default.target`
for boot startup. Health failures kill the container so systemd can restart it.

Installed paths:

- Quadlet: `/home/mifs/.config/containers/systemd/mifs.container`
- Data: `/home/mifs/data` (SQLite, media; keep across deployments)
- Credentials: `/home/mifs/.config/mifs/server.env` (mifs-owned, mode 0600)
- Build sources: `/home/mifs/src/server`
- nginx: `/etc/nginx/sites-available/mifs`, linked from `/etc/nginx/sites-enabled/mifs`
- TLS: existing `/home/acme/live/cgn/cert.pem` and `key.pem`, covering `cgn.fi` and `*.cgn.fi`

To rotate credentials, update `MIFS_SPOTIFY_CLIENT_ID` and `MIFS_SPOTIFY_CLIENT_SECRET` in the
credentials file, then restart `mifs.service`. The deployment did not change the
existing wildcard certificate's renewal setup.

## Container

From `server/`, build with `podman build -t localhost/mifs-server:latest .`.
The image contains the Go binary, ffmpeg/ffprobe, spotDL 4.5.2, Deno, and demo recordings.
Only `/data` is persistent. Container root maps to the service account when run rootless.

Build on the remote host as `mifs`, or transfer an image built for the host's architecture
using `podman save` and `podman load` (also as `mifs`). Root's image store is separate.
Use the existing server helper to create the account with subuid/subgid ranges and
enable its user manager at boot with `loginctl enable-linger mifs`.

Install these files, owned by `mifs`:

| Repository file | Remote path |
| --- | --- |
| `mifs.container` | `/home/mifs/.config/containers/systemd/mifs.container` |
| `server.env.example` | `/home/mifs/.config/mifs/server.env` (mode 0600) |

Create `/home/mifs/data`, owned by `mifs`. Add Spotify credentials to `server.env` on the
host; never include that file in the image or source control. Without them, prepared songs
and sharing work, but Spotify discovery is disabled.

Run the following in the `mifs` user's login session after loading/building the image:

```sh
podman run --rm --volume /home/mifs/data:/data localhost/mifs-server:latest ingest /opt/mifs/seed
systemctl --user daemon-reload
systemctl --user start mifs.service
systemctl --user status mifs.service
curl --fail http://127.0.0.1:18080/healthz
```

Quadlet generates `mifs.service`; its `[Install]` section attaches it to `default.target`.
Do not run `systemctl enable` on the generated service. With linger, it starts at boot.
From root, address the user manager with `systemctl --user --machine=mifs@.host ...`
where supported, or `runuser -u mifs -- env XDG_RUNTIME_DIR=/run/user/UID systemctl --user ...`
using the account's numeric UID. On this host, root exports its own D-Bus address; use the
following when invoking Podman directly as the service user:

```sh
cd /home/mifs
runuser -u mifs -- env XDG_RUNTIME_DIR=/run/user/1002 \
  DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1002/bus podman ps
```

Logs: `journalctl --user -u mifs.service` as `mifs`.

## nginx and HTTPS

`mifs.nginx.conf` is the installed configuration, rendered from `nginx.conf.template` using
the certificate paths in `/etc/nginx/sites-enabled/notka.conf`. It forwards to loopback port 18080, leaving the container off the public
network, and limits POST requests to reduce unauthenticated download abuse. Confirm port 18080
is free before installation. If Cloudflare proxying is enabled, reuse the server's trusted
Cloudflare real-IP configuration so rate limits apply to visitors instead of Cloudflare edges;
never trust arbitrary incoming IP headers.

Replace `__TLS_CERTIFICATE__` and `__TLS_CERTIFICATE_KEY__` with actual paths to a certificate
covering `mifs.cgn.fi`. A certificate for only `cgn.fi` does not cover this subdomain.
Use the existing certificate automation; if issuing through HTTP-01, serve the challenge
location on port 80 before enabling the HTTPS block. The challenge webroot is
`/var/www/letsencrypt` in this template. A Cloudflare Origin CA certificate requires proxied
DNS; a publicly trusted certificate also supports direct connections.

Install the rendered configuration as `/etc/nginx/sites-available/mifs` and symlink it into
`/etc/nginx/sites-enabled/mifs`. Run `nginx -t` successfully before `systemctl reload nginx`.
Keep a copy of any configuration being replaced.

On 2026-09-20, public DNS returned `mifs.cgn.fi A 62.238.47.100`, with Cloudflare authoritative
nameservers. It currently resolves directly to the origin (DNS-only). No OVHcloud or Cloudflare
change is needed for the deployed HTTPS service. If enabling Cloudflare proxying later, first
configure trusted Cloudflare real-IP ranges in nginx, then use Full (strict) with a
valid origin certificate. Do not use Flexible with the HTTP-to-HTTPS redirect.

## Validation and updates

### Audio-provider authentication

On 2026-09-20, downloading “God Is” from the server was rejected by YouTube with a sign-in
requirement, including after Deno was installed. The downloader now preserves this error and
marks the song failed immediately, rather than silently retrying while the apps say downloading.
Both clients display the server's safe preparation message; iOS no longer fabricates progress.
The updated server and Android build were deployed and the authentication error was verified
on the OnePlus 6T. Go tests/vet, Android unit tests/lint/build, and existing public clip playback
passed. Later on 2026-09-20, a user-supplied YouTube cookie file was installed privately,
the cookie mount and downloader setting were enabled, and “God Is” downloaded successfully
and reached `ready` with real byte progress. Its 43 lyric lines, 2,033 waveform samples,
and [10-second share](https://mifs.cgn.fi/m/xsxda4eop2mr) were verified over public HTTPS.
The AAC clip decoded without errors; audio range requests, Spotify search, and container
health passed. The prepared song and share remained playable after a service restart.

The installed service and checked-in Quadlet now mount an authorized Netscape-format YouTube
cookies file at `/home/mifs/.config/mifs/youtube-cookies.txt` (owner mifs, mode 0600):

```ini
Volume=%h/.config/mifs/youtube-cookies.txt:/run/mifs/youtube-cookies.txt
```

The installed `server.env` sets `MIFS_SPOTDL_COOKIE_FILE=/run/mifs/youtube-cookies.txt`.
For a new deployment, provision this private file and setting before starting the service,
or remove the cookie mount to run without authentication. After changing configuration,
reload the user systemd manager and restart MIFS. Keep configuration files owned by `mifs`;
`server.env` must have mode 0600. yt-dlp may update the cookie jar, so the mount is writable.
When replacing cookies, stop MIFS, replace the file with owner `mifs` and mode 0600,
then start MIFS to ensure the bind mount uses the replacement file.
Never commit cookies or bake them into the image. Credentials may expire, and authentication
does not guarantee that an upstream provider will allow a download. A matching local recording
through `MIFS_LIBRARY_DIR` is another supported source. After correcting the source, select the
song again to retry (the server applies a one-minute cooldown to failed songs).

See [yt-dlp cookie authentication](https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp)
and its [JavaScript runtime requirements](https://github.com/yt-dlp/yt-dlp/wiki/EJS).

### Checks

Passed locally: Go tests and vet; image build; catalog/lyrics/waveform/clip/range/persistence
smoke checks; Quadlet generation; nginx syntax; Android debug APK, unit tests, and lint.
Passed on the deployed service: public TLS and HTTP redirect, all demo catalog/clip checks,
share-page playback responses, audio byte ranges, Apple association, Podman healthcheck,
and persistence of the demo mif through a systemd service restart. Boot startup configuration
was verified without rebooting the shared host. On a physical OnePlus 6T (Android 11), the
updated app passed `catalogEditorShareAndRecent` against production (catalog, preview,
share sheet, Recent playback), and a manual Spotify search returned tracks successfully.
iOS still needs a signed build and device verification in Xcode.

Check public HTTPS `/healthz`, `/v1/songs`, song waveforms/lyrics, and create/play a mif from
a demo song. Verify returned media/share URLs start with `https://mifs.cgn.fi`. Confirm audio
range requests work. Restart the service and confirm catalog and mif metadata persist.
Check `/.well-known/apple-app-site-association`; the Quadlet uses the checked-in signing
identity `VKNG7T32BD.fi.cgn.mifs`. Update it if the Apple team or bundle ID changes.

Both mobile apps use the HTTPS origin by default. Existing Android Settings overrides survive
an upgrade and must be changed manually. iOS local build settings and launch arguments can
also override the default. Rebuild/install the apps to apply source changes. iOS Universal
Links require a signed build on an Apple device. Android supports opening received links via
its existing paste/share flow; verified Android App Links are not configured.

For an update, tag the previous image as a rollback image, build/load the replacement, then
`systemctl --user restart mifs.service`. Back up `/home/mifs/data` with the service stopped
(SQLite uses WAL) and protect a separate copy of `server.env`. Keep the old image and matching
database backup for rollback. Never delete the data directory during deployment.

References: [Podman Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html),
[Cloudflare subdomain records](https://developers.cloudflare.com/dns/manage-dns-records/how-to/create-subdomain/).

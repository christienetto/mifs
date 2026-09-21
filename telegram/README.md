# MIFS for Telegram

The Telegram counterpart of the MIFS iMessage app: a **Main Mini App** plus a small bot, served by one
stateless Cloudflare Worker.

## How snippets travel

| From | Server mif (selected song interval) | Clip of the user's own audio |
| --- | --- | --- |
| iMessage (unchanged) | `MSMessage` bubble; tap opens the MIFS player | audio attachment |
| Telegram | photo card (artwork, song) with **▶︎ Play Snippet**; tap opens the MIFS player in the chat | native Telegram audio message (title, artist, artwork) via Telegram's share extension |

**Sending from iOS or Android.** Choose **Share → Telegram** for a mif from `https://mifs.cgn.fi`. This opens
`tg://resolve?domain=<bot>&startapp=s2_<mifId>&mode=compact`. The MIFS Mini App opens and, without waiting for a
tap, the server prepares the card (`savePreparedInlineMessage`) and Telegram's share sheet shows its preview
(`WebApp.shareMessage`); picking a chat sends it. If the sheet is dismissed, **Send to Chat** opens it again.
Without Telegram installed, the same link opens on t.me.

**Receiving.** The card's button is a Main Mini App link (`t.me/<bot>?startapp=p2_<mifId>&mode=compact`), so anyone
can play the exact moment inside Telegram on any platform, without installing anything. **Reply with a Snippet** opens the
Mini App's song browser and editor, the same flow as the iMessage extension.

**Other ways in.** The bot's menu button and `/start` open the song browser. Typing `@<bot>` in any chat shows a
**Make a Snippet** button; the finished snippet returns to that chat as an inline result.

The server code (`<s|p>2_<mifId>`) references an immutable song interval stored by the music server.
The Worker resolves metadata and proxies only the selected audio interval, including HTTP range requests.
The iOS, Android, and Mini App codecs use the same fixture. Everything comes from the MIFS music server: the
Mini App's top songs, search (including tracks the server fetches on demand), downloads, waveforms, lyrics and
mifs, and the card's artwork. Nothing is looked up in or streamed from Apple Music.

Owned audio stays on-device and uses the system share sheet.

## Layout

```
public/          the Mini App (no build step): app.js, audio.js, mifs.js, snippet-code.js
src/worker.js    routes: static files, POST /api/share, POST /telegram (webhook)
src/init-data.js Telegram initData verification (HMAC-SHA256)
src/card.js      the chat card (InlineQueryResultPhoto)
src/bot.js       Bot API client, /start and inline queries
scripts/setup.mjs one-shot bot configuration
test/            node --test (no dependencies)
```

## Setup

1. **Create the bot** with [@BotFather](https://t.me/BotFather): `/newbot`. The username must match
   `MIFS_TELEGRAM_BOT` in `../project.yml` and `BOT_USERNAME` in `wrangler.toml` (both `MIFSAppBot` now; if
   you choose another, change both and run `xcodegen generate`).
2. **Deploy the Worker.** Put the secrets in `.dev.vars` (git-ignored; see `.dev.vars.example`): `BOT_TOKEN` from
   BotFather and `WEBHOOK_SECRET`, any random string such as `openssl rand -hex 32`. Then:
   ```sh
   npx wrangler@4 login
   npx wrangler@4 secret bulk .dev.vars       # uploads BOT_TOKEN and WEBHOOK_SECRET
   npm run deploy                             # prints https://mifs-telegram.<account>.workers.dev
   ```
   `wrangler.toml` sets `MIFS_SERVER_URL=https://mifs.cgn.fi`. Redeploying the Worker is required:
   older deployments do not expose `/api/mifs/<id>` and cannot play the apps' new server mif codes.
   Preserve the existing bot token and webhook secret when updating; if they are already configured
   in Cloudflare, `npm run deploy` is sufficient and does not require re-uploading secrets or running setup.
3. **In BotFather** (these have no Bot API equivalent):
   - `/mybots` → the bot → *Bot Settings* → *Configure Mini App* → enable the **Main Mini App** with the Worker URL.
   - `/setinline` → the bot → placeholder `Make a snippet…`.
   - Optionally upload a bot picture (the MIFS icon is at `../MIFS/Assets.xcassets/AppIcon.appiconset/AppIcon.png`).
4. **Configure the bot**, which also checks step 3:
   ```sh
   APP_URL=https://mifs-telegram.<account>.workers.dev EXPECTED_USERNAME=MIFSAppBot npm run setup
   ```

## Development

```sh
npm test                      # server, card, initData and codec tests
npm run dev                   # local Worker + Mini App at http://localhost:8787
```

Outside Telegram, the page substitutes its own bottom and back buttons, so the player
(`/?code=p2_<mifId>`), browser and editor can be tried in a desktop browser. Sending needs
Telegram, because `initData` proves who is sending.

## Restoration validation (2026-09-20)

The updated code passed 30 JavaScript tests and a Wrangler deployment dry run. Against the
public music server, the Worker code resolved the “God Is” mif into an artwork card and proxied
its audio with a 206 range response. Android build, five unit tests, and lint (no errors)
passed; the updated APK was installed on the OnePlus 6T and the catalog/editor/share/Recent
device test passed. Swift source parsing passed on Linux, but iOS still needs an Xcode build
and signed installation. The live Worker still needs deployment: its `/api/mifs/<id>` route
returned 404. No Telegram message was sent during these checks.

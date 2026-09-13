# MIFS for Telegram

The Telegram counterpart of the MIFS iMessage app: a **Main Mini App** plus a small bot, served by one
stateless Cloudflare Worker.

## How snippets travel

| From | Catalog snippet (Apple Music preview) | Clip of the user's own audio |
| --- | --- | --- |
| iMessage (unchanged) | `MSMessage` bubble; tap opens the MIFS player | audio attachment |
| Telegram | photo card (artwork, song) with **▶︎ Play Snippet**; tap opens the MIFS player in the chat | native Telegram audio message (title, artist, artwork) via Telegram's share extension |

**Sending from the iOS app.** Tapping **Telegram** in the editor (or *Send in Telegram* in Snippets) opens
`tg://resolve?domain=<bot>&startapp=s1_…&mode=compact`. Telegram shows the MIFS Mini App as a half-sheet playing the
snippet, with **Send to Chat** → the server prepares the card (`savePreparedInlineMessage`) → Telegram's own chat
picker (`WebApp.shareMessage`). Without Telegram installed, the same link opens on t.me.

**Receiving.** The card's button is a Main Mini App link (`t.me/<bot>?startapp=p1_…&mode=compact`), so anyone
can play the exact moment inside Telegram on any platform, without installing anything. **Reply with a Snippet** opens the
Mini App's song browser and editor, the same flow as the iMessage extension.

**Other ways in.** The bot's menu button and `/start` open the song browser. Typing `@<bot>` in any chat shows a
**Make a Snippet** button; the finished snippet returns to that chat as an inline result.

The snippet code (`<s|p>1_<trackId>_<storefront>_<startMs>_<durationMs>_<waveform hex>`) fully describes a
snippet, so nothing is stored. It is defined in `public/snippet-code.js` and `MIFS/Telegram/TelegramLink.swift`;
both test suites assert the same fixture string.

Owned audio never touches MIFS servers. Apple previews are streamed from Apple, never re-hosted. This matches how the
iMessage app treats them.

## Layout

```
public/          the Mini App (no build step): app.js, audio.js, catalog.js, snippet-code.js
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
(`/?code=p1_1499378607_us_12345_10000_…`), browser and editor can be tried in a desktop browser. Sending needs
Telegram, because `initData` proves who is sending.

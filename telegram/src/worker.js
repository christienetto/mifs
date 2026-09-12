// MIFS for Telegram, as one Cloudflare Worker:
//   /                  the MIFS Mini App (static files in ../public)
//   POST /api/share    turns a snippet into a Telegram message the user can send with WebApp.shareMessage
//   POST /telegram     the bot's webhook
//   /app/…             links back into the MIFS iOS app (universal links; a fallback page elsewhere)
// It keeps no state: a snippet is fully described by its code, and song details come from Apple's catalog.

import { lookupTrack } from '../public/catalog.js';
import { parseCode } from '../public/snippet-code.js';
import { callBot, handleUpdate } from './bot.js';
import { sanitizeTrack, snippetResult } from './card.js';
import { verifyInitData } from './init-data.js';

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    try {
      if (url.pathname === '/api/share' && request.method === 'POST') return await share(request, env);
      if (url.pathname === '/telegram' && request.method === 'POST') return await webhook(request, env, url.origin);
      if (url.pathname === '/.well-known/apple-app-site-association') return appSiteAssociation(env);
      if (url.pathname.startsWith('/app/')) return backToAppPage();
    } catch (error) {
      console.error(error);
      return json({ error: 'Something went wrong. Try again.' }, 500);
    }
    if (url.pathname.startsWith('/api/') || url.pathname === '/telegram') return json({ error: 'Not found' }, 404);
    return env.ASSETS ? env.ASSETS.fetch(request) : new Response('Not found', { status: 404 });
  },
};

/// Body: `{ initData, code, track? }`. Replies `{ id }`, a PreparedInlineMessage for WebApp.shareMessage.
export async function share(request, env, fetchImpl = fetch) {
  const body = await request.json().catch(() => null);
  const auth = await verifyInitData(body?.initData, env.BOT_TOKEN);
  if (!auth?.user?.id) return json({ error: 'Open MIFS from Telegram to send snippets.' }, 401);

  const snippet = parseCode(body.code);
  if (!snippet) return json({ error: 'This snippet can’t be sent.' }, 400);

  // Prefer Apple's own catalog data; fall back to what the Mini App already looked up.
  const track = await lookupTrack(snippet.trackId, snippet.storefront, cachedFetch(fetchImpl)).catch(() => null)
    ?? sanitizeTrack(body.track, snippet.trackId);
  if (!track) return json({ error: 'Couldn’t reach Apple Music. Try again in a moment.' }, 502);

  const prepared = await callBot(env, 'savePreparedInlineMessage', {
    user_id: auth.user.id,
    result: snippetResult({ snippet, track, botUsername: env.BOT_USERNAME }),
    allow_user_chats: true,
    allow_bot_chats: false,
    allow_group_chats: true,
    allow_channel_chats: true,
  }, fetchImpl);
  return json({ id: prepared.id });
}

export async function webhook(request, env, origin, fetchImpl = fetch) {
  if (!env.WEBHOOK_SECRET || request.headers.get('x-telegram-bot-api-secret-token') !== env.WEBHOOK_SECRET) {
    return new Response('Forbidden', { status: 403 });
  }
  const update = await request.json().catch(() => null);
  const reply = update ? await handleUpdate(update, { env, origin, fetchImpl: cachedFetch(fetchImpl) }) : null;
  return reply ? json(reply) : new Response(null, { status: 204 });
}

/// Lets iOS open /app/… links in the MIFS app itself (universal links) instead of the browser.
function appSiteAssociation(env) {
  const appIDs = env.APPLE_APP_ID ? [env.APPLE_APP_ID] : [];
  return json({ applinks: { details: [{ appIDs, components: [{ '/': '/app/*', comment: 'Back to MIFS' }] }] } });
}

/// Where /app/… links land when the MIFS app didn't catch them (not installed, or opened in a browser).
function backToAppPage() {
  return new Response(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>MIFS</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:17px -apple-system,system-ui,sans-serif;
background:#3d2e73;color:#fff;text-align:center}a{display:inline-block;margin-top:18px;padding:14px 28px;border-radius:26px;
background:#fff;color:#000;font-weight:600;text-decoration:none}</style></head>
<body><main><h1>Sent ✓</h1><p>Your snippet is in the chat.</p><a href="mifs://telegram/sent">Back to MIFS</a></main></body></html>`, {
    headers: { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store' },
  });
}

/// Lets Cloudflare cache Apple's catalog responses at the edge (ignored elsewhere).
function cachedFetch(fetchImpl) {
  return (url, init = {}) => fetchImpl(url, { ...init, cf: { cacheTtl: 3600, cacheEverything: true } });
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store' },
  });
}

import assert from 'node:assert/strict';
import test from 'node:test';
import { handleUpdate } from '../src/bot.js';
import { sanitizeTrack, snippetResult } from '../src/card.js';
import { signInitData, verifyInitData } from '../src/init-data.js';
import worker from '../src/worker.js';

const BOT_TOKEN = '123456:TEST-token';
const env = { BOT_TOKEN, BOT_USERNAME: 'MIFSAppBot', WEBHOOK_SECRET: 'hook-secret' };
const CODE = 's1_1499378607_us_12345_10000_08f4';

const lookupResponse = {
  resultCount: 1,
  results: [{
    wrapperType: 'track', kind: 'song', trackId: 1499378607, trackName: 'Blinding <Lights>', artistName: 'The Weeknd',
    collectionName: 'After Hours', trackExplicitness: 'notExplicit',
    artworkUrl100: 'https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/a/b.jpg/100x100bb.jpg',
    previewUrl: 'https://audio-ssl.itunes.apple.com/itunes-assets/x/mzaf_1.plus.aac.p.m4a',
    trackViewUrl: 'https://music.apple.com/us/album/blinding-lights/1499378108?i=1499378607&uo=4',
  }],
};

/// A fetch that answers Apple lookups and Bot API calls, recording the Bot API calls it saw.
function fakeFetch({ appleStatus = 200 } = {}) {
  const calls = [];
  const fetchImpl = async (url, init = {}) => {
    const href = String(url);
    if (href.startsWith('https://itunes.apple.com/lookup')) {
      return new Response(JSON.stringify(lookupResponse), { status: appleStatus });
    }
    if (href.startsWith(`https://api.telegram.org/bot${BOT_TOKEN}/`)) {
      calls.push({ method: href.split('/').pop(), params: JSON.parse(init.body) });
      return Response.json({ ok: true, result: { id: 'prepared-1', expiration_date: 0 } });
    }
    throw new Error(`Unexpected fetch ${href}`);
  };
  return { fetchImpl, calls };
}

async function initData(overrides = {}) {
  return signInitData({
    auth_date: String(Math.floor(Date.now() / 1000)),
    query_id: 'AAE',
    user: JSON.stringify({ id: 42, first_name: 'Sam' }),
    start_param: CODE,
    signature: 'ed25519-signature-is-also-hashed',
    ...overrides,
  }, BOT_TOKEN);
}

// MARK: - initData

test('accepts genuine init data', async () => {
  const verified = await verifyInitData(await initData(), BOT_TOKEN);
  assert.equal(verified.user.id, 42);
  assert.equal(verified.startParam, CODE);
});

test('rejects tampered, foreign, stale or missing init data', async () => {
  const genuine = await initData();
  const tampered = genuine.replace('Sam', 'Eve');
  assert.equal(await verifyInitData(tampered, BOT_TOKEN), null);
  assert.equal(await verifyInitData(genuine, '999:other-bot'), null);
  const stale = await initData({ auth_date: String(Math.floor(Date.now() / 1000) - 2 * 86400) });
  assert.equal(await verifyInitData(stale, BOT_TOKEN), null);
  assert.equal(await verifyInitData('', BOT_TOKEN), null);
  assert.equal(await verifyInitData(undefined, BOT_TOKEN), null);
});

// Known-answer check against an independent implementation of Telegram's documented algorithm.
test('computes Telegram’s documented hash', async () => {
  const { createHmac } = await import('node:crypto');
  const fields = 'auth_date=1700000000\nquery_id=Q\nuser={"id":1}';
  const secret = createHmac('sha256', 'WebAppData').update(BOT_TOKEN).digest();
  const expected = createHmac('sha256', secret).update(fields).digest('hex');
  const signed = new URLSearchParams(await signInitData({ user: '{"id":1}', query_id: 'Q', auth_date: '1700000000' }, BOT_TOKEN));
  assert.equal(signed.get('hash'), expected);
});

// MARK: - Card

test('builds a photo card that opens the player in the chat', () => {
  const track = {
    id: '1499378607', title: 'Blinding <Lights>', artist: 'The Weeknd & Co', explicit: true,
    artworkURL: 'https://is1-ssl.mzstatic.com/image/thumb/a/b.jpg/600x600bb.jpg',
  };
  const snippet = { trackId: '1499378607', storefront: 'us', start: 12.345, duration: 10, waveform: [0, 0.5, 1, 0.25] };
  const result = snippetResult({ snippet, track, botUsername: 'MIFSAppBot' });

  assert.equal(result.type, 'photo');
  assert.equal(result.photo_url, 'https://is1-ssl.mzstatic.com/image/thumb/a/b.jpg/1000x1000bb.jpg');
  assert.equal(result.thumbnail_url, 'https://is1-ssl.mzstatic.com/image/thumb/a/b.jpg/200x200bb.jpg');
  assert.equal(result.parse_mode, 'HTML');
  assert.equal(result.caption, '🎵 <b>Blinding &lt;Lights&gt;</b> 🅴\nThe Weeknd &amp; Co');
  const [[button]] = result.reply_markup.inline_keyboard;
  assert.equal(button.url, 'https://t.me/MIFSAppBot?startapp=p1_1499378607_us_12345_10000_08f4&mode=compact');
  assert.ok(result.id.length <= 64);
});

test('falls back to a text card without artwork', () => {
  const result = snippetResult({
    snippet: { trackId: '1', storefront: 'us', start: 0, duration: 5, waveform: [] },
    track: { id: '1', title: 'Song', artist: 'Artist' },
    botUsername: 'MIFSAppBot',
  });
  assert.equal(result.type, 'article');
  assert.match(result.input_message_content.message_text, /Song/);
});

test('only trusts Apple URLs and plain text from the client', () => {
  const clean = sanitizeTrack({
    id: '7', title: ' Title\n', artist: 'Artist', explicit: 1,
    artworkURL: 'https://evil.example/x.jpg', appleMusicURL: 'https://music.apple.com/us/album/x/1?i=7',
  }, '7');
  assert.deepEqual(clean, {
    id: '7', title: 'Title', artist: 'Artist', explicit: true,
    artworkURL: null, appleMusicURL: 'https://music.apple.com/us/album/x/1?i=7',
  });
  assert.equal(sanitizeTrack({ id: '8', title: 'x' }, '7'), null);
  assert.equal(sanitizeTrack({ id: '7', title: '  ' }, '7'), null);
});

// MARK: - /api/share

function shareRequest(body) {
  return new Request('https://mifs.example/api/share', { method: 'POST', body: JSON.stringify(body) });
}

test('prepares a message for the verified user', async () => {
  const { share } = await import('../src/worker.js');
  const { fetchImpl, calls } = fakeFetch();
  const response = await share(shareRequest({ initData: await initData(), code: CODE }), env, fetchImpl);

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { id: 'prepared-1' });
  assert.equal(calls.length, 1);
  const { method, params } = calls[0];
  assert.equal(method, 'savePreparedInlineMessage');
  assert.equal(params.user_id, 42);
  assert.equal(params.allow_user_chats, true);
  assert.equal(params.allow_group_chats, true);
  assert.equal(params.result.type, 'photo');
  assert.match(params.result.caption, /Blinding &lt;Lights&gt;/);
  assert.match(params.result.reply_markup.inline_keyboard[0][0].url, /startapp=p1_1499378607_us_12345_10000_08f4/);
});

test('uses the Mini App’s metadata when Apple can’t be reached', async () => {
  const { share } = await import('../src/worker.js');
  const { fetchImpl, calls } = fakeFetch({ appleStatus: 403 });
  const track = { id: '1499378607', title: 'Blinding Lights', artist: 'The Weeknd', artworkURL: 'https://is1-ssl.mzstatic.com/a/600x600bb.jpg' };
  const response = await share(shareRequest({ initData: await initData(), code: CODE, track }), env, fetchImpl);
  assert.equal(response.status, 200);
  assert.match(calls[0].params.result.caption, /Blinding Lights/);
});

test('refuses unverified users and bad codes', async () => {
  const { share } = await import('../src/worker.js');
  const { fetchImpl, calls } = fakeFetch();
  assert.equal((await share(shareRequest({ initData: 'user=%7B%22id%22%3A1%7D&hash=00', code: CODE }), env, fetchImpl)).status, 401);
  assert.equal((await share(shareRequest({ initData: await initData(), code: 'nope' }), env, fetchImpl)).status, 400);
  assert.equal(calls.length, 0);
});

// MARK: - Webhook

function webhookRequest(update, secret = env.WEBHOOK_SECRET) {
  return new Request('https://mifs.example/telegram', {
    method: 'POST',
    headers: { 'x-telegram-bot-api-secret-token': secret },
    body: JSON.stringify(update),
  });
}

test('the webhook requires Telegram’s secret token', async () => {
  const response = await worker.fetch(webhookRequest({}, 'wrong'), env);
  assert.equal(response.status, 403);
});

test('/start greets with a button that opens the Mini App', async () => {
  const reply = await handleUpdate(
    { message: { text: '/start', chat: { id: 5, type: 'private' } } },
    { env, origin: 'https://mifs.example' },
  );
  assert.equal(reply.method, 'sendMessage');
  assert.equal(reply.chat_id, 5);
  assert.deepEqual(reply.reply_markup.inline_keyboard.at(-1)[0].web_app, { url: 'https://mifs.example/' });
});

test('/start with a snippet code offers to play it', async () => {
  const reply = await handleUpdate(
    { message: { text: `/start ${CODE}`, chat: { id: 5, type: 'private' } } },
    { env, origin: 'https://mifs.example' },
  );
  assert.equal(reply.reply_markup.inline_keyboard[0][0].web_app.url, `https://mifs.example/?code=${CODE}`);
});

test('an inline query with a snippet code answers with its card', async () => {
  const { fetchImpl } = fakeFetch();
  const reply = await handleUpdate(
    { inline_query: { id: 'iq', query: CODE, from: { id: 42 } } },
    { env, origin: 'https://mifs.example', fetchImpl },
  );
  assert.equal(reply.method, 'answerInlineQuery');
  assert.equal(reply.results.length, 1);
  assert.equal(reply.results[0].type, 'photo');
  assert.equal(reply.button.web_app.url, 'https://mifs.example/?from=inline');
});

test('any other inline query offers to make a snippet', async () => {
  const reply = await handleUpdate({ inline_query: { id: 'iq', query: 'blinding' } }, { env, origin: 'https://mifs.example' });
  assert.deepEqual(reply.results, []);
  assert.equal(reply.button.text, '🎵 Make a Snippet');
});

test('replies to the webhook with the Bot API call', async () => {
  const response = await worker.fetch(webhookRequest({ message: { text: '/start', chat: { id: 5, type: 'private' } } }), env);
  assert.equal(response.status, 200);
  assert.equal((await response.json()).method, 'sendMessage');
  const ignored = await worker.fetch(webhookRequest({ message: { text: 'hi', chat: { id: 5, type: 'private' } } }), env);
  assert.equal(ignored.status, 204);
});

test('lets the MIFS iOS app claim /app/ links', async () => {
  const response = await worker.fetch(
    new Request('https://mifs.example/.well-known/apple-app-site-association'),
    { ...env, APPLE_APP_ID: 'TEAM123.dev.kuchta.mifs' },
  );
  assert.equal(response.headers.get('content-type'), 'application/json; charset=utf-8');
  const [details] = (await response.json()).applinks.details;
  assert.deepEqual(details.appIDs, ['TEAM123.dev.kuchta.mifs']);
  assert.equal(details.components[0]['/'], '/app/*');
});

test('/app/ links fall back to a page that opens MIFS', async () => {
  const response = await worker.fetch(new Request('https://mifs.example/app/sent'), env);
  assert.equal(response.status, 200);
  assert.match(await response.text(), /href="mifs:\/\/telegram\/sent"/);
});

test('serves the Mini App from static assets', async () => {
  const assets = { fetch: async () => new Response('<html>MIFS</html>') };
  const response = await worker.fetch(new Request('https://mifs.example/'), { ...env, ASSETS: assets });
  assert.equal(await response.text(), '<html>MIFS</html>');
  assert.equal((await worker.fetch(new Request('https://mifs.example/api/nope'), env)).status, 404);
});

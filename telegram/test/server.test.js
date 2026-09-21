import assert from 'node:assert/strict';
import test from 'node:test';
import { handleUpdate } from '../src/bot.js';
import { snippetResult } from '../src/card.js';
import { signInitData, verifyInitData } from '../src/init-data.js';
import worker from '../src/worker.js';

const BOT_TOKEN = '123456:TEST-token';
const env = { BOT_TOKEN, BOT_USERNAME: 'MIFSAppBot', WEBHOOK_SECRET: 'hook-secret', MIFS_SERVER_URL: 'https://music.example' };
const CODE = 's2_abcdefghijkl';
const LEGACY_APPLE_CODE = 's1_1499378607_us_12345_10000_08f4';

const mifResponse = {
  id: 'abcdefghijkl', songId: 'song', startMs: 12345, durationMs: 10000, lyrics: [],
  song: {
    title: 'Blinding <Lights>', artist: 'The Weeknd', explicit: false,
    artwork: { url: 'https://music.example/media/art.jpg', thumbnailUrl: 'https://music.example/media/thumb.jpg' },
  },
};

/// A fetch that answers the music server's mif and Bot API calls, recording the Bot API calls it saw.
function fakeFetch({ serverStatus = 200 } = {}) {
  const calls = [];
  const fetchImpl = async (url, init = {}) => {
    const href = String(url);
    if (href === 'https://music.example/v1/mifs/abcdefghijkl') {
      return Response.json(mifResponse, { status: serverStatus });
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

test('builds a photo card from the music server’s artwork that opens the player in the chat', () => {
  const track = {
    id: 'song', title: 'Blinding <Lights>', artist: 'The Weeknd & Co', explicit: true,
    artworkURL: 'https://music.example/media/art.jpg', thumbnailURL: 'https://music.example/media/thumb.jpg',
  };
  const snippet = { mifId: 'abcdefghijkl', start: 12.345, duration: 10, lyrics: [] };
  const result = snippetResult({ snippet, track, botUsername: 'MIFSAppBot' });

  assert.equal(result.type, 'photo');
  assert.equal(result.id, 'abcdefghijkl');
  assert.equal(result.photo_url, 'https://music.example/media/art.jpg');
  assert.equal(result.thumbnail_url, 'https://music.example/media/thumb.jpg');
  assert.equal(result.parse_mode, 'HTML');
  assert.equal(result.caption, '🎵 <b>Blinding &lt;Lights&gt;</b> 🅴\nThe Weeknd &amp; Co');
  const [[button]] = result.reply_markup.inline_keyboard;
  assert.equal(button.url, 'https://t.me/MIFSAppBot?startapp=p2_abcdefghijkl&mode=compact');
});

test('falls back to a text card without artwork', () => {
  const result = snippetResult({
    snippet: { mifId: 'abcdefghijkl', start: 0, duration: 5 },
    track: { id: 'song', title: 'Song', artist: 'Artist' },
    botUsername: 'MIFSAppBot',
  });
  assert.equal(result.type, 'article');
  assert.match(result.input_message_content.message_text, /Song/);
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
  assert.equal(params.result.photo_url, 'https://music.example/media/art.jpg');
  assert.match(params.result.caption, /Blinding &lt;Lights&gt;/);
  assert.match(params.result.reply_markup.inline_keyboard[0][0].url, /startapp=p2_abcdefghijkl/);
});

test('reports a mif the music server can’t load', async () => {
  const { share } = await import('../src/worker.js');
  const { fetchImpl, calls } = fakeFetch({ serverStatus: 503 });
  const response = await share(shareRequest({ initData: await initData(), code: CODE }), env, fetchImpl);
  assert.equal(response.status, 502);
  assert.equal(calls.length, 0);
});

test('refuses unverified users, bad codes and legacy Apple codes', async () => {
  const { share } = await import('../src/worker.js');
  const { fetchImpl, calls } = fakeFetch();
  assert.equal((await share(shareRequest({ initData: 'user=%7B%22id%22%3A1%7D&hash=00', code: CODE }), env, fetchImpl)).status, 401);
  assert.equal((await share(shareRequest({ initData: await initData(), code: 'nope' }), env, fetchImpl)).status, 400);
  assert.equal((await share(shareRequest({ initData: await initData(), code: LEGACY_APPLE_CODE }), env, fetchImpl)).status, 400);
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
  assert.equal(reply.cache_time, 3600);
  assert.equal(reply.button.web_app.url, 'https://mifs.example/?from=inline');
});

test('a mif the music server can’t load is offered again soon', async () => {
  const { fetchImpl } = fakeFetch({ serverStatus: 503 });
  const reply = await handleUpdate({ inline_query: { id: 'iq', query: CODE } }, { env, origin: 'https://mifs.example', fetchImpl });
  assert.deepEqual(reply.results, []);
  assert.equal(reply.cache_time, 5);
});

test('any other inline query, including legacy Apple codes, offers to make a snippet', async () => {
  for (const query of ['blinding', LEGACY_APPLE_CODE]) {
    const reply = await handleUpdate({ inline_query: { id: 'iq', query } }, { env, origin: 'https://mifs.example' });
    assert.deepEqual(reply.results, [], query);
    assert.equal(reply.button.text, '🎵 Make a Snippet');
  }
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
    { ...env, APPLE_APP_ID: 'TEAM123.fi.cgn.mifs' },
  );
  assert.equal(response.headers.get('content-type'), 'application/json; charset=utf-8');
  const [details] = (await response.json()).applinks.details;
  assert.deepEqual(details.appIDs, ['TEAM123.fi.cgn.mifs']);
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

test('server mifs produce bot artwork cards with escaped lyrics and a play button', async () => {
  const { share } = await import('../src/worker.js');
  const calls = [];
  const serverEnv = { ...env, MIFS_SERVER_URL: 'https://music.example' };
  const fetchImpl = async (url, options = {}) => {
    if (String(url) === 'https://music.example/v1/mifs/abcdefghijkl') {
      return Response.json({ id: 'abcdefghijkl', songId: 'song', startMs: 12345, durationMs: 7250,
        lyrics: [{ startMs: 13000, endMs: 16000, text: 'You & I <sing>' }],
        song: { title: 'Song', artist: 'Artist', artwork: { url: 'https://music.example/media/art.jpg' } } });
    }
    calls.push(JSON.parse(options.body));
    return Response.json({ ok: true, result: { id: 'prepared-mif' } });
  };
  const response = await share(new Request('https://bot.example/api/share', { method: 'POST',
    body: JSON.stringify({ code: 's2_abcdefghijkl', initData: await initData(), track: { title: 'forged' } }) }), serverEnv, fetchImpl);
  assert.equal(response.status, 200);
  const result = calls[0].result;
  assert.equal(result.type, 'photo');
  assert.equal(result.photo_url, 'https://music.example/media/art.jpg');
  assert.match(result.caption, /You &amp; I &lt;sing&gt;/);
  assert.match(result.reply_markup.inline_keyboard[0][0].url, /startapp=p2_abcdefghijkl/);
  assert.equal(calls[0].user_id, 42);
});

test('an inline query with a server mif code answers with the mif card from the music server', async () => {
  // The iOS app opens Telegram's chat picker with "@bot p2_<mifId>" typed into the chosen chat.
  const serverEnv = { ...env, MIFS_SERVER_URL: 'https://music.example' };
  const fetchImpl = async (url) => {
    assert.equal(String(url), 'https://music.example/v1/mifs/abcdefghijkl');
    return Response.json({ id: 'abcdefghijkl', songId: 'song', startMs: 12345, durationMs: 7250, lyrics: [],
      song: { title: 'Song', artist: 'Artist', artwork: { url: 'https://music.example/media/art.jpg' } } });
  };
  const reply = await handleUpdate(
    { inline_query: { id: 'iq', query: 'p2_abcdefghijkl', from: { id: 42 } } },
    { env: serverEnv, origin: 'https://mifs.example', fetchImpl },
  );
  assert.equal(reply.method, 'answerInlineQuery');
  const [result] = reply.results;
  assert.equal(result.type, 'photo');
  assert.equal(result.photo_url, 'https://music.example/media/art.jpg');
  assert.equal(result.reply_markup.inline_keyboard[0][0].url, 'https://t.me/MIFSAppBot?startapp=p2_abcdefghijkl&mode=compact');
});

test('mif audio proxy preserves partial responses and never takes a client-supplied URL', async () => {
  const { mifAudio } = await import('../src/mifs.js');
  const response = await mifAudio(new Request('https://bot.example/api/mifs/abcdefghijkl/audio', { headers: { Range: 'bytes=0-99' } }),
    { MIFS_SERVER_URL: 'https://music.example' }, 'abcdefghijkl', async (url, options) => {
      assert.equal(url, 'https://music.example/v1/mifs/abcdefghijkl/audio');
      assert.equal(options.headers.get('Range'), 'bytes=0-99');
      return new Response('audio', { status: 206, headers: { 'Content-Range': 'bytes 0-4/100', 'Content-Type': 'audio/mp4' } });
    });
  assert.equal(response.status, 206);
  assert.equal(response.headers.get('Content-Range'), 'bytes 0-4/100');
});

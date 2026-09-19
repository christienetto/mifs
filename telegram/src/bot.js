// The MIFS bot: a thin companion to the Mini App. It greets people who open it directly, and answers
// inline queries so a snippet made from the "@bot" button in a chat lands back in that same chat.

import { loadMif } from './mifs.js';
import { lookupTrack } from '../public/catalog.js';
import { parseCode } from '../public/snippet-code.js';
import { snippetResult } from './card.js';

export class BotError extends Error {}

export async function callBot(env, method, params, fetchImpl = fetch) {
  const response = await fetchImpl(`https://api.telegram.org/bot${env.BOT_TOKEN}/${method}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(params),
  });
  const body = await response.json().catch(() => ({}));
  if (!body.ok) throw new BotError(`${method} failed: ${body.description ?? response.status}`);
  return body.result;
}

/// Returns the Bot API call to make in reply to a webhook update (sent back as the webhook response),
/// or null when there's nothing to do.
export async function handleUpdate(update, { env, origin, fetchImpl = fetch }) {
  if (update.inline_query) return answerInlineQuery(update.inline_query, { env, origin, fetchImpl });

  const message = update.message;
  if (message?.chat?.type === 'private' && /^\/(start|help)\b/.test(message.text ?? '')) {
    const argument = message.text.split(/\s+/)[1];
    const code = parseCode(argument) ? argument : null;
    return {
      method: 'sendMessage',
      chat_id: message.chat.id,
      parse_mode: 'HTML',
      text: [
        '🎵 <b>MIFS</b> sends the best moment of a song, not the whole thing.',
        '',
        'Pick any 5–15 seconds of a song and send it to a chat, where it plays right in the conversation.',
        '',
        'On iPhone, make a snippet in the MIFS app and tap <b>Telegram</b>. Or make one right here.',
      ].join('\n'),
      reply_markup: {
        inline_keyboard: [
          ...(code ? [[{ text: '▶︎ Play Snippet', web_app: { url: `${origin}/?code=${code}` } }]] : []),
          [{ text: '🎵 Make a Snippet', web_app: { url: `${origin}/` } }],
        ],
      },
    };
  }
  return null;
}

async function answerInlineQuery(query, { env, origin, fetchImpl }) {
  const makeButton = { text: '🎵 Make a Snippet', web_app: { url: `${origin}/?from=inline` } };
  let snippet = parseCode(query.query);
  let results = [];
  if (snippet) {
    let track;
    if (snippet.mifId) {
      const resolved = await loadMif(env, snippet.mifId, fetchImpl).catch(() => null);
      if (resolved) { snippet = resolved.snippet; track = resolved.track; }
    } else { track = await lookupTrack(snippet.trackId, snippet.storefront, fetchImpl).catch(() => null); }
    if (track) results = [snippetResult({ snippet, track, botUsername: env.BOT_USERNAME })];
  }
  return {
    method: 'answerInlineQuery',
    inline_query_id: query.id,
    results,
    button: makeButton,
    cache_time: snippet ? 3600 : 300,
  };
}

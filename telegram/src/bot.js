// The MIFS bot: a thin companion to the Mini App. It greets people who open it directly, and answers
// "@bot <code>" inline queries with the mif's card from the MIFS music server — typed in by the MIFS app
// after choosing a chat, or by the Mini App so a snippet made from the "@bot" button lands in that chat.

import { loadMif } from './mifs.js';
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
        'Pick up to 20 seconds of a song and send it to a chat, where it plays right in the conversation.',
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
  const code = parseCode(query.query);
  const mif = code ? await loadMif(env, code.mifId, fetchImpl).catch(() => null) : null;
  return {
    method: 'answerInlineQuery',
    inline_query_id: query.id,
    results: mif ? [snippetResult({ ...mif, botUsername: env.BOT_USERNAME })] : [],
    button: makeButton,
    // Mifs never change; a mif the music server couldn't load is retried soon.
    cache_time: mif ? 3600 : code ? 5 : 300,
  };
}

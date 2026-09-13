// Points the bot at the deployed Worker and sets up how it presents itself.
//   APP_URL=https://mifs-telegram.<you>.workers.dev npm run setup
// BOT_TOKEN and WEBHOOK_SECRET come from .dev.vars (variables set in the shell take precedence).
// A bot's Main Mini App and inline mode can only be switched on in @BotFather; this checks they are.

const { BOT_TOKEN, WEBHOOK_SECRET, APP_URL } = process.env;
if (!BOT_TOKEN || !WEBHOOK_SECRET || !APP_URL) {
  console.error('Set BOT_TOKEN, WEBHOOK_SECRET and APP_URL (the Worker’s https URL).');
  process.exit(1);
}
const appURL = APP_URL.replace(/\/+$/, '');

async function call(method, params = {}) {
  const response = await fetch(`https://api.telegram.org/bot${BOT_TOKEN}/${method}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(params),
  });
  const body = await response.json();
  if (!body.ok) throw new Error(`${method}: ${body.description}`);
  return body.result;
}

const me = await call('getMe');
console.log(`Bot: @${me.username}`);

await call('setWebhook', {
  url: `${appURL}/telegram`,
  secret_token: WEBHOOK_SECRET,
  allowed_updates: ['message', 'inline_query'],
  drop_pending_updates: true,
});
await call('setChatMenuButton', { menu_button: { type: 'web_app', text: 'MIFS', web_app: { url: `${appURL}/` } } });
await call('setMyCommands', { commands: [{ command: 'start', description: 'Make a music snippet' }] });
await call('setMyShortDescription', { short_description: 'Send the best 5–15 seconds of any song. It plays right in the chat.' });
await call('setMyDescription', {
  description: 'MIFS sends the best moment of a song, not the whole thing. Pick any 5–15 seconds, send it to a chat, and it plays right in the conversation.',
});
console.log(`Webhook, menu button and descriptions set for ${appURL}`);

const problems = [];
if (!me.supports_inline_queries) problems.push('Inline mode is off: @BotFather → /setinline → choose the bot → placeholder “Make a snippet…”.');
if (me.has_main_web_app === false) problems.push(`No Main Mini App: @BotFather → /mybots → the bot → Bot Settings → Configure Mini App → Enable, URL ${appURL}/`);
if (process.env.EXPECTED_USERNAME && process.env.EXPECTED_USERNAME !== me.username) {
  problems.push(`The iOS app is built for @${process.env.EXPECTED_USERNAME}; set MIFS_TELEGRAM_BOT (project.yml) and BOT_USERNAME (wrangler.toml) to ${me.username}.`);
}
for (const problem of problems) console.warn(`⚠︎ ${problem}`);
if (problems.length === 0) console.log('All set.');

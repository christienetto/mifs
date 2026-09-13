// Verifies Telegram.WebApp.initData, as described in
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
//   secret = HMAC_SHA256(key: "WebAppData", data: bot_token)
//   hash   = hex(HMAC_SHA256(key: secret, data: data_check_string))
// where data_check_string is every received field except `hash`, sorted, as "key=value" lines.

const encoder = new TextEncoder();
const DAY = 24 * 60 * 60;

/// Returns `{ user, authDate, startParam }` for genuine, fresh init data, otherwise null.
export async function verifyInitData(initData, botToken, { maxAge = DAY, now = Date.now() / 1000 } = {}) {
  if (typeof initData !== 'string' || !initData || !botToken) return null;
  const params = new URLSearchParams(initData);
  const hash = params.get('hash');
  if (!hash) return null;
  params.delete('hash');

  const expected = await sign(params, botToken);
  if (!constantTimeEqual(expected, hash.toLowerCase())) return null;

  const authDate = Number(params.get('auth_date'));
  if (!Number.isFinite(authDate) || now - authDate > maxAge) return null;

  let user = null;
  try {
    user = JSON.parse(params.get('user') ?? 'null');
  } catch {
    return null;
  }
  return { user, authDate, startParam: params.get('start_param') };
}

/// Produces init data the way Telegram does; used by tests and local development.
export async function signInitData(fields, botToken) {
  const params = new URLSearchParams(fields);
  params.set('hash', await sign(params, botToken));
  return params.toString();
}

async function sign(params, botToken) {
  const dataCheckString = [...params.entries()]
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([key, value]) => `${key}=${value}`)
    .join('\n');
  const secret = await hmac(encoder.encode('WebAppData'), encoder.encode(botToken));
  return toHex(await hmac(secret, encoder.encode(dataCheckString)));
}

async function hmac(keyBytes, dataBytes) {
  const key = await crypto.subtle.importKey('raw', keyBytes, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  return new Uint8Array(await crypto.subtle.sign('HMAC', key, dataBytes));
}

function toHex(bytes) {
  return [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('');
}

function constantTimeEqual(a, b) {
  if (a.length !== b.length) return false;
  let difference = 0;
  for (let index = 0; index < a.length; index += 1) difference |= a.charCodeAt(index) ^ b.charCodeAt(index);
  return difference === 0;
}

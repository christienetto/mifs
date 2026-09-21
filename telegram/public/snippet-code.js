// The snippet "code" carried by Telegram start parameters and inline queries: a mif on the MIFS music server.
// Mirrors MIFS/Telegram/TelegramLink.swift and android/…/TelegramLink.kt:
//   <intent>2_<mifId>
// intent: "s" = opened by the sender to send it, "p" = opened from a chat to play it.

export const LENGTHS = [5, 10, 15, 20];

export function parseCode(value) {
  const match = /^([sp])2_([a-z2-7]{12})$/.exec(String(value ?? '').trim());
  return match ? { intent: match[1] === 's' ? 'send' : 'play', mifId: match[2] } : null;
}

export function formatCode({ intent = 'play', mifId }) {
  if (!/^[a-z2-7]{12}$/.test(mifId)) throw new Error('Invalid mif ID');
  return `${intent === 'send' ? 's' : 'p'}2_${mifId}`;
}

export function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

/// "0:07", "3:42".
export function clock(seconds) {
  const total = Math.floor(Math.max(0, seconds));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
}

// The snippet "code" carried by Telegram start parameters and inline queries.
// Mirrors MIFS/Telegram/TelegramLink.swift:
//   <intent>1_<trackId>_<storefront>_<startMs>_<durationMs>_<waveform>
// intent: "s" = opened by the sender to send it, "p" = opened from a chat to play it.
// waveform: one hex digit (0–f) per bar, like SnippetLink's mifs_w.

export const VERSION = 1;
export const LENGTHS = [5, 10, 15];
export const WAVEFORM_BARS = 40;

const PATTERN = /^([sp])1_([0-9]{1,15})_([a-z]{2})_([0-9]{1,6})_([0-9]{1,5})_([0-9a-f]{0,64})$/;

export function parseCode(value) {
  const match = PATTERN.exec(String(value ?? '').trim());
  if (!match) return null;
  const [, intent, trackId, storefront, startMs, durationMs, waveform] = match;
  const duration = Number(durationMs) / 1000;
  if (duration <= 0) return null;
  return {
    intent: intent === 's' ? 'send' : 'play',
    trackId,
    storefront,
    start: Number(startMs) / 1000,
    duration,
    waveform: decodeWaveform(waveform),
  };
}

export function formatCode({ intent = 'play', trackId, storefront, start, duration, waveform = [] }) {
  return [
    `${intent === 'send' ? 's' : 'p'}${VERSION}`,
    String(trackId),
    String(storefront || 'us').toLowerCase(),
    String(Math.round(start * 1000)),
    String(Math.round(duration * 1000)),
    encodeWaveform(waveform.slice(0, 64)),
  ].join('_');
}

export function encodeWaveform(levels) {
  return levels.map((level) => Math.round(clamp(level, 0, 1) * 15).toString(16)).join('');
}

export function decodeWaveform(hex) {
  return [...hex].map((digit) => parseInt(digit, 16) / 15);
}

export function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

/// "0:07", "3:42".
export function clock(seconds) {
  const total = Math.floor(Math.max(0, seconds));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
}

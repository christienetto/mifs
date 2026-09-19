import assert from 'node:assert/strict';
import test from 'node:test';
import { contrast, loudestWindow, RESOLUTION, snippetLevels } from '../public/audio.js';
import { clock, decodeWaveform, encodeWaveform, formatCode, parseCode } from '../public/snippet-code.js';

// Same snippet and string as MIFSTests/TelegramLinkTests.matchesTheMiniAppFormat.
test('matches the iOS app’s format', () => {
  const code = formatCode({
    intent: 'send', trackId: '1499378607', storefront: 'gb', start: 12.345, duration: 10, waveform: [0, 0.5, 1, 0.25],
  });
  assert.equal(code, 's1_1499378607_gb_12345_10000_08f4');

  const parsed = parseCode(code);
  assert.equal(parsed.intent, 'send');
  assert.equal(parsed.trackId, '1499378607');
  assert.equal(parsed.storefront, 'gb');
  assert.equal(parsed.start, 12.345);
  assert.equal(parsed.duration, 10);
  assert.deepEqual(parsed.waveform.map((level) => Math.round(level * 15)), [0, 8, 15, 4]);
});

test('round-trips a play code with a full waveform', () => {
  const waveform = Array.from({ length: 40 }, (_, index) => (index % 16) / 15);
  const code = formatCode({ intent: 'play', trackId: '1', storefront: 'US', start: 0, duration: 15, waveform });
  assert.match(code, /^p1_1_us_0_15000_[0-9a-f]{40}$/);
  assert.ok(code.length <= 512);
  assert.deepEqual(parseCode(code).waveform, waveform.map((level) => Math.round(level * 15) / 15));
});

test('rejects malformed codes', () => {
  for (const value of [
    null, '', 's1', 'x1_1_us_0_10000_', 's2_1499378607_us_0_10000_ff', 's1_abc_us_0_10000_ff',
    's1_1_USA_0_10000_ff', 's1_1_us_0_0_ff', 's1_1_us_0_10000_zz', 's1_1_us_0_10000_ff_extra',
    'hello world',
  ]) {
    assert.equal(parseCode(value), null, String(value));
  }
});

test('waveform hex encoding clamps and rounds', () => {
  assert.equal(encodeWaveform([-1, 0.5, 2]), '08f');
  assert.deepEqual(decodeWaveform('0f'), [0, 1]);
});

test('clock formats like the app', () => {
  assert.equal(clock(7.9), '0:07');
  assert.equal(clock(222), '3:42');
});

// Ports of MIFSTests/SnippetLinkTests' WaveformTests, so the Mini App suggests the same moments.
test('finds the loudest window', () => {
  const levels = Array.from({ length: 300 }, (_, index) => (index >= 150 && index < 250 ? 1 : 0.1));
  assert.ok(Math.abs(loudestWindow(levels, 10) - 15) < 0.01);
});

test('resamples snippet levels', () => {
  const levels = Array.from({ length: 300 }, (_, index) => (index % 10) / 10);
  const bars = snippetLevels(levels, 5, 10, 40);
  assert.equal(bars.length, 40);
  assert.ok(bars.every((level) => level >= 0 && level <= 1));
  assert.equal(Math.max(...bars), 1);
});

test('handles a selection past the end', () => {
  assert.equal(snippetLevels(new Array(50).fill(0.5), 4, 10, 8).length, 8);
});

test('contrast maps loudness into 0.1…1', () => {
  const levels = contrast([0.001, 0.01, 0.1, 1]);
  assert.equal(levels.length, 4);
  assert.ok(Math.abs(levels[3] - 1) < 1e-9);
  assert.ok(levels.every((level, index) => level >= 0.1 && (index === 0 || level >= levels[index - 1])));
  assert.equal(RESOLUTION, 10);
});

test('server mif codes match iOS and retain only immutable mif identity', () => {
  assert.deepEqual(parseCode('s2_abcdefghijkl'), { intent: 'send', mifId: 'abcdefghijkl' });
  assert.equal(formatCode({ intent: 'play', mifId: 'abcdefghijkl' }), 'p2_abcdefghijkl');
  for (const bad of ['s2_https://evil.test', 'p2_../etc/passwd', 's2_abcdefghijkl_extra']) assert.equal(parseCode(bad), null);
});

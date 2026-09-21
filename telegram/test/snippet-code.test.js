import assert from 'node:assert/strict';
import test from 'node:test';
import { contrast, loudestWindow, RESOLUTION, snippetLevels } from '../public/audio.js';
import { clock, formatCode, parseCode } from '../public/snippet-code.js';

// Same codes as MIFSTests/TelegramLinkTests.serverMifUsesBotCardCode.
test('server mif codes match iOS and retain only immutable mif identity', () => {
  assert.deepEqual(parseCode('s2_abcdefghijkl'), { intent: 'send', mifId: 'abcdefghijkl' });
  assert.deepEqual(parseCode(' p2_abcdefghijkl '), { intent: 'play', mifId: 'abcdefghijkl' });
  assert.equal(formatCode({ intent: 'play', mifId: 'abcdefghijkl' }), 'p2_abcdefghijkl');
  assert.equal(formatCode({ intent: 'send', mifId: 'abcdefghijkl' }), 's2_abcdefghijkl');
  assert.throws(() => formatCode({ mifId: '../etc/passwd' }));
});

test('rejects malformed codes and legacy Apple preview codes', () => {
  for (const value of [
    null, '', 's2', 'x2_abcdefghijkl', 's2_abcdefghijk', 's2_ABCDEFGHIJKL', 's2_abcdefghijk1',
    's2_https://evil.test', 'p2_../etc/passwd', 's2_abcdefghijkl_extra', 'hello world',
    's1_1499378607_gb_12345_10000_08f4', 'p1_1_us_0_15000_',
  ]) {
    assert.equal(parseCode(value), null, String(value));
  }
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

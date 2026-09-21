import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';
import { formatCode } from '../public/snippet-code.js';

// Exercise the actual send handler without the player DOM or a Telegram account.
const app = readFileSync(new URL('../public/app.js', import.meta.url), 'utf8');
const handler = app.slice(app.indexOf('async function send('), app.indexOf('function backToApp('));

function harness({ platform = 'android', intent = 'send', fromInline = false, modern = true } = {}) {
  const calls = [];
  let tap;
  const context = vm.createContext({
    formatCode, fromInline, inTelegram: true, launchCode: intent ? { intent } : null,
    current: { pause() {} }, player: { stop() {} },
    mainButton: { busy() {}, show(_text, handler) { tap = handler; } }, notify: message => calls.push(['error', message]),
    tg: {
      platform, initData: 'signed-data', isVersionAtLeast: () => modern,
      switchInlineQuery: (...args) => calls.push(['inline', ...args]),
      shareMessage: id => calls.push(['prepared', id]),
    },
    fetch: async (url, options) => {
      calls.push(['fetch', url, JSON.parse(options.body)]);
      return { ok: true, json: async () => ({ id: 'prepared-card' }) };
    },
  });
  vm.runInContext(handler, context);
  return {
    tap() { tap(); return JSON.parse(JSON.stringify(calls)); },
    async send(options) {
      await context.send({ mifId: 'abcdefghijkl' }, options);
      return JSON.parse(JSON.stringify(calls));
    },
  };
}

test('Android prepares on launch and opens the sending sheet synchronously on tap', async () => {
  const app = harness();
  const prepared = [['fetch', '/api/share', { initData: 'signed-data', code: 'p2_abcdefghijkl' }]];
  assert.deepEqual(await app.send({ automatic: true }), prepared);
  assert.deepEqual(app.tap(), [...prepared, ['prepared', 'prepared-card']]);
});

test('iOS app launch keeps the prepared card share sheet', async () => {
  assert.deepEqual(await harness({ platform: 'ios' }).send({ automatic: true }), [
    ['fetch', '/api/share', { initData: 'signed-data', code: 'p2_abcdefghijkl' }],
    ['prepared', 'prepared-card'],
  ]);
});

test('Android replies from an existing card retain the prepared share sheet', async () => {
  assert.equal((await harness({ intent: 'play' }).send()).at(-1)[0], 'prepared');
});

test('inline launches return the card to the current chat', async () => {
  assert.deepEqual(await harness({ fromInline: true }).send(), [['inline', 'p2_abcdefghijkl']]);
});

test('older Telegram versions use the inline chat picker', async () => {
  assert.deepEqual(await harness({ platform: 'ios', modern: false }).send(), [
    ['inline', 'p2_abcdefghijkl', ['users', 'groups', 'channels']],
  ]);
});

// The MIFS Mini App. Three screens, mirroring the iMessage extension:
//   player   — opened from a snippet card in a chat (or by the sender, from the iOS app) to play it
//   browser  — Apple Music top songs and search, to make a snippet (or reply with one)
//   editor   — pick the moment, preview it, send it
// Sending prepares a message on the server and hands it to Telegram's own chat picker (shareMessage).

import { contrast, analyze, loadPreview, loudestWindow, SnippetPlayer, snippetLevels, unlockOnFirstGesture } from './audio.js';
import { CatalogError, currentStorefront, lookupTrack, spotifySearchURL } from './catalog.js';
import { clamp, clock, formatCode, LENGTHS, parseCode, WAVEFORM_BARS } from './snippet-code.js';

import { api, prepareTrack, serverSearch as search, serverTopSongs as topSongs } from './mifs.js';

const tg = window.Telegram?.WebApp;
const inTelegram = Boolean(tg?.initData);
const launch = new URLSearchParams(location.search);
/// Opened from the button above inline results: the snippet goes back to that chat as an inline query.
const fromInline = launch.get('from') === 'inline';
/// The snippet this launch was for, if any: "send" from the iOS app, "play" from a card in a chat.
const launchCode = parseCode(tg?.initDataUnsafe?.start_param || launch.get('tgWebAppStartParam') || launch.get('code'));
const storefront = currentStorefront();
const app = document.getElementById('app');
const player = new SnippetPlayer(() => current?.update?.());
let current = null;

if (inTelegram) document.documentElement.classList.add('tg');

// MARK: - Telegram chrome (with stand-ins when opened in a plain browser)

function call(method, ...args) {
  try {
    return tg?.[method]?.(...args);
  } catch {
    return undefined;
  }
}

const haptics = {
  tap: () => inTelegram && tg.HapticFeedback?.impactOccurred('light'),
  selection: () => inTelegram && tg.HapticFeedback?.selectionChanged(),
  success: () => inTelegram && tg.HapticFeedback?.notificationOccurred('success'),
};

/// Header, background and bottom bar follow the artwork tint on dark screens, and the theme elsewhere.
function paintChrome(tint) {
  if (!inTelegram) return;
  call('setHeaderColor', tint ?? 'secondary_bg_color');
  call('setBackgroundColor', tint ?? 'secondary_bg_color');
  if (tg.isVersionAtLeast?.('7.10')) call('setBottomBarColor', tint ?? 'secondary_bg_color');
}

const mainButton = (() => {
  const fallback = document.getElementById('fallback-button');
  let handler = null;
  if (inTelegram) tg.MainButton.onClick(() => handler?.());
  else fallback.addEventListener('click', () => handler?.());

  return {
    show(text, onClick, { light = false, enabled = true } = {}) {
      handler = onClick;
      if (inTelegram) {
        tg.MainButton.setParams({
          text,
          color: light ? '#ffffff' : tg.themeParams.button_color ?? '#2a9ef1',
          text_color: light ? '#000000' : tg.themeParams.button_text_color ?? '#ffffff',
          is_active: enabled,
          is_visible: true,
        });
      } else {
        fallback.textContent = text;
        fallback.classList.toggle('light', light);
        fallback.disabled = !enabled;
        fallback.hidden = false;
        document.body.classList.add('has-fallback-button');
      }
    },
    enable(enabled) {
      if (inTelegram) enabled ? tg.MainButton.enable() : tg.MainButton.disable();
      else fallback.disabled = !enabled;
    },
    busy(busy) {
      if (inTelegram) busy ? tg.MainButton.showProgress(false) : tg.MainButton.hideProgress();
      else fallback.disabled = busy;
    },
    hide() {
      handler = null;
      if (inTelegram) tg.MainButton.hide();
      else {
        fallback.hidden = true;
        document.body.classList.remove('has-fallback-button');
      }
    },
  };
})();

const backButton = (() => {
  const fallback = document.getElementById('fallback-back');
  let handler = null;
  if (inTelegram) tg.BackButton.onClick(() => handler?.());
  else fallback.addEventListener('click', () => handler?.());
  return {
    set(onBack) {
      handler = onBack;
      if (inTelegram) onBack ? tg.BackButton.show() : tg.BackButton.hide();
      else fallback.hidden = !onBack;
    },
  };
})();

function openLink(url) {
  if (inTelegram) tg.openLink(url);
  else window.open(url, '_blank', 'noopener');
}

function notify(message) {
  if (inTelegram) tg.showAlert(message);
  else window.alert(message);
}

// MARK: - Navigation

function mount(view) {
  if (current === view) return;
  current?.leave?.();
  player.stop();
  current = view;
  app.replaceChildren(view.element);
  view.enter();
}

function frame() {
  current?.tick?.();
  requestAnimationFrame(frame);
}

// MARK: - Sending

/// Turns the snippet into a MIFS card and lets the user pick the chat, like Messages' compose sheet.
async function send(snippet, track) {
  current?.pause?.();
  const code = formatCode({ ...snippet, intent: 'play' });
  if (!inTelegram) {
    notify('Open MIFS from Telegram to send snippets.');
    return;
  }
  // Telegram's share sheet sends at once but can't say where, so it closes back to where the Mini App was
  // opened. That suits two launches: from a snippet card (back to that conversation) and from the iOS app
  // (back to MIFS). Opened from the bot itself, pick the chat first instead and land in it, with
  // "@bot <code>" typed in and the card offered above the keyboard (the bot answers that inline query).
  const fromApp = launchCode?.intent === 'send' && !fromInline;
  const insideConversation = launchCode?.intent === 'play' && !fromInline;
  if (!(fromApp || insideConversation) || !tg.isVersionAtLeast('8.0')) {
    try {
      player.stop();
      if (fromInline) tg.switchInlineQuery(code);
      else tg.switchInlineQuery(code, ['users', 'groups', 'channels']);
      return;
    } catch {
      // Inline mode unavailable: fall through to the share sheet.
    }
  }

  mainButton.busy(true);
  try {
    const response = await fetch('/api/share', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ initData: tg.initData, code, track }),
    });
    const body = await response.json().catch(() => ({}));
    if (!response.ok || !body.id) throw new Error(body.error || 'Couldn’t send this snippet. Try again.');
    player.stop();
    tg.shareMessage(body.id, (sent) => {
      if (!sent) return;
      haptics.success();
      if (fromApp && tg.platform === 'ios') backToApp();
      else setTimeout(() => tg.close(), 350);
    });
  } catch (error) {
    notify(error instanceof TypeError ? 'Couldn’t reach MIFS. Check your connection and try again.' : error.message);
  } finally {
    mainButton.busy(false);
  }
}

/// Hands back to the MIFS iOS app through a universal link it claims (/app/…). Telegram may only open links
/// right after a tap, so if the automatic hand-off is refused the bottom button offers it.
function backToApp() {
  let leaving = false;
  const open = () => {
    if (!leaving) {
      leaving = true;
      // Once MIFS is in front, close the Mini App so Telegram isn't left showing it.
      tg.onEvent('deactivated', () => tg.close());
    }
    tg.openLink(`${location.origin}/app/sent`);
  };
  mainButton.show('Back to MIFS', open, { light: true });
  try {
    open();
  } catch {
    // Refused without a tap: the button stays.
  }
}

// MARK: - Player

function playerView(snippet) {
  if (snippet.mifId) return mifPlayerView(snippet);
  const element = html(`
    <section class="player dark">
      <div class="backdrop"><div class="backdrop-art"></div></div>
      <div class="screen">
        <div class="art-wrap"><img class="art" alt=""></div>
        <div class="titles"><h1><span class="skeleton">&nbsp;</span></h1><p>&nbsp;</p></div>
        <div class="wave-block">
          <canvas class="bars" aria-hidden="true"></canvas>
          <div class="times"><span class="elapsed">0:00</span><span>${clock(snippet.duration)}</span></div>
        </div>
        <div class="controls-row"></div>
        <p class="status" hidden></p>
        <div class="listen" hidden>
          <button class="pill" type="button" data-open="apple" aria-label="Open in Apple Music">Apple Music</button>
          <button class="pill" type="button" data-open="spotify" aria-label="Open in Spotify">Spotify</button>
        </div>
      </div>
    </section>`);
  const art = element.querySelector('.art');
  const bars = element.querySelector('.bars');
  const elapsed = element.querySelector('.elapsed');
  const status = element.querySelector('.status');
  const button = playButton('big');
  element.querySelector('.controls-row').append(button.element);

  const owner = `snippet-${formatCode(snippet)}`;
  let track = null;
  let buffer = null;
  let preparing = true;
  let loaded = false;
  let browser = null;

  const view = { element, enter, update, tick };

  button.element.addEventListener('click', () => {
    haptics.tap();
    play();
  });
  element.querySelector('.listen').addEventListener('click', (event) => {
    const target = event.target.closest('[data-open]')?.dataset.open;
    if (target === 'apple' && track?.appleMusicURL) openLink(track.appleMusicURL);
    if (target === 'spotify' && track) openLink(spotifySearchURL(track));
  });

  function enter() {
    paintChrome(element.dataset.tint ?? '#3d2e73');
    backButton.set(null);
    showMainButton();
    if (!loaded) {
      loaded = true;
      load();
    }
    requestAnimationFrame(update);
  }

  function showMainButton() {
    if (snippet.intent === 'send') {
      mainButton.show('Send to Chat', () => send(snippet, track), { light: true, enabled: Boolean(track) });
    } else {
      mainButton.show('Reply with a Snippet', () => {
        browser ??= browserView({ onBack: () => mount(view) });
        mount(browser);
      }, { light: true });
    }
  }

  async function load() {
    setStatus(null);
    preparing = true;
    update();
    try {
      track = await lookupTrack(snippet.trackId, snippet.storefront);
      if (!track) throw new Error('This song isn’t available on Apple Music anymore.');
    } catch (error) {
      preparing = false;
      update();
      setStatus(error instanceof CatalogError || !(error instanceof TypeError) ? error.message
        : 'Couldn’t load this snippet. Check your connection and try again.', load);
      return;
    }
    element.querySelector('.titles').innerHTML = titles(track);
    if (track.artworkURL) art.src = track.artworkURL;
    element.querySelector('.listen').hidden = false;
    applyTint(element, track, view);
    if (current === view) showMainButton();

    buffer = await loadPreview(track.previewURL).catch(() => null);
    preparing = false;
    if (current === view) play(); // plays on open, like tapping a MIFS bubble in Messages
    update();
  }

  function play() {
    if (!track) return;
    player.toggle({ owner, buffer, url: buffer ? null : track.previewURL, start: snippet.start, duration: snippet.duration });
  }

  function setStatus(message, retry) {
    status.hidden = !message;
    status.innerHTML = message ? `${escapeHTML(message)}${retry ? '<br><button class="retry" type="button">Try Again</button>' : ''}` : '';
    status.querySelector('.retry')?.addEventListener('click', retry);
  }

  function update() {
    const active = player.isActive(owner);
    button.render(preparing && !active ? 'loading' : active ? player.state : 'idle', active ? player.progress : 0);
    art.classList.toggle('playing', active && player.state === 'playing');
    tick();
  }

  function tick() {
    const active = player.isActive(owner);
    const progress = active ? player.progress : 0;
    drawBars(bars, snippet.waveform, progress);
    elapsed.textContent = clock(progress * snippet.duration);
    button.render(active ? player.state : preparing ? 'loading' : 'idle', progress);
  }

  return view;
}

// Server mifs stream only their selected interval, with lyrics in original song time.
function mifPlayerView(snippet) {
  const element = html(`<section class="player dark"><div class="backdrop"><div class="backdrop-art"></div></div>
    <div class="screen"><div class="art-wrap"><img class="art" alt=""></div>
    <div class="titles"><h1>Loading song…</h1></div><div class="mif-lyrics" hidden></div>
    <div class="controls-row"></div><p class="status"></p></div></section>`);
  const button = playButton('big'); element.querySelector('.controls-row').append(button.element);
  const audio = new Audio(`/api/mifs/${snippet.mifId}/audio`);
  audio.preload = 'auto'; audio.setAttribute('playsinline', '');
  const status = element.querySelector('.status'), art = element.querySelector('.art-wrap');
  const lyrics = element.querySelector('.mif-lyrics');
  let resolved = null, alive = true;
  const view = { element, enter, leave, tick, update: tick, pause: () => audio.pause() };
  async function play() {
    status.textContent = '';
    if (audio.ended || (resolved && audio.currentTime >= resolved.snippet.duration)) audio.currentTime = 0;
    try { await audio.play(); }
    catch { if (alive) status.textContent = 'Tap Play to listen'; }
  }
  button.element.onclick = () => audio.paused ? play() : audio.pause();
  audio.onplay = tick; audio.onpause = tick; audio.ontimeupdate = tick;
  audio.onerror = () => { if (alive) status.textContent = 'Couldn’t play this mif. Tap Play to retry.'; };
  // Some WebViews require one gesture after opening. Retry once on that first gesture.
  function unlock(event) { if (!event.target.closest('.play') && audio.paused && alive) play(); }
  function enter() {
    alive = true; paintChrome('#3d2e73'); backButton.set(null);
    mainButton.hide();
    element.addEventListener('pointerdown', unlock, { once: true });
    play();
    api(`mifs/${snippet.mifId}`).then(data => {
      if (!alive) return;
      resolved = data; snippet = { ...data.snippet, intent: snippet.intent };
      element.querySelector('.titles').innerHTML = titles(data.track);
      if (data.track.artworkURL) element.querySelector('.art').src = data.track.artworkURL;
      lyrics.replaceChildren(...snippet.lyrics.map(line => {
        const p = document.createElement('p'); p.textContent = line.text; return p;
      }));
      applyTint(element, data.track, view);
      if (snippet.intent === 'send') mainButton.show('Send to Chat', () => send(snippet, data.track), { light: true });
      else mainButton.show('Reply with a Mif', () => mount(browserView()), { light: true });
      tick();
    }).catch(error => { if (alive) status.textContent = error.message; });
  }
  function leave() { alive = false; audio.pause(); audio.removeAttribute('src'); audio.load(); }
  function tick() {
    if (!alive) return;
    const duration = resolved?.snippet.duration ?? 0;
    if (duration && audio.currentTime >= duration && !audio.paused) audio.pause();
    button.render(audio.paused ? 'idle' : 'playing', duration ? clamp(audio.currentTime / duration, 0, 1) : 0);
    const hasLyrics = Boolean(snippet.lyrics?.length);
    lyrics.hidden = audio.paused || !hasLyrics; art.hidden = !audio.paused && hasLyrics;
    const time = ((snippet.start ?? 0) + audio.currentTime) * 1000;
    [...lyrics.children].forEach((p, i) => p.classList.toggle('sung', time >= snippet.lyrics[i].startMs && time < snippet.lyrics[i].endMs));
  }
  return view;
}

// MARK: - Browser

function browserView({ onBack } = {}) {
  const element = html(`
    <section class="browser">
      <div class="search-bar">
        <label class="field">${icons.search}<input type="search" placeholder="Songs or artists" enterkeyhint="search"
          autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" aria-label="Search songs"></label>
      </div>
      <div class="content"></div>
    </section>`);
  const input = element.querySelector('input');
  const content = element.querySelector('.content');
  const tracks = new Map();
  let charts = null;
  let chartsError = null;
  let chartsLoading = false;
  let results = [];
  let searching = false;
  let searchError = null;
  let term = '';
  let timer = null;
  let controller = null;

  const view = { element, enter, update };

  input.addEventListener('input', () => {
    term = input.value.trim();
    clearTimeout(timer);
    controller?.abort();
    results = [];
    searchError = null;
    searching = Boolean(term);
    render();
    if (term) timer = setTimeout(runSearch, 350);
  });
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') input.blur();
  });
  content.addEventListener('click', (event) => {
    if (event.target.closest('.retry-charts')) return loadCharts(true);
    const row = event.target.closest('[data-id]');
    const track = row && tracks.get(row.dataset.id);
    if (!track) return;
    {
      input.blur();
      call('hideKeyboard');
      mount(editorView(track, { onBack: () => mount(view) }));
    }
  });

  function enter() {
    paintChrome(null);
    call('expand');
    mainButton.hide();
    backButton.set(onBack ?? null);
    render();
    loadCharts();
  }

  async function loadCharts(retry = false) {
    if (charts || chartsLoading || (chartsError && !retry)) return;
    chartsError = null;
    chartsLoading = true;
    render();
    try {
      charts = await topSongs(storefront);
    } catch (error) {
      chartsError = error instanceof CatalogError ? error.message : 'Couldn’t reach MIFS. Check your connection and try again.';
    }
    chartsLoading = false;
    render();
  }

  async function runSearch() {
    const query = term;
    controller = new AbortController();
    try {
      const found = await search(query, storefront, { signal: controller.signal });
      if (query !== term) return;
      results = found;
      searchError = null;
    } catch (error) {
      if (error.name === 'AbortError') return;
      searchError = error instanceof CatalogError ? error.message : 'Couldn’t reach MIFS. Check your connection and try again.';
    }
    searching = false;
    render();
  }

  function render() {
    if (term) {
      let body;
      if (results.length) body = `<div class="list">${results.map((track) => row(track)).join('')}</div>`;
      else if (searching) body = placeholders();
      else if (searchError) body = message(searchError);
      else body = message(`No songs found for “${escapeHTML(term)}”.`);
      content.innerHTML = body;
      results.forEach((track) => tracks.set(track.id, track));
    } else {
      const intro = `
        <div class="intro">
          <h2>Send the moment, not the whole song</h2>
          <p>${fromInline ? 'Pick a song and any up to 20 seconds of it. It goes straight back to your chat.'
            : 'Pick a song and any up to 20 seconds of it. It plays right in the chat.'}</p>
        </div>`;
      let list;
      if (charts?.length) list = `<div class="list">${charts.map((track, index) => row(track, index + 1)).join('')}</div>`;
      else if (chartsError) list = message(chartsError, 'Retry', 'retry-charts');
      else list = placeholders();
      content.innerHTML = `${intro}<h2 class="section-title">Top Songs</h2>${list}`;
      charts?.forEach((track) => tracks.set(track.id, track));
    }
    update();
  }

  function update() {
    for (const button of content.querySelectorAll('.preview')) {
      const active = player.isActive(`row-${button.closest('[data-id]').dataset.id}`);
      if (button.dataset.active === String(active)) continue;
      button.dataset.active = String(active);
      button.innerHTML = active ? icons.rowStop : icons.rowPlay;
      button.setAttribute('aria-label', active ? 'Stop preview' : 'Play preview');
    }
  }

  return view;
}

function row(track, rank) {
  return `
    <div class="row" data-id="${escapeHTML(track.id)}">
      <button class="open" type="button">
        ${rank ? `<span class="rank">${rank}</span>` : ''}
        ${track.artworkURL ? `<img src="${escapeHTML(track.artworkURL.replace('600x600bb', '200x200bb'))}" alt="" loading="lazy">` : '<span class="thumb"></span>'}
        <span class="text">
          <strong>${escapeHTML(track.title)}${track.explicit ? '<span class="explicit" aria-label="Explicit"></span>' : ''}</strong>
          <span>${escapeHTML(track.artist)}</span>
        </span>
      </button>

    </div>`;
}

function placeholders() {
  const item = `<div class="row placeholder" aria-hidden="true"><span class="thumb"></span>
    <span class="text" style="flex:1;gap:7px"><span class="line" style="width:60%"></span><span class="line" style="width:35%"></span></span></div>`;
  return `<div class="list">${item.repeat(6)}</div>`;
}

function message(text, action, actionClass) {
  return `<div class="message">${escapeHTML(text)}${action ? `<br><button type="button" class="${actionClass}">${action}</button>` : ''}</div>`;
}

// MARK: - Editor

function editorView(track, { onBack }) {
  const element = html(`
    <section class="editor dark">
      <div class="backdrop"><div class="backdrop-art"></div></div>
      <div class="screen">
        <div class="art-wrap"><img class="art playing" alt=""></div>
        <div class="titles">${titles(track)}</div>
        <div class="loading"><p>Your song is downloading</p><progress max="1"></progress></div>
        <div class="timeline" hidden>
          <div class="range"><span class="from"></span><strong class="length"></strong><span class="to"></span></div>
          <canvas class="scrubber" tabindex="0" role="slider" aria-label="Snippet start"></canvas>
          <p class="hint">Drag the waveform or its edges · up to 20 seconds</p>
        </div>
        <div class="editor-lyrics mif-lyrics"></div>
        <div class="controls" hidden><div class="lengths" role="radiogroup" aria-label="Snippet length"></div></div>
      </div>
    </section>`);
  const art = element.querySelector('.art');
  const loading = element.querySelector('.loading');
  const timeline = element.querySelector('.timeline');
  const controls = element.querySelector('.controls');
  const lengths = element.querySelector('.lengths');
  const scrubber = element.querySelector('.scrubber');
  const button = playButton();
  controls.prepend(button.element);
  if (track.artworkURL) art.src = track.artworkURL;

  const owner = `editor-${track.id}`;
  let buffer = null;
  let waveform = null;
  let start = 0;
  let length = 10;
  let drag = null;
  let loaded = false;
  let lyricLines = [];
  let loadingController = null;

  const view = { element, enter, leave, update, tick };

  const maxStart = () => Math.max(0, waveform.duration - length);
  const availableLengths = () => {
    const fitting = LENGTHS.filter((option) => option <= waveform.duration + 0.05);
    return fitting.length ? fitting : [waveform.duration];
  };

  function enter() {
    paintChrome(element.dataset.tint ?? '#3d2e73');
    call('expand');
    call('disableVerticalSwipes'); // dragging the waveform shouldn't pull the Mini App down
    backButton.set(onBack);
    mainButton.show('Send to Chat', sendSnippet, { light: true, enabled: Boolean(waveform) });
    applyTint(element, track, view);
    if (!loaded) {
      loaded = true;
      load();
    }
  }

  function leave() {
    loadingController?.abort();
    call('enableVerticalSwipes');
  }

  async function load() {
    loadingController?.abort(); loadingController = new AbortController();
    loading.innerHTML = '<p>Your song is downloading</p><progress max="1"></progress>';
    try {
      const prepared = await prepareTrack(track, song => {
        const progress = loading.querySelector('progress');
        if (song.downloadTotalBytes > 0) {
          progress.value = clamp(song.downloadedBytes / song.downloadTotalBytes, 0, 1);
          loading.querySelector('p').textContent = progress.value >= 1 ? 'Finishing your song…' : `Your song is downloading · ${Math.floor(progress.value * 100)}%`;
        } else progress.removeAttribute('value');
      }, loadingController.signal);
      track = prepared.track;
      waveform = { rms: prepared.waveform.rms, levels: contrast(prepared.waveform.rms), duration: prepared.waveform.durationMs / 1000 };
      lyricLines = prepared.lyrics;
      if (current !== view) return;
      const panel = element.querySelector('.editor-lyrics');
      panel.replaceChildren(...lyricLines.map(line => {
        const b = document.createElement('button'); b.type = 'button'; b.textContent = line.text;
        b.onclick = () => { start = clamp(line.startMs / 1000 - 0.25, 0, maxStart()); playSelection(); update(); };
        return b;
      }));
    } catch (error) {
      if (error.name === 'AbortError') { loaded = false; return; }
      loading.innerHTML = `<p>${escapeHTML(error.message)}</p><button class="retry" type="button">Try Again</button>`;
      loading.querySelector('.retry').addEventListener('click', load); return;
    }
    length = Math.min(10, waveform.duration); start = loudestWindow(waveform.rms, length);
    loading.hidden = true; timeline.hidden = false; controls.hidden = false;
    renderLengths(); mainButton.enable(true); playSelection(); update();
  }

  function renderLengths() {
    lengths.innerHTML = availableLengths().map((option) => `
      <button type="button" role="radio" data-length="${option}" aria-checked="${Math.abs(option - length) < 0.01}"
        aria-label="${Math.round(option)} seconds">${Math.round(option)}s</button>`).join('');
  }

  lengths.addEventListener('click', (event) => {
    const option = Number(event.target.closest('[data-length]')?.dataset.length);
    if (!option || Math.abs(option - length) < 0.01) return;
    const centre = start + length / 2; // keep the selection centred on the same moment
    length = Math.min(option, waveform.duration);
    start = clamp(centre - length / 2, 0, maxStart());
    haptics.selection();
    renderLengths();
    playSelection();
    update();
  });

  button.element.addEventListener('click', () => {
    haptics.tap();
    if (player.isActive(owner)) player.stop();
    else playSelection();
  });

  function playSelection() {
    if (waveform) player.play({ owner, url: track.previewURL, start, duration: length });
  }

  // Drag anywhere: inside the window moves it, elsewhere centres it under the finger.
  scrubber.addEventListener('pointerdown', (event) => {
    if (!waveform) return;
    const time = timeAt(event);
    const inside = time >= start && time <= start + length;
    const tolerance = 14 * waveform.duration / scrubber.getBoundingClientRect().width;
    const edge = Math.abs(time - start) < tolerance ? 'start' : Math.abs(time - start - length) < tolerance ? 'end' : null;
    drag = { edge, end: start + length, start, offset: inside ? time - start : length / 2 };
    scrubber.setPointerCapture(event.pointerId);
    player.stop();
    moveTo(time);
  });
  scrubber.addEventListener('pointermove', (event) => {
    if (drag) moveTo(timeAt(event));
  });
  const endDrag = () => {
    if (!drag) return;
    drag = null;
    haptics.selection();
    playSelection();
  };
  scrubber.addEventListener('pointerup', endDrag);
  scrubber.addEventListener('pointercancel', endDrag);
  scrubber.addEventListener('keydown', (event) => {
    if (!waveform || !['ArrowLeft', 'ArrowRight'].includes(event.key)) return;
    event.preventDefault();
    start = clamp(start + (event.key === 'ArrowRight' ? 1 : -1), 0, maxStart());
    playSelection();
    update();
  });

  function timeAt(event) {
    const rect = scrubber.getBoundingClientRect();
    return ((event.clientX - rect.left) / rect.width) * waveform.duration;
  }

  function moveTo(time) {
    if (drag.edge === 'start') { start = clamp(time, Math.max(0, drag.end - 20), drag.end - 1); length = drag.end - start; }
    else if (drag.edge === 'end') { length = clamp(time - drag.start, 1, Math.min(20, waveform.duration - drag.start)); }
    else start = clamp(time - drag.offset, 0, maxStart());
    renderLengths();
    update();
  }

  async function sendSnippet() {
    if (!waveform) return;
    mainButton.busy(true);
    try {
      const mif = await api(`songs/${track.id}/mifs`, { body: { startMs: Math.round(start * 1000), durationMs: Math.round(length * 1000) } });
      await send({ mifId: mif.id, start, duration: length, lyrics: mif.lyrics }, track);
    } catch (error) { notify(error.message); }
    finally { mainButton.busy(false); }
  }

  function update() {
    const active = player.isActive(owner);
    button.render(active ? player.state : 'idle', active ? player.progress : 0);
    if (!waveform) return;
    element.querySelector('.from').textContent = clock(start);
    element.querySelector('.to').textContent = clock(start + length);
    element.querySelector('.length').textContent = `${Math.round(length)}s snippet`;
    scrubber.setAttribute('aria-valuetext', `${clock(start)} to ${clock(start + length)}`);
    drawScrubber();
  }

  function tick() {
    const active = player.isActive(owner);
    button.render(active ? player.state : 'idle', active ? player.progress : 0);
    if (waveform) drawScrubber();
  }

  function drawScrubber() {
    const { context, width, height } = prepareCanvas(scrubber);
    const pps = width / waveform.duration;
    const step = 4;
    const barWidth = step * 0.62;
    const pointsPerBar = (step / pps) * 10;
    context.fillStyle = 'rgba(255, 255, 255, 0.9)';
    for (let bar = 0; bar * step < width; bar += 1) {
      const lower = Math.floor(bar * pointsPerBar);
      const upper = Math.max(lower + 1, Math.floor((bar + 1) * pointsPerBar));
      const level = lower < waveform.levels.length ? Math.max(...waveform.levels.slice(lower, upper)) : 0.05;
      const barHeight = Math.max(barWidth, height * 0.9 * level);
      roundRect(context, bar * step, (height - barHeight) / 2, barWidth, barHeight, barWidth / 2);
      context.fill();
    }

    const x = start * pps;
    const windowWidth = length * pps;
    context.fillStyle = 'rgba(0, 0, 0, 0.35)';
    context.fillRect(0, 0, x, height);
    context.fillRect(x + windowWidth, 0, width - x - windowWidth, height);
    roundRect(context, x + 1.25, 1.25, windowWidth - 2.5, height - 2.5, 12);
    context.fillStyle = 'rgba(255, 255, 255, 0.10)';
    context.fill();
    context.lineWidth = 2.5;
    context.strokeStyle = '#ffffff';
    context.stroke();
    context.fillStyle = '#fff';
    roundRect(context, x - 3, height * 0.25, 6, height * 0.5, 3); context.fill();
    roundRect(context, x + windowWidth - 3, height * 0.25, 6, height * 0.5, 3); context.fill();

    if (player.isActive(owner) && player.state === 'playing') {
      context.fillStyle = '#ffffff';
      roundRect(context, x + Math.max(0, windowWidth * player.progress - 1.25), 6, 2.5, height - 12, 1.25);
      context.fill();
    }
  }

  return view;
}

// MARK: - Shared pieces

const icons = {
  play: '<svg class="glyph play-glyph" viewBox="0 0 24 24" aria-hidden="true"><path d="M7 4.6v14.8a1.1 1.1 0 0 0 1.7.94l11.6-7.4a1.1 1.1 0 0 0 0-1.88L8.7 3.66A1.1 1.1 0 0 0 7 4.6z"/></svg>',
  stop: '<svg class="glyph" viewBox="0 0 24 24" aria-hidden="true"><rect x="5.5" y="5.5" width="13" height="13" rx="2.5"/></svg>',
  search: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 3a7 7 0 1 0 4.2 12.6l4.6 4.6a1 1 0 0 0 1.4-1.4l-4.6-4.6A7 7 0 0 0 10 3zm0 2a5 5 0 1 1 0 10 5 5 0 0 1 0-10z"/></svg>',
  rowPlay: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm0 1.6a8.4 8.4 0 1 1 0 16.8 8.4 8.4 0 0 1 0-16.8zM10 8.4v7.2c0 .5.5.8.9.5l5.5-3.6a.6.6 0 0 0 0-1L10.9 7.9c-.4-.3-.9 0-.9.5z"/></svg>',
  rowStop: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zM9.2 8.5h5.6c.4 0 .7.3.7.7v5.6c0 .4-.3.7-.7.7H9.2a.7.7 0 0 1-.7-.7V9.2c0-.4.3-.7.7-.7z"/></svg>',
};

function playButton(size = '') {
  const element = html(`
    <button class="play ${size}" type="button" aria-label="Play">
      <svg class="ring" viewBox="0 0 36 36" aria-hidden="true"><circle cx="18" cy="18" r="16.75" pathLength="100"/></svg>
      <span class="face"></span>
    </button>`);
  const face = element.querySelector('.face');
  const ring = element.querySelector('circle');
  return {
    element,
    render(state, progress) {
      ring.style.strokeDashoffset = String(100 - progress * 100);
      if (element.dataset.state === state) return;
      element.dataset.state = state;
      face.innerHTML = state === 'loading' ? '<span class="spinner"></span>' : state === 'playing' ? icons.stop : icons.play;
      element.setAttribute('aria-label', state === 'idle' ? 'Play' : 'Stop');
    },
  };
}

function titles(track) {
  return `<h1>${escapeHTML(track.title)}${track.explicit ? '<span class="explicit" aria-label="Explicit"></span>' : ''}</h1>
    <p>${escapeHTML(track.artist)}</p>`;
}

/// Rounded bars with a played/unplayed split, like the app's WaveformBars.
function drawBars(canvas, levels, progress) {
  const { context, width, height } = prepareCanvas(canvas);
  if (!levels.length) return;
  const spacing = 3;
  const barWidth = Math.max(1, (width - spacing * (levels.length - 1)) / levels.length);
  levels.forEach((level, index) => {
    const x = index * (barWidth + spacing);
    const barHeight = Math.max(barWidth, height * level);
    context.fillStyle = x + barWidth / 2 <= width * progress ? '#ffffff' : 'rgba(255, 255, 255, 0.35)';
    roundRect(context, x, (height - barHeight) / 2, barWidth, barHeight, barWidth / 2);
    context.fill();
  });
}

function prepareCanvas(canvas) {
  const { width, height } = canvas.getBoundingClientRect();
  const scale = window.devicePixelRatio || 1;
  if (canvas.width !== Math.round(width * scale) || canvas.height !== Math.round(height * scale)) {
    canvas.width = Math.round(width * scale);
    canvas.height = Math.round(height * scale);
  }
  const context = canvas.getContext('2d');
  context.setTransform(scale, 0, 0, scale, 0, 0);
  context.clearRect(0, 0, width, height);
  return { context, width, height };
}

function roundRect(context, x, y, width, height, radius) {
  context.beginPath();
  if (context.roundRect) context.roundRect(x, y, Math.max(0, width), Math.max(0, height), radius);
  else context.rect(x, y, Math.max(0, width), Math.max(0, height));
}

// MARK: - Artwork tint (as ArtworkLoader.tint: a dark, saturated colour behind white text)

const tints = new Map();

function applyTint(element, track, view) {
  element.querySelector('.backdrop-art').style.backgroundImage = track.artworkURL ? `url("${track.artworkURL}")` : '';
  tintFor(track.artworkURL).then((tint) => {
    if (!tint) return;
    element.dataset.tint = tint;
    element.style.setProperty('--tint', tint);
    if (current === view) paintChrome(tint);
  });
}

function tintFor(url) {
  if (!url) return Promise.resolve(null);
  if (!tints.has(url)) {
    tints.set(url, (async () => {
      const image = new Image();
      image.crossOrigin = 'anonymous';
      image.src = url.replace(/\/\d+x\d+bb\./, '/64x64bb.');
      await image.decode();
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 16;
      const context = canvas.getContext('2d', { willReadFrequently: true });
      context.drawImage(image, 0, 0, 16, 16);
      const pixels = context.getImageData(0, 0, 16, 16).data;
      let red = 0;
      let green = 0;
      let blue = 0;
      for (let index = 0; index < pixels.length; index += 4) {
        red += pixels[index];
        green += pixels[index + 1];
        blue += pixels[index + 2];
      }
      const count = pixels.length / 4;
      const [hue, saturation, brightness] = hsb(red / count / 255, green / count / 255, blue / count / 255);
      return hex(hue, clamp(saturation * 1.15, 0.25, 0.85), clamp(brightness * 0.8, 0.22, 0.45));
    })().catch(() => null));
  }
  return tints.get(url);
}

function hsb(red, green, blue) {
  const max = Math.max(red, green, blue);
  const delta = max - Math.min(red, green, blue);
  let hue = 0;
  if (delta) {
    if (max === red) hue = ((green - blue) / delta) % 6;
    else if (max === green) hue = (blue - red) / delta + 2;
    else hue = (red - green) / delta + 4;
  }
  return [((hue * 60) + 360) % 360, max ? delta / max : 0, max];
}

function hex(hue, saturation, brightness) {
  const channel = (n) => {
    const k = (n + hue / 60) % 6;
    const value = brightness - brightness * saturation * Math.max(0, Math.min(k, 4 - k, 1));
    return Math.round(value * 255).toString(16).padStart(2, '0');
  };
  return `#${channel(5)}${channel(3)}${channel(1)}`;
}

// MARK: - Utilities

function html(markup) {
  const template = document.createElement('template');
  template.innerHTML = markup.trim();
  return template.content.firstElementChild;
}

function escapeHTML(value) {
  return String(value ?? '').replace(/[&<>"']/g, (character) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[character]
  ));
}

// MARK: - Launch

tg?.ready();
unlockOnFirstGesture();
document.addEventListener('visibilitychange', () => {
  if (document.hidden) { player.stop(); current?.pause?.(); }
});
tg?.onEvent?.('deactivated', () => { player.stop(); current?.pause?.(); });

mount(launchCode ? playerView(launchCode) : browserView());
requestAnimationFrame(frame);

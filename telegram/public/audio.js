// Snippet audio for the Mini App, matching the iOS app: songs stream from the MIFS music server, and the
// server's loudness envelope drives the waveform and the suggested moment (WaveformAnalyzer).

import { clamp } from './snippet-code.js';

export const RESOLUTION = 10; // envelope points per second, as Waveform.resolution

let sharedContext;

function audioContext() {
  if (!sharedContext) {
    // Play through the ringer switch on iOS, like the app's .playback audio session.
    if (navigator.audioSession) navigator.audioSession.type = 'playback';
    sharedContext = new (window.AudioContext || window.webkitAudioContext)();
  }
  return sharedContext;
}

/// Browsers only start audio from a gesture; the first tap anywhere unlocks it for later autoplay.
export function unlockOnFirstGesture() {
  const unlock = () => {
    audioContext().resume().catch(() => {});
    window.removeEventListener('pointerdown', unlock, true);
  };
  window.addEventListener('pointerdown', unlock, true);
}

/// Plays one range at a time. `onChange` fires whenever `state` changes: 'idle' | 'loading' | 'playing'.
export class SnippetPlayer {
  constructor(onChange = () => {}) {
    this.onChange = onChange;
    this.state = 'idle';
    this.owner = null;
    this.element = null;
  }

  /// Streams `duration` seconds of `url` from `start`.
  async play({ owner, url, start, duration }) {
    this.stop();
    this.owner = owner;
    this.range = { start, duration };
    this.set('loading');

    if (url) {
      const element = new Audio(url);
      element.crossOrigin = 'anonymous';
      this.element = element;
      element.currentTime = start;
      element.ontimeupdate = () => { if (element.currentTime >= start + duration) this.finish(owner); };
      element.onended = () => this.finish(owner);
      try {
        await element.play();
      } catch {
        return this.finish(owner);
      }
      if (this.owner !== owner) return false;
    } else {
      return this.finish(owner);
    }
    this.set('playing');
    return true;
  }

  toggle(options) {
    if (this.isActive(options.owner)) {
      this.stop();
      return Promise.resolve(false);
    }
    return this.play(options);
  }

  isActive(owner) {
    return this.owner === owner && this.state !== 'idle';
  }

  /// 0…1 through the playing range.
  get progress() {
    if (this.state !== 'playing' || !this.range) return 0;
    const elapsed = (this.element?.currentTime ?? this.range.start) - this.range.start;
    return clamp(elapsed / this.range.duration, 0, 1);
  }

  stop() {
    if (this.owner !== null) this.finish(this.owner);
  }

  finish(owner) {
    if (this.owner !== owner) return false;
    if (this.element) {
      this.element.pause();
      this.element.removeAttribute('src');
    }
    this.element = null;
    this.owner = null;
    this.set('idle');
    return false;
  }

  set(state) {
    this.state = state;
    this.onChange(state);
  }
}

// MARK: - Analysis (ports of Waveform / WaveformAnalyzer)

/// RMS mapped to 0…1 in decibels between the track's own quiet floor and its peak.
export function contrast(rms) {
  const decibels = rms.map((value) => 20 * Math.log10(Math.max(value, 1e-5)));
  const sorted = [...decibels].sort((a, b) => a - b);
  if (sorted.length === 0) return [];
  const peak = sorted[sorted.length - 1];
  const floor = Math.min(sorted[Math.floor(sorted.length / 20)] - 2, peak - 6);
  return decibels.map((value) => 0.1 + 0.9 * Math.pow(clamp((value - floor) / (peak - floor), 0, 1), 1.4));
}

/// Start of the most energetic `length`-second window — usually the hook or chorus.
export function loudestWindow(rms, length) {
  const window = Math.floor(length * RESOLUTION);
  if (rms.length <= window || window <= 0) return 0;
  const energy = rms.map((value) => value * value);
  let sum = energy.slice(0, window).reduce((total, value) => total + value, 0);
  let best = sum;
  let bestIndex = 0;
  for (let index = window; index < energy.length; index += 1) {
    sum += energy[index] - energy[index - window];
    if (sum > best) {
      best = sum;
      bestIndex = index - window + 1;
    }
  }
  return bestIndex / RESOLUTION;
}

/// The snippet's own waveform: `bars` peak levels across the range, normalised to its loudest bar.
export function snippetLevels(levels, start, length, bars) {
  if (levels.length === 0 || bars <= 0) return new Array(bars).fill(0.1);
  const first = Math.floor(start * RESOLUTION);
  const last = Math.max(first + 1, Math.floor((start + length) * RESOLUTION));
  const slice = levels.slice(clamp(first, 0, levels.length - 1), clamp(last, 1, levels.length));
  if (slice.length === 0) return new Array(bars).fill(0.1);
  const resampled = Array.from({ length: bars }, (_, bar) => {
    const lower = Math.floor((bar * slice.length) / bars);
    const upper = Math.max(lower + 1, Math.floor(((bar + 1) * slice.length) / bars));
    return Math.max(...slice.slice(lower, Math.min(upper, slice.length)));
  });
  const peak = Math.max(...resampled);
  return resampled.map((value) => (peak > 0 ? Math.max(0.08, value / peak) : 0.08));
}

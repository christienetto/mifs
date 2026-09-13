# Seed content credits and licence

The three songs in this directory are original demo recordings made for the MIFS prototype.
Their compositions (chords, melodies, arrangements), recordings, lyrics and artwork were all
created from code by [`server/tools/demogen`](../tools/demogen). No samples, loops, melodies or
lyrics from existing works were used.

| Song | Artist | Album | Genre | Length |
| --- | --- | --- | --- | --- |
| Neon Harbor | Orchid Relay | Night Signals (track 1) | Synthwave, 100 BPM | 2:07 |
| Send Me the Chorus | Orchid Relay | Night Signals (track 3) | Dance Pop, 118 BPM | 1:56 |
| Paper Satellites | Juniper Fold | — (single) | Lo-fi Hip Hop, 84 BPM | 2:20 |

The artists and album are fictional.

## Licence

All of it (audio, lyrics, artwork and metadata) is dedicated to the public domain under
[CC0 1.0 Universal](https://creativecommons.org/publicdomain/zero/1.0/). You may copy, modify,
distribute and use it commercially without asking permission.

## How it was made

- **Instruments**: synthesised sample by sample in Go (band-limited oscillators, state-variable
  filters, synthetic drums, a Freeverb-style reverb, ping-pong delay, sidechain ducking and a
  look-ahead limiter). Everything is mastered to about −14 LUFS integrated, with true peak
  at or below −1 dBTP.
- **Vocals**: spoken lines rendered with [Piper TTS](https://github.com/OHF-Voice/piper1-gpl)
  (`piper-tts` 1.8.0) using the **`en_US-ljspeech-high`** voice from
  [rhasspy/piper-voices](https://huggingface.co/rhasspy/piper-voices/tree/main/en/en_US/ljspeech/high).
  - The voice's model card lists its training data as the
    [LJ Speech dataset](https://keithito.com/LJ-Speech-Dataset/), licence **public domain**.
  - The `rhasspy/piper-voices` repository is published under the **MIT** licence.
  - Piper's generated speech is output, not a copy of the software, so the GPL licence of
    the Piper program does not apply to these recordings.
  - macOS system voices (`say`) were deliberately **not** used. Their licence allows only
    personal, non-commercial use and forbids redistribution.
- **Lyrics timing**: every line was placed on the beat grid by the generator, and
  `lyrics.lrc` is written from those placements, so the timestamps are the exact vocal
  onsets. They were not transcribed by hand.
- **Artwork**: generative vector-style art drawn with signed-distance shapes in Go
  (1400 × 1400 JPEG).
- **Encoding**: ffmpeg, using the AAC (AudioToolbox) encoder at 256 kb/s in an MP4
  container with `+faststart`.

# demogen

`demogen` is a one-off generator for the three original demo songs in [`server/seed`](../../seed),
released under CC0. It writes one folder per song containing `audio.m4a`, `artwork.jpg`,
`lyrics.lrc` and `song.json`. See [`seed/CREDITS.md`](../../seed/CREDITS.md) for licence details
and provenance.

The outputs are committed to the repository, so **you don't need to run this** to use the
server. It's here so anyone can see, and reproduce, how the content was made.

## Requirements

- Go (standard library only)
- `ffmpeg` on your `PATH`
- [Piper TTS](https://github.com/OHF-Voice/piper1-gpl) and the `en_US-ljspeech-high` voice

Piper can live in a throwaway virtual environment:

```sh
python3 -m venv /tmp/piper && /tmp/piper/bin/pip install piper-tts
V=https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/ljspeech/high
curl -LO $V/en_US-ljspeech-high.onnx -LO $V/en_US-ljspeech-high.onnx.json
```

## Run

From the `server/` directory:

```sh
go run ./tools/demogen -piper /tmp/piper/bin/piper -voice en_US-ljspeech-high.onnx
go run ./tools/demogen -only paper-satellites ...   # render a single song
```

| Flag | Effect |
| --- | --- |
| `-out` | Seed directory to write into (default `seed`). |
| `-work` | Cache for Piper renders and stems (default: your temp dir). The dry vocal stem `<id>-vocals.wav` in this directory is handy for checking lyric sync. |
| `-format` | `m4a` (the default), `flac`, or `auto` (FLAC unless the file exceeds 12 MB). The seed uses AAC to keep the repository small. |

Piper's synthesis is slightly random, so a re-render won't be bit-identical: line lengths
can shift a little, but every line still lands on the beat grid and the LRC is always rewritten to match.

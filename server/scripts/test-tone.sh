#!/bin/sh
# Stand-in audio source for trying on-demand songs without music files: a quiet tone as
# long as the requested song, so the waveform, synced lyrics and mifs all line up with the
# real song's timing. Use it as MIFS_AUDIO_COMMAND (see .env.example). It's only a tone:
# for real audio, point MIFS_LIBRARY_DIR at music you own.
set -eu
seconds=$(awk "BEGIN { print $MIFS_DURATION_MS / 1000 }")
"${MIFS_FFMPEG:-ffmpeg}" -v error -f lavfi -i "sine=frequency=220:duration=$seconds" \
  -af "volume=0.3" -ac 2 "$MIFS_OUT_DIR/tone.wav"

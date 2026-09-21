export async function api(path, { signal, body } = {}) {
  const response = await fetch(`/api/${path}`, {
    signal, method: body ? 'POST' : 'GET',
    headers: body ? { 'Content-Type': 'application/json', 'X-Telegram-Init-Data': window.Telegram?.WebApp?.initData ?? '' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error?.message || data.error || 'Couldn’t reach MIFS.');
  return data;
}

function track(song) {
  return { id: song.id, title: song.title, artist: song.artist, explicit: song.explicit,
    artworkURL: song.artwork?.url, thumbnailURL: song.artwork?.thumbnailUrl, server: true, status: song.status,
    previewURL: `/api/songs/${encodeURIComponent(song.id)}/audio` };
}
export async function serverTopSongs() { return (await api('songs')).songs.map(track); }
export async function serverSearch(query, options = {}) {
  return (await api(`search?q=${encodeURIComponent(query)}`, options)).results.map(result => ({
    ...(result.song ? track(result.song) : track({ ...result, id: result.ref })), ref: result.ref,
  }));
}
export async function prepareTrack(selected, onProgress, signal) {
  let song = selected.ref ? await api('songs', { body: { ref: selected.ref }, signal })
    : await api(`songs/${encodeURIComponent(selected.id)}`, { signal });
  const deadline = Date.now() + 360000;
  while (song.status !== 'ready') {
    if (['failed', 'unavailable'].includes(song.status)) throw new Error(song.statusMessage || 'Couldn’t download this song. Try again.');
    if (Date.now() >= deadline) throw new Error('This song is still downloading. Try again shortly.');
    onProgress(song);
    await new Promise(resolve => setTimeout(resolve, 500));
    if (signal.aborted) throw new DOMException('Cancelled', 'AbortError');
    song = await api(`songs/${song.id}`, { signal });
  }
  const [waveform, lyrics] = await Promise.all([
    api(`songs/${song.id}/waveform`, { signal }),
    api(`songs/${song.id}/lyrics`, { signal }).catch(() => ({ lines: [] })),
  ]);
  return { track: track(song), waveform, lyrics: lyrics.lines ?? [] };
}

// Only the configured music server is trusted; clients supply an opaque mif ID, never a URL.
export function mifURL(env, id, audio = false) {
  if (!/^[a-z2-7]{12}$/.test(id)) throw new Error('Invalid mif');
  if (!env.MIFS_SERVER_URL) throw new Error('Music server is not configured');
  const base = new URL(env.MIFS_SERVER_URL);
  if (!['http:', 'https:'].includes(base.protocol)) throw new Error('Invalid music server');
  return new URL(`/v1/mifs/${id}${audio ? '/audio' : ''}`, base).toString();
}

export async function loadMif(env, id, fetchImpl = fetch) {
  const response = await fetchImpl(mifURL(env, id), { signal: AbortSignal.timeout(15000) });
  if (!response.ok) throw new Error('This mif could not be loaded. Try again.');
  const mif = await response.json();
  if (mif.id !== id || !mif.song || !(mif.durationMs > 0) || mif.durationMs > 30000) throw new Error('Invalid mif response');
  return {
    snippet: { mifId: mif.id, trackId: mif.songId, start: mif.startMs / 1000,
      duration: mif.durationMs / 1000, lyrics: Array.isArray(mif.lyrics) ? mif.lyrics : [] },
    track: { id: mif.songId, title: String(mif.song.title).slice(0, 200), artist: String(mif.song.artist).slice(0, 200),
      artworkURL: mif.song.artwork?.url, explicit: mif.song.explicit },
  };
}

export async function mifAudio(request, env, id, fetchImpl = fetch) {
  const headers = new Headers();
  for (const key of ['Range', 'If-None-Match', 'If-Range']) {
    if (request.headers.has(key)) headers.set(key, request.headers.get(key));
  }
  const response = await fetchImpl(mifURL(env, id, true), { method: request.method, headers, signal: AbortSignal.timeout(30000) });
  const out = new Headers();
  for (const key of ['Content-Type', 'Content-Length', 'Content-Range', 'Accept-Ranges', 'ETag', 'Cache-Control']) {
    if (response.headers.has(key)) out.set(key, response.headers.get(key));
  }
  return new Response(response.body, { status: response.status, headers: out });
}

export async function musicRequest(request, env, fetchImpl = fetch) {
  if (!env.MIFS_SERVER_URL) return Response.json({ error: 'Music server is not configured.' }, { status: 503 });
  const source = new URL(request.url);
  const path = source.pathname.replace('/api/', '/v1/');
  const writable = path === '/v1/songs' || /^\/v1\/songs\/[a-zA-Z0-9_-]{1,100}\/mifs$/.test(path);
  if (!['GET', 'HEAD'].includes(request.method) && !(request.method === 'POST' && writable)) return new Response(null, { status: 405 });
  if (request.method === 'POST') {
    // Downloading and creating mifs are restricted to verified Mini App sessions.
    const { verifyInitData } = await import('./init-data.js');
    const auth = await verifyInitData(request.headers.get('X-Telegram-Init-Data'), env.BOT_TOKEN);
    if (!auth?.user?.id) return Response.json({ error: 'Open MIFS in Telegram to choose a song.' }, { status: 401 });
  }
  let target = new URL(path + source.search, env.MIFS_SERVER_URL);
  if (path.endsWith('/audio')) {
    const metadata = await fetchImpl(new URL(path.slice(0, -6), env.MIFS_SERVER_URL), { signal: AbortSignal.timeout(15000) });
    if (!metadata.ok) return new Response(null, { status: metadata.status });
    const song = await metadata.json();
    if (!song.audio?.url || song.status !== 'ready') return new Response(null, { status: 409 });
    target = new URL(song.audio.url);
  }
  const headers = new Headers();
  for (const key of ['Range', 'If-None-Match', 'If-Range', 'Content-Type']) {
    if (request.headers.has(key)) headers.set(key, request.headers.get(key));
  }
  return fetchImpl(target.toString(), { method: request.method, headers,
    body: request.method === 'POST' ? await request.text() : undefined, signal: AbortSignal.timeout(30000) });
}

// Apple's public catalog, as used by the iOS app's CatalogService: iTunes Search/Lookup for songs and
// 30-second previews, and the iTunes top-songs feed. All of it is CORS-enabled, so the Mini App calls it
// directly; the server uses the same code to build the cards it sends.

const ITUNES = 'https://itunes.apple.com';

export async function lookup(ids, storefront = 'us', fetchImpl = fetch) {
  const list = [].concat(ids).filter(Boolean);
  if (list.length === 0) return [];
  const url = `${ITUNES}/lookup?id=${list.join(',')}&entity=song&country=${storefront}`;
  const byId = new Map((await fetchTracks(url, fetchImpl)).map((track) => [track.id, track]));
  return list.map((id) => byId.get(String(id))).filter(Boolean);
}

export async function lookupTrack(id, storefront = 'us', fetchImpl = fetch) {
  const [track] = await lookup([id], storefront, fetchImpl);
  return track ?? null;
}

export async function search(term, storefront = 'us', { limit = 40, signal } = {}) {
  const params = new URLSearchParams({ term, media: 'music', entity: 'song', limit: String(limit), country: storefront });
  return fetchTracks(`${ITUNES}/search?${params}`, (url) => fetch(url, { signal }));
}

export async function topSongs(storefront = 'us', limit = 25) {
  const response = await fetch(`${ITUNES}/${storefront}/rss/topsongs/limit=${limit}/json`);
  if (!response.ok) throw new CatalogError(response.status);
  const feed = await response.json();
  const ids = (feed?.feed?.entry ?? []).map((entry) => entry?.id?.attributes?.['im:id']).filter(Boolean);
  return lookup(ids, storefront);
}

/// Apple artwork URLs embed their size (".../100x100bb.jpg"); ask for another one.
export function artwork(url, size) {
  return url ? url.replace(/\/\d+x\d+bb\./, `/${size}x${size}bb.`) : null;
}

export function spotifySearchURL(track) {
  return `https://open.spotify.com/search/${encodeURIComponent(`${track.title} ${track.artist}`)}`;
}

/// The user's storefront, from the browser's locale ("en-GB" → "gb").
export function currentStorefront() {
  const region = (navigator.language || '').split('-')[1];
  return /^[a-z]{2}$/i.test(region ?? '') ? region.toLowerCase() : 'us';
}

export class CatalogError extends Error {
  constructor(status) {
    super(status === 403 || status === 429
      ? 'Apple Music is busy right now. Try again in a moment.'
      : 'Couldn’t reach Apple Music. Check your connection and try again.');
    this.status = status;
  }
}

async function fetchTracks(url, fetchImpl) {
  const response = await fetchImpl(url);
  if (!response.ok) throw new CatalogError(response.status);
  const body = await response.json();
  return (body.results ?? []).map(toTrack).filter(Boolean);
}

function toTrack(item) {
  if (item?.kind !== 'song' || !item.trackId || !item.trackName || !item.previewUrl) return null;
  const appleMusicURL = item.trackViewUrl ? item.trackViewUrl.replace(/([?&])uo=\d+&?/, '$1').replace(/[?&]$/, '') : null;
  return {
    id: String(item.trackId),
    title: item.trackName,
    artist: item.artistName ?? '',
    album: item.collectionName ?? null,
    artworkURL: artwork(item.artworkUrl100, 600),
    previewURL: item.previewUrl,
    appleMusicURL,
    explicit: item.trackExplicitness === 'explicit',
  };
}

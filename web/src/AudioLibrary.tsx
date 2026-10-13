import { useEffect, useState } from 'react';
import { api, request } from './api';

type Track = { index: number; title: string; rel_path: string };
type Album = { id: string; title: string; library_name?: string; track_count: number; cover_path: string; tracks: Track[] };
type Props = { active: boolean; onPlay: (path: string, title: string, album: string) => void };
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

// AudioLibrary pages albums/tracks only while active and delegates to one player.
export default function AudioLibrary({ active, onPlay }: Props) {
  const [albums, setAlbums] = useState<Album[]>([]);
  const [offset, setOffset] = useState(0);
  const [more, setMore] = useState(false);
  const [selected, setSelected] = useState<Album>();
  const [album, setAlbum] = useState<Album>();
  const [trackOffset, setTrackOffset] = useState(0);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    setError('');
    request(`/api/media/audio/albums?limit=100&offset=${offset}`, { signal: controller.signal }).then(async response => {
      const next = await response.json() as Album[];
      if (!controller.signal.aborted) { setAlbums(next); setMore(response.headers.get('X-Has-More') === 'true'); }
    }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [active, offset]);

  useEffect(() => {
    if (!active || !selected) return;
    const controller = new AbortController();
    setAlbum(undefined); setError('');
    api<Album>(`/api/media/audio/album?path=${encodeURIComponent(selected.id)}&limit=100&offset=${trackOffset}`, { signal: controller.signal })
      .then(next => { if (!controller.signal.aborted) setAlbum(next); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [active, selected, trackOffset]);

  return <div className="grid gap-5 md:grid-cols-2">
    <section className="space-y-3"><h3 className="text-lg font-semibold">音频专辑</h3>
      {albums.map(item => <button key={item.id} className={`${button} block w-full text-left`} onClick={() => { setSelected(item); setTrackOffset(0); }}>
        {item.title}<span className="ml-2 text-muted">{item.library_name} · {item.track_count} 集</span>
      </button>)}
      <div className="flex gap-3"><button className={button} disabled={offset === 0} onClick={() => setOffset(value => Math.max(0, value - 100))}>上一页</button><button className={button} disabled={!more} onClick={() => setOffset(value => value + 100)}>下一页</button></div>
    </section>
    <section className="space-y-3"><h3 className="text-lg font-semibold">{selected?.title ?? '选择一个专辑'}</h3>
      {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
      {selected && !album && !error ? <p role="status">正在加载分集…</p> : null}
      {album?.cover_path ? <img className="h-40 w-40 rounded-lg object-cover" src={`/api/media/audio/cover?path=${encodeURIComponent(album.cover_path)}`} alt={`${album.title} 封面`} loading="lazy" /> : null}
      {album?.tracks.map(track => <button key={track.rel_path} className={`${button} block w-full text-left`} onClick={() => onPlay(track.rel_path, track.title, album.title)}>{track.index}. {track.title}</button>)}
      {album ? <div className="flex gap-3"><button className={button} disabled={trackOffset === 0} onClick={() => setTrackOffset(value => Math.max(0, value - 100))}>上一页</button><button className={button} disabled={trackOffset + album.tracks.length >= album.track_count} onClick={() => setTrackOffset(value => value + 100)}>下一页</button></div> : null}
    </section>
  </div>;
}

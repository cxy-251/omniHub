import { useEffect, useMemo, useState } from 'react';
import { api } from './api';
import { useAudio, type Track } from './AudioEngine';
import { MusicRatingButtons } from './MusicFeedback';
import MusicCover from './MusicCover';
import VirtualGrid from './VirtualGrid';
import VirtualList from './VirtualList';
import { usePaged } from './paged';
import { useHash } from './useHash';

type Artist = { id: string; title: string; album_count: number; main_artist: boolean; cover_path?: string; library_name?: string };
type Album = { id: string; album: string; count: number; cover_path?: string };
const destination = (artist: string, album = '') => `#audio/albums?${new URLSearchParams({ artist, ...(album ? { album } : {}) })}`;

function AlbumTracks({ album, artist }: { album: Album; artist: Artist }) {
  const engine = useAudio();
  const tracks = usePaged<Track>(`/api/media/audio/tracks?path=${encodeURIComponent(album.id)}`, 60, 4);
  return <div className="album-detail">
    <a className="album-back" href={destination(artist.id)}>← {artist.title}的专辑</a>
    <div className="album-summary"><MusicCover path={album.cover_path} /><div><h2>{album.album}</h2><p>{artist.title} · {album.count} 首</p><button className="workbench-button primary" disabled={!album.count} onClick={() => engine?.open(album.id, album.album, 0, true)}>播放专辑</button></div></div>
    {tracks.error ? <p role="alert" className="text-danger">{tracks.error}</p> : null}
    <VirtualList total={tracks.total} rowHeight={60} className="album-tracks" render={index => {
      const track = tracks.item(index), selected = !!track && track.id === engine?.track?.id;
      return <div className={`album-track ${selected ? 'is-playing' : ''}`}>
        <button disabled={!track} onClick={() => track && engine?.open(album.id, album.album, track.index - 1, true)}><span>{index + 1}</span><span>{track?.title ?? '正在读取…'}</span>{selected ? <span className="track-state">{engine?.playing ? '播放中' : '已暂停'}</span> : null}</button>
        {track ? <MusicRatingButtons id={track.id} engine={engine} /> : null}
      </div>;
    }} />
  </div>;
}

function ArtistAlbums({ artist, selected }: { artist: Artist; selected: string }) {
  const [albums, setAlbums] = useState<Album[]>(), [error, setError] = useState(''), [query, setQuery] = useState(''), [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); setError('');
    api<{ sections: Album[] }>(`/api/media/audio/artist?path=${encodeURIComponent(artist.id)}`, { signal: controller.signal })
      .then(data => { if (!controller.signal.aborted) setAlbums((data.sections ?? []).filter(a => a.id && a.album)); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [artist.id, revision]);
  const shown = useMemo(() => (albums ?? []).filter(a => a.album.toLowerCase().includes(query.trim().toLowerCase())), [albums, query]);
  const album = albums?.find(a => a.id === selected);
  if (error) return <div className="music-empty" role="alert"><p>{error}</p><button className="workbench-button" onClick={() => setRevision(n => n + 1)}>重试</button></div>;
  if (!albums) return <p role="status" className="music-empty">正在读取专辑…</p>;
  if (album) return <AlbumTracks key={album.id} album={album} artist={artist} />;
  return <div className="artist-albums">
    <div className="albums-heading"><div><h2>{artist.title}</h2><p>{albums.length} 张专辑{artist.library_name ? ` · ${artist.library_name}` : ''}</p></div><input type="search" aria-label="搜索当前歌手的专辑" placeholder="搜索这位歌手的专辑" value={query} onChange={e => setQuery(e.target.value)} /></div>
    {selected ? <p role="status" className="text-muted">这张专辑已不在当前歌手的目录中。</p> : null}
    {!shown.length ? <div className="music-empty"><p>{query ? '没有匹配的专辑' : '这位歌手还没有专辑目录'}</p>{!query ? <p>单曲仍可在音乐库中收听。</p> : null}<a className="workbench-button" href="#audio/library">返回音乐库</a></div>
      : <VirtualGrid key={query} total={shown.length} minColumnWidth={180} aspect={1} extraHeight={64} gap={20} className="album-grid" render={index => {
        const item = shown[index];
        return <a className="album-tile" href={destination(artist.id, item.id)}><MusicCover path={item.cover_path} /><span className="album-name" title={item.album}>{item.album}</span><span className="album-caption">{item.count} 首</span></a>;
      }} />}
  </div>;
}

/** Album browsing fetches only the selected artist, never an N+1 full-library fanout. */
export default function MusicAlbums() {
  const hash = useHash(), parameters = new URLSearchParams(hash.split('?')[1]);
  const engine = useAudio();
  const [artists, setArtists] = useState<Artist[]>(), [error, setError] = useState(''), [query, setQuery] = useState(''), [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); setError('');
    api<Artist[]>('/api/media/audio/artists', { signal: controller.signal }).then(data => { if (!controller.signal.aborted) setArtists(data ?? []); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [engine?.musicCatalogVersion, revision]);
  useEffect(() => { void engine?.refreshMusic().catch((e: Error) => setError(e.message)); }, [engine?.refreshMusic]);
  const selected = parameters.get('artist');
  const artist = artists?.find(a => a.id === selected) ?? (!selected ? artists?.find(a => a.main_artist && a.album_count > 0) ?? artists?.find(a => a.album_count > 0) ?? artists?.[0] : undefined);
  const shown = useMemo(() => (artists ?? []).filter(a => a.title.toLowerCase().includes(query.trim().toLowerCase())), [artists, query]);
  return <div className="albums-workspace">
    <aside className="album-artists"><label htmlFor="artist-search">按歌手浏览</label><input id="artist-search" type="search" placeholder="搜索歌手" value={query} onChange={e => setQuery(e.target.value)} />
      {error ? <div role="alert"><p className="text-danger">{error}</p><button className="workbench-button" onClick={() => setRevision(n => n + 1)}>重试</button></div> : null}
      {!artists && !error ? <p role="status">正在读取歌手…</p> : null}
      {artists && !shown.length ? <p className="text-muted">{query ? '没有匹配的歌手' : '音乐库还没有歌手'}</p> : null}
      <VirtualList key={query} total={shown.length} rowHeight={68} className="album-artist-list" render={index => {
        const item = shown[index];
        return <a href={destination(item.id)} className="album-artist" aria-current={item.id === artist?.id ? 'true' : undefined}><MusicCover path={item.cover_path} /><span><strong>{item.title}</strong><small>{item.album_count} 张专辑{item.main_artist ? ' · 常听' : ''}</small></span></a>;
      }} />
    </aside>
    <div className="album-browser">{artist ? <ArtistAlbums key={artist.id + ':' + (engine?.musicCatalogVersion ?? 0)} artist={artist} selected={parameters.get('album') ?? ''} />
      : <div className="music-empty"><h2>{selected ? '这个歌手已不可用' : '专辑收藏'}</h2><p>{selected ? '请选择左侧可用的歌手。' : '选择歌手，浏览资源库里的专辑。'}</p></div>}</div>
  </div>;
}

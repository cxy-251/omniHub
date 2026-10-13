import { useEffect, useRef, useState } from 'react';
import { api } from './api';

const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';
type SlotProps = { active: boolean; channels: string[]; index: number; muted: boolean; focused: boolean; revision: number; onFocus(): void };

function VideoSlot({ active, channels, index, muted, focused, revision, onFocus }: SlotProps) {
  const [channel, setChannel] = useState(channels[index % channels.length] ?? '');
  const [selection, setSelection] = useState(0);
  const [error, setError] = useState('');
  const [playing, setPlaying] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const selected = channels.includes(channel) ? channel : channels[index % channels.length] ?? '';

  useEffect(() => {
    const player = video.current;
    if (!player || !active || !selected) return;
    const controller = new AbortController();
    setError('');
    api<{ path: string }>(`/api/media/shortvideo/random?channel=${encodeURIComponent(selected)}`, { signal: controller.signal })
      .then(async data => {
        if (controller.signal.aborted) return;
        player.src = `/api/media/shortvideo/stream?path=${encodeURIComponent(data.path)}`;
        await player.play();
      }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => { controller.abort(); player.pause(); player.removeAttribute('src'); player.load(); };
  }, [active, selected, selection, revision]);

  function togglePlayback() {
    const player = video.current;
    if (!player) return;
    onFocus();
    if (player.paused) { setError(''); player.play().catch((e: Error) => setError(e.message)); }
    else player.pause();
  }

  return <article className={`overflow-hidden rounded-lg border ${focused ? 'border-primary' : 'border-line'} bg-surface`}>
    <div className="flex flex-wrap items-center gap-2 p-2"><select className="min-w-0 flex-1 rounded border border-line bg-surface p-2 text-sm" value={selected} onChange={event => setChannel(event.target.value)} aria-label={`第 ${index + 1} 屏频道`}>
      {channels.map(name => <option key={name} value={name}>{name}</option>)}
    </select><button className={button} onClick={togglePlayback}>{playing ? '暂停' : '播放'}</button><button className={button} onClick={() => setSelection(value => value + 1)}>换片</button></div>
    <video ref={video} muted={muted} loop playsInline preload="metadata" className="aspect-[9/16] max-h-[65vh] w-full bg-black object-contain" onClick={onFocus} onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)} onError={() => setError('视频不可用或浏览器无法解码')} />
    {error ? <p role="alert" className="break-words p-2 text-sm text-danger">{error}</p> : null}
  </article>;
}

export default function VideoMatrix({ active, onAccompany }: { active: boolean; onAccompany?: () => void }) {
  const [channels, setChannels] = useState<string[]>([]);
  const [count, setCount] = useState(3);
  const [focused, setFocused] = useState(0);
  const [focusAudio, setFocusAudio] = useState(true);
  const [accompany, setAccompany] = useState(false);
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    setLoading(true); setError('');
    api<string[]>('/api/media/shortvideo/channels', { signal: controller.signal })
      .then(next => { if (!controller.signal.aborted) setChannels(next); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [active]);

  return <section className="space-y-4">
    <div className="flex flex-wrap items-center gap-4">
      <label className="flex items-center gap-2 text-sm">分屏<select className="rounded border border-line bg-surface p-2" value={count} onChange={event => { setCount(Number(event.target.value)); setFocused(0); }}><option value={2}>2 屏</option><option value={3}>3 屏</option></select></label>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={focusAudio} onChange={event => setFocusAudio(event.target.checked)} />仅焦点出声</label>
      {onAccompany ? <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={accompany} onChange={event => { setAccompany(event.target.checked); if (event.target.checked) onAccompany(); }} />音频伴听（视频静音）</label> : null}
      <button className={button} disabled={!channels.length} onClick={() => setRevision(value => value + 1)}>全部换片</button>
    </div>
    {error ? <p role="alert" className="text-danger">{error}</p> : null}
    {loading ? <p role="status">正在加载频道…</p> : !channels.length ? <p>暂无短视频频道</p> : null}
    <div className={`grid gap-3 ${count === 2 ? 'md:grid-cols-2' : 'md:grid-cols-3'}`}>
      {channels.length ? Array.from({ length: count }, (_, index) => <VideoSlot key={index} index={index} active={active} channels={channels} focused={focused === index} muted={accompany || focusAudio && focused !== index} revision={revision} onFocus={() => setFocused(index)} />) : null}
    </div>
  </section>;
}

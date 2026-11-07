import { useEffect, useRef, useState } from 'react';
import { api } from './api';
import { formatTime, useAudio, useTrackTags, type Engine } from './AudioEngine';
import { MusicRatingButtons } from './MusicFeedback';
import MusicCover from './MusicCover';
import {
  IconForward30,
  IconPause,
  IconPlay,
  IconRepeatList,
  IconRepeatOne,
  IconRewind10,
  IconShuffle,
  IconSkipBack,
  IconSkipForward,
  IconSleep,
  IconSpeed,
  IconStop,
  ViewerIconButton,
  VolumeSliderPill,
} from './ViewerControls';

type Lyric = { time: number; text: string };
type Lyrics = { synced: boolean; lines: Lyric[] | null };
const modeNames = { list: '列表循环', single: '单曲循环', random: '随机播放' };
const rowHeight = 88;

function LyricsView({ engine, id }: { engine: Engine; id: string }) {
  const [data, setData] = useState<Lyrics>(), [error, setError] = useState(''), [revision, setRevision] = useState(0);
  const [follow, setFollow] = useState(true), [top, setTop] = useState(0), [height, setHeight] = useState(400);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const controller = new AbortController(); setError('');
    api<Lyrics>(`/api/media/audio/lyrics?path=${encodeURIComponent(id)}`, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [id, revision]);
  const lines = data?.lines ?? [];
  // Server returns sorted timestamps; a long LRC file need not be scanned on
  // every audio timeupdate. Only the visible lyric rows become DOM nodes.
  let low = 0, high = lines.length;
  while (low < high) { const mid = (low + high) >>> 1; if (lines[mid].time <= engine.current + .2) low = mid + 1; else high = mid; }
  const current = data?.synced ? low - 1 : -1;
  useEffect(() => {
    const el = box.current; if (!el) return;
    const observer = new ResizeObserver(() => setHeight(el.clientHeight)); observer.observe(el);
    return () => observer.disconnect();
  }, [data]);
  useEffect(() => {
    if (follow && current >= 0 && !document.hidden) box.current?.scrollTo({ top: Math.max(0, current * rowHeight - height / 2 + rowHeight / 2), behavior: 'instant' });
  }, [current, follow, height]);
  const first = Math.max(0, Math.floor(top / rowHeight) - 3), last = Math.min(lines.length, Math.ceil((top + height) / rowHeight) + 3);
  return <div className="listening-lyrics">
    <div className="lyrics-heading"><h2>{data?.synced ? '同步歌词' : '歌词'}</h2>{data?.synced && !follow ? <button className="workbench-button" onClick={() => setFollow(true)}>跟随播放</button> : null}</div>
    {error ? <div className="lyrics-empty" role="alert"><p>歌词暂时无法读取</p><p className="text-muted text-sm">{error}</p><button className="workbench-button" onClick={() => setRevision(v => v + 1)}>重试</button></div>
      : !data ? <div className="lyrics-empty" role="status">正在读取歌词…</div>
      : !lines.length ? <div className="lyrics-empty"><p>这首音频还没有歌词</p><p className="text-muted text-sm">你仍然可以继续收听。</p></div>
      : !data.synced ? <div className="lyrics-scroll lyrics-plain" tabIndex={0} aria-label="纯文本歌词">{lines.map(l => l.text).join('\n')}</div>
      : <div ref={box} className="lyrics-scroll" tabIndex={0} aria-label="同步歌词" onScroll={e => setTop(e.currentTarget.scrollTop)}
        onWheel={() => setFollow(false)} onTouchMove={() => setFollow(false)} onKeyDown={e => { if (['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Home', 'End'].includes(e.key)) setFollow(false); }}>
        <div className="lyrics-canvas" style={{ height: lines.length * rowHeight }}>
          {lines.slice(first, last).map((line, index) => {
            const i = first + index;
            return <button key={i} className="lyric-line" title={line.text} aria-current={i === current ? 'true' : undefined} style={{ top: i * rowHeight, height: rowHeight }}
              onClick={() => { engine.seek(line.time); setFollow(true); }}><span>{line.text || '♪'}</span></button>;
          })}
        </div>
      </div>}
    {lines.length ? <p className="lyrics-hint">{data?.synced ? '点击歌词跳转播放位置' : '纯文本歌词，暂无时间轴'}</p> : null}
  </div>;
}

function CurrentTrack({ engine }: { engine: Engine }) {
  const track = engine.track!;
  const tags = useTrackTags(track.id);
  const ModeIcon = engine.mode === 'single' ? IconRepeatOne : engine.mode === 'random' ? IconShuffle : IconRepeatList;
  return <>
    <div className="listening-grid">
      <div className="listening-artwork"><MusicCover path={tags.has_cover ? track.id : undefined} />
        <h2>{track.title}</h2><p>{[tags.artist || track.artist, tags.album || track.album || engine.albumTitle].filter(Boolean).join(' · ')}</p>
        <div className="listening-rating"><MusicRatingButtons id={track.id} engine={engine} size="md" tooltip="down" /></div>
      </div>
      <LyricsView engine={engine} id={track.id} />
    </div>
    <div className="listening-controls">
      {engine.error ? <p className="text-danger" role="alert">{engine.error}</p> : null}
      <div className="listening-progress"><span>{formatTime(engine.current)}</span><input type="range" aria-label="播放进度" min={0} max={Math.max(1, engine.duration)} step={.1} value={Math.min(engine.current, Math.max(1, engine.duration))} disabled={!engine.duration} onChange={e => engine.seek(Number(e.target.value))} /><span>{formatTime(engine.duration)}</span></div>
      <div className="listening-actions">
        <div className="transport-controls">
          <ViewerIconButton tooltip="up" label="上一首" onClick={engine.previous}><IconSkipBack /></ViewerIconButton>
          <ViewerIconButton tooltip="up" label="后退 10 秒" onClick={() => engine.seekBy(-10)}><IconRewind10 /></ViewerIconButton>
          <ViewerIconButton tooltip="up" size="lg" primary label={engine.playing ? '暂停' : '播放'} onClick={engine.toggle}>{engine.playing ? <IconPause /> : <IconPlay />}</ViewerIconButton>
          <ViewerIconButton tooltip="up" label="前进 30 秒" onClick={() => engine.seekBy(30)}><IconForward30 /></ViewerIconButton>
          <ViewerIconButton tooltip="up" label="下一首" onClick={engine.next}><IconSkipForward /></ViewerIconButton>
        </div>
        <div className="playback-options">
          <VolumeSliderPill tooltip="up" volume={engine.volume} muted={engine.muted} onToggleMute={engine.toggleMute} onChangeVolume={engine.setVolume} />
          <ViewerIconButton tooltip="up" active={engine.mode !== 'list'} label={`模式：${modeNames[engine.mode]} (点击切换)`} onClick={engine.cycleMode}><ModeIcon /></ViewerIconButton>
          <ViewerIconButton tooltip="up" active={engine.rate !== 1} badge={engine.rate !== 1 ? `${engine.rate}×` : undefined} label={`倍速：${engine.rate}× (点击切换)`} onClick={engine.cycleRate}><IconSpeed /></ViewerIconButton>
          <ViewerIconButton tooltip="up" active={engine.sleepMinutes > 0} badge={engine.sleepMinutes ? `${engine.sleepMinutes}m` : undefined} label={engine.sleepMinutes ? `定时关闭：${engine.sleepMinutes} 分钟后` : '定时关闭'} onClick={engine.cycleSleep}><IconSleep /></ViewerIconButton>
          <ViewerIconButton tooltip="up" label="停止播放" onClick={() => engine.stop()}><IconStop /></ViewerIconButton>
        </div>
      </div>
    </div>
  </>;
}

/** A dedicated view of the same player used by collections and accompaniment. */
export default function ListeningPage() {
  const engine = useAudio();
  if (!engine?.track) return <div className="music-empty"><h2>选一首，开始收听</h2><p>从音乐库或专辑中播放，歌词和播放控制会出现在这里。</p><a className="workbench-button primary" href="#audio/library">浏览音乐库</a></div>;
  return <CurrentTrack key={engine.track.id} engine={engine} />;
}

import type { Engine } from './AudioEngine';
import { chip } from './ui';

export function MusicRatingButtons({ id, engine }: { id: string; engine?: Engine }) {
  if (!engine || !id.includes(':media_library/audio/music/')) return null;
  const rating = engine.music.ratings[id] ?? 0;
  const rate = (value: number) => { void engine.rateMusic(id, rating === value ? 0 : value).catch(() => undefined); };
  return <span className="inline-flex shrink-0 gap-1">
    <button type="button" className={chip(rating === 1)} disabled={!engine.music.can_rate} aria-label={rating === 1 ? '取消点赞' : '点赞'} aria-pressed={rating === 1}
      onClick={event => { event.stopPropagation(); rate(1); }}>{rating === 1 ? '♥ 已赞' : '♡ 点赞'}</button>
    <button type="button" className={chip(rating === -1)} disabled={!engine.music.can_rate} aria-label={rating === -1 ? '取消不喜欢' : '不喜欢'} aria-pressed={rating === -1}
      onClick={event => { event.stopPropagation(); rate(-1); }}>{rating === -1 ? '已不喜欢' : '不喜欢'}</button>
  </span>;
}

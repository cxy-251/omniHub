import type { Engine } from './AudioEngine';
import { IconHeart, IconThumbDown, ViewerIconButton } from './ViewerControls';

export function MusicRatingButtons({
  id,
  engine,
  size = 'sm',
  tooltip = 'up',
}: {
  id: string;
  engine?: Engine;
  size?: 'sm' | 'md';
  tooltip?: 'up' | 'down';
}) {
  if (!engine || !engine.music.can_rate || !id.includes(':media_library/audio/music/')) return null;
  const rating = engine.music.ratings[id] ?? 0;
  const rate = (value: number) => {
    void engine.rateMusic(id, rating === value ? 0 : value).catch(() => undefined);
  };
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5" onClick={e => e.stopPropagation()}>
      <ViewerIconButton
        size={size}
        tooltip={tooltip}
        active={rating === 1}
        disabled={!engine.music.can_rate}
        label={rating === 1 ? '取消点赞' : '点赞'}
        onClick={event => {
          event.stopPropagation();
          rate(1);
        }}
      >
        <IconHeart filled={rating === 1} />
      </ViewerIconButton>
      <ViewerIconButton
        size={size}
        tooltip={tooltip}
        danger={rating === -1}
        disabled={!engine.music.can_rate}
        label={rating === -1 ? '取消不喜欢' : '不喜欢'}
        onClick={event => {
          event.stopPropagation();
          rate(-1);
        }}
      >
        <IconThumbDown filled={rating === -1} />
      </ViewerIconButton>
    </span>
  );
}

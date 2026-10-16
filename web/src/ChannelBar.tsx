import { chip } from './ui';
import type { Group } from './feed';

// ChannelBar：频道条。推荐 | ♥ 我的点赞 | 额外筛选 | 📌 固定的分组 | 其余分组 | 回收站（管理员）。
export default function ChannelBar({ groups, pins, channel, onPick, filters = [], showHidden = false }: {
  groups: Group[]; pins: string[]; channel: string; onPick(channel: string): void;
  filters?: { channel: string; label: string }[]; showHidden?: boolean;
}) {
  const items = [{ channel: 'recommend', label: '推荐' }, { channel: 'liked', label: '♥ 我的点赞' }, ...filters];
  for (const id of pins) {
    const g = groups.find(x => x.id === id);
    if (g) items.push({ channel: `group:${g.id}`, label: `📌 ${g.label} · ${g.count}` });
  }
  for (const g of groups) if (!pins.includes(g.id)) items.push({ channel: `group:${g.id}`, label: `${g.label} · ${g.count}` });
  if (showHidden) items.push({ channel: 'hidden', label: '🗑 回收站' });
  return <div className="flex gap-1.5 overflow-x-auto pb-0.5">
    {items.map(i => <button key={i.channel} className={chip(channel === i.channel)} onClick={() => onPick(i.channel)}>{i.label}</button>)}
  </div>;
}

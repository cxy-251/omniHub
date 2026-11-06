import { useId, useMemo, useRef, useState } from 'react';
import { chip } from './ui';
import type { Group } from './feed';
import VirtualGrid from './VirtualGrid';
import './channels.css';

type Props = {
  groups: Group[]; pins: string[]; channel: string; onPick(channel: string): void;
  filters?: { channel: string; label: string }[]; showHidden?: boolean; loggedIn?: boolean;
  basicItems?: { channel: string; label: string }[];
  groupLabel?: string; onPin?(id: string, pinned: boolean): void;
};

/** Common channels stay visible; the searchable group panel mounts only on demand. */
export default function ChannelBar({ groups, pins, channel, onPick, filters = [], basicItems, showHidden = false, loggedIn = true, groupLabel = '分类', onPin }: Props) {
  const [open, setOpen] = useState(false);
  const panelId = useId(), toggle = useRef<HTMLButtonElement>(null);
  const byId = useMemo(() => new Map(groups.map(g => [g.id, g])), [groups]);
  const pinned = useMemo(() => new Set(pins), [pins]);
  const current = channel.startsWith('group:') ? byId.get(channel.slice(6)) : undefined;
  // Bound the collapsed row when there are many groups (e.g. hundreds of creators); show all inline when <= 24 (e.g. gallery members + colors).
  const inlineAll = groups.length <= 24;
  const common = inlineAll
    ? [...groups.filter(g => pinned.has(g.id)), ...groups.filter(g => !pinned.has(g.id))]
    : pins.flatMap(id => { const g = byId.get(id); return g ? [g] : []; }).slice(0, 6);
  if (!inlineAll && current && !common.some(g => g.id === current.id)) common.push(current);
  const defaultBasics = loggedIn ? [{ channel: 'recommend', label: '推荐' }, { channel: 'liked', label: '♥ 我的点赞' }] : [{ channel: 'recommend', label: '推荐' }];
  const basics = [...(basicItems ?? defaultBasics), ...filters];
  return <div className="channel-picker" onKeyDown={e => {
    if (e.key === 'Escape' && open) { e.stopPropagation(); setOpen(false); toggle.current?.focus(); }
  }}>
    <div className="channel-shortcuts">
      {basics.map(item => <button key={item.channel} className={chip(channel === item.channel)} aria-pressed={channel === item.channel} onClick={() => onPick(item.channel)}>{item.label}</button>)}
      {common.map(g => <button key={g.id} title={g.label} className={`${chip(channel === `group:${g.id}`)} channel-shortcut`} aria-pressed={channel === `group:${g.id}`} onClick={() => onPick(`group:${g.id}`)}>
        {pinned.has(g.id) ? <span aria-hidden="true">📌</span> : null}<span>{g.label}</span><small>{g.count}</small>
      </button>)}
      {!inlineAll && groups.length ? <button ref={toggle} className={`${chip(open)} channel-expand`} aria-expanded={open} aria-controls={open ? panelId : undefined} onClick={() => setOpen(v => !v)}>{open ? '收起' : `更多${groupLabel}（${groups.length}）`} <span aria-hidden="true">{open ? '▴' : '▾'}</span></button> : null}
      {showHidden ? <button className={chip(channel === 'hidden')} aria-pressed={channel === 'hidden'} onClick={() => onPick('hidden')}>🗑 回收站</button> : null}
    </div>
    {open ? <GroupPanel id={panelId} groups={groups} pinned={pinned} channel={channel} onPick={onPick} groupLabel={groupLabel} onPin={onPin} /> : null}
  </div>;
}

function GroupPanel({ id, groups, pinned, channel, onPick, groupLabel, onPin }: {
  id: string; groups: Group[]; pinned: Set<string>; channel: string; onPick(channel: string): void;
  groupLabel: string; onPin?: Props['onPin'];
}) {
  const [search, setSearch] = useState('');
  const query = search.trim().toLocaleLowerCase();
  const matches = useMemo(() => {
    const hits = groups.filter(g => !query || g.label.toLocaleLowerCase().includes(query));
    return [...hits.filter(g => pinned.has(g.id)), ...hits.filter(g => !pinned.has(g.id))];
  }, [groups, pinned, query]);
  return <section id={id} className="channel-panel" aria-label={`${groupLabel}筛选`}>
    <input type="search" autoFocus aria-label={`搜索${groupLabel}`} placeholder={`搜索${groupLabel}`} value={search} onChange={e => setSearch(e.target.value)} />
    {matches.length ? <VirtualGrid key={`${query}:${matches.length}`} total={matches.length} minColumns={1} minColumnWidth={220} aspect={0} extraHeight={48} gap={6} overscan={2} className="channel-results" render={index => {
      const g = matches[index], selected = channel === `group:${g.id}`;
      return <div className={`channel-option${selected ? ' is-selected' : ''}`}>
        <button className="channel-pick" title={g.label} aria-pressed={selected} onClick={() => onPick(`group:${g.id}`)}><span>{g.label}</span><small>{g.count}</small></button>
        {onPin ? <button className="channel-pin" aria-label={`${pinned.has(g.id) ? '取消固定' : '固定'} ${g.label}`} aria-pressed={pinned.has(g.id)} onClick={() => onPin(g.id, !pinned.has(g.id))}>📌</button> : null}
      </div>;
    }} /> : <p className="channel-empty">没有匹配的{groupLabel}</p>}
  </section>;
}

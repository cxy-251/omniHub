import { useCallback, useEffect, useState } from 'react';
import { api, post } from './api';

// 推荐与互动（与桌面端同一组接口 /api/feed/<domain>/…）。
// 频道：recommend 推荐 | liked 我的点赞 | hidden 回收站 | group:<分组> | tag:<标签>（如 tag:platform:抖音）。
// 行为上报：view 展示过；complete 看完；dwell 停留较久；skip 很快划走——服务端据此调整推荐。
export type Domain = 'shortvideo' | 'gallery';
export type Group = { id: string; label: string; count: number; platform?: string };

export const newSeed = () => Math.floor(Math.random() * 1e9);
export const feedPath = (domain: Domain, action: string) => `/api/feed/${domain}/${action}`;

export function feedQuery(params: Record<string, string | number | boolean | undefined>) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '' && v !== false) q.set(k, v === true ? '1' : String(v));
  return q.toString();
}

export const like = (domain: Domain, id: string, liked: boolean) => post(feedPath(domain, 'like'), { id, liked });
export const trash = (domain: Domain, id: string) => post(feedPath(domain, 'trash'), { id });
export const restore = (domain: Domain, id: string) => post(feedPath(domain, 'restore'), { id });
export function sendEvent(domain: Domain, id: string | undefined, type: 'view' | 'complete' | 'dwell' | 'skip') {
  if (id) post(feedPath(domain, 'event'), { id, type }).catch(() => undefined);
}

// useGroups：频道条用的分组（短视频是博主、相册是色系）与固定的分组。
export function useGroups(domain: Domain, active: boolean) {
  const [groups, setGroups] = useState<Group[]>([]);
  const [pins, setPins] = useState<string[]>([]);
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<{ groups: Group[]; pins: string[] }>(feedPath(domain, 'groups'), { signal: controller.signal })
      .then(r => { setGroups(r.groups); setPins(r.pins ?? []); }).catch(() => undefined);
    return () => controller.abort();
  }, [domain, active]);
  const setPinned = useCallback((group: string, pinned: boolean) => {
    post<string[]>(feedPath(domain, 'pin'), { group, pinned }).then(setPins).catch(() => undefined);
  }, [domain]);
  return { groups, pins, setPinned };
}

// useLiked：点赞后先在本地改状态（列表按页缓存，不必重取）。
export function useLiked(domain: Domain) {
  const [override, setOverride] = useState<Record<string, boolean>>({});
  const isLiked = useCallback((item?: { id: string; liked?: boolean }) => !!item && (item.id in override ? override[item.id] : !!item.liked), [override]);
  const toggle = useCallback((item: { id: string; liked?: boolean }) => {
    const next = !(item.id in override ? override[item.id] : !!item.liked);
    like(domain, item.id, next).then(() => setOverride(o => ({ ...o, [item.id]: next }))).catch(() => undefined);
  }, [domain, override]);
  return { isLiked, toggle };
}

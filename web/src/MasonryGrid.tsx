import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';

// MasonryGrid：等宽瀑布流的虚拟滚动。ratios 为每项高宽比 ×1000（全部项，与数据同序）。
// 每项放进当前最矮的一列，因此纵坐标随序号单调不减，二分查找可视范围，只渲染上下各一屏内的项。
export default function MasonryGrid({ ratios, minColumnWidth, render }: {
  ratios: number[]; minColumnWidth: number; render: (index: number) => ReactNode;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [height, setHeight] = useState(0);
  const [scrollTop, setScrollTop] = useState(0);
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const observer = new ResizeObserver(() => { setWidth(el.clientWidth); setHeight(el.clientHeight); });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  useEffect(() => { scroller.current?.scrollTo(0, 0); }, [ratios]);

  const gap = 4;
  const columns = Math.max(2, Math.floor((width + gap) / (minColumnWidth + gap)));
  const colWidth = width > 0 ? (width - gap * (columns - 1)) / columns : 0;
  const layout = useMemo(() => {
    const n = ratios.length, ys = new Float64Array(n), hs = new Float64Array(n), cols = new Uint16Array(n);
    const heights = new Array<number>(columns).fill(0);
    let tallest = 0;
    for (let i = 0; i < n; i++) {
      let best = 0;
      for (let k = 1; k < columns; k++) if (heights[k] < heights[best]) best = k;
      const h = colWidth * Math.min(2.5, Math.max(0.4, ratios[i] / 1000));
      ys[i] = heights[best]; hs[i] = h; cols[i] = best;
      heights[best] += h + gap;
      tallest = Math.max(tallest, h);
    }
    return { ys, hs, cols, tallest, total: Math.max(0, ...heights) };
  }, [ratios, columns, colWidth]);

  const find = (y: number) => {
    let lo = 0, hi = layout.ys.length;
    while (lo < hi) { const mid = (lo + hi) >> 1; if (layout.ys[mid] < y) lo = mid + 1; else hi = mid; }
    return lo;
  };
  const first = find(scrollTop - Math.max(height, layout.tallest));
  const last = find(scrollTop + 2 * height);
  const items: ReactNode[] = [];
  for (let i = first; i < last; i++) {
    items.push(<div key={i} style={{ position: 'absolute', top: layout.ys[i], left: layout.cols[i] * (colWidth + gap), width: colWidth, height: layout.hs[i] }}>{render(i)}</div>);
  }
  return <div ref={scroller} className="relative h-[calc(100vh-11rem)] min-h-80 overflow-y-auto" onScroll={e => setScrollTop(e.currentTarget.scrollTop)}>
    <div style={{ position: 'relative', height: layout.total }}>{items}</div>
  </div>;
}

import { useEffect, useRef, useState, type ReactNode } from 'react';

// VirtualGrid：固定比例格子的虚拟滚动网格。只渲染可视区上下各 overscan 行，
// 滚远的格子（及其 <img>）从 DOM 移除，浏览器随之释放解码后的图片。
// 滚动容器自身占满剩余高度；extraHeight 为格子下方文字等固定高度。
export default function VirtualGrid({ total, minColumnWidth, aspect, extraHeight = 0, overscan = 3, render }: {
  total: number; minColumnWidth: number; aspect: number; extraHeight?: number; overscan?: number;
  render: (index: number) => ReactNode;
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

  const gap = 4;
  const columns = Math.max(2, Math.floor((width + gap) / (minColumnWidth + gap)));
  const cell = width > 0 ? (width - gap * (columns - 1)) / columns : 0;
  const rowHeight = cell * aspect + extraHeight + gap;
  const rows = Math.ceil(total / columns);
  const first = rowHeight > 0 ? Math.max(0, Math.floor(scrollTop / rowHeight) - overscan) : 0;
  const last = rowHeight > 0 ? Math.min(rows, Math.ceil((scrollTop + height) / rowHeight) + overscan) : 0;

  const cells: ReactNode[] = [];
  for (let i = first * columns; i < Math.min(total, last * columns); i++) {
    const row = Math.floor(i / columns), col = i % columns;
    cells.push(<div key={i} style={{ position: 'absolute', top: row * rowHeight, left: col * (cell + gap), width: cell, height: rowHeight - gap }}>{render(i)}</div>);
  }
  return <div ref={scroller} className="relative h-[calc(100vh-11rem)] min-h-80 overflow-y-auto" onScroll={e => setScrollTop(e.currentTarget.scrollTop)}>
    <div style={{ position: 'relative', height: rows * rowHeight }}>{cells}</div>
  </div>;
}

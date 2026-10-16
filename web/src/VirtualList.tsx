import { useEffect, useRef, useState, type ReactNode } from 'react';

// VirtualList：固定行高的虚拟滚动列表，只渲染可视区上下各 overscan 行（缺页的数据此时才去取）。
export default function VirtualList({ total, rowHeight, overscan = 8, className = '', render }: {
  total: number; rowHeight: number; overscan?: number; className?: string; render: (index: number) => ReactNode;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(0);
  const [scrollTop, setScrollTop] = useState(0);
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setHeight(el.clientHeight));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  const first = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan);
  const last = Math.min(total, Math.ceil((scrollTop + height) / rowHeight) + overscan);
  const rows: ReactNode[] = [];
  for (let i = first; i < last; i++) rows.push(<div key={i} style={{ position: 'absolute', top: i * rowHeight, left: 0, right: 0, height: rowHeight }}>{render(i)}</div>);
  return <div ref={scroller} className={`relative overflow-y-auto ${className}`} onScroll={e => setScrollTop(e.currentTarget.scrollTop)}>
    <div style={{ position: 'relative', height: total * rowHeight }}>{rows}</div>
  </div>;
}

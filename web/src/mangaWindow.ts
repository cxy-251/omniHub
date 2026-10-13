export type PageAnchor = { row: number; ratio: number };
const estimate = 320;
const maxMeasured = 64;
const maxHeight = 65536;

// MangaWindow represents every page without every DOM node. A bounded set of
// measured heights and a Fenwick tree keep scroll lookup/updates logarithmic.
export class MangaWindow {
  readonly count: number;
  private readonly tree: Float64Array;
  private readonly measured = new Map<number, number>();

  constructor(count: number) {
    if (!Number.isInteger(count) || count < 1 || count > 20000) throw new RangeError('漫画页数超出范围');
    this.count = count; this.tree = new Float64Array(count + 1);
  }

  private add(row: number, delta: number) {
    for (let index = row + 1; index <= this.count; index += index & -index) this.tree[index] += delta;
  }

  height(row: number) { return this.measured.get(row) ?? estimate; }

  prefix(rows: number) {
    rows = Number.isFinite(rows) ? Math.floor(rows) : 0;
    let total = Math.max(0, Math.min(this.count, rows)) * estimate;
    for (let index = Math.max(0, Math.min(this.count, rows)); index > 0; index -= index & -index) total += this.tree[index];
    return total;
  }

  total() { return this.prefix(this.count); }

  locate(offset: number): PageAnchor {
    const y = Math.max(0, Math.min(this.total(), Number.isFinite(offset) ? offset : 0));
    let low = 0, high = this.count;
    while (low + 1 < high) { const mid = Math.floor((low + high) / 2); if (this.prefix(mid) <= y) low = mid; else high = mid; }
    return { row: low, ratio: Math.max(0, Math.min(1, (y - this.prefix(low)) / this.height(low))) };
  }

  offset(anchor: PageAnchor) { return this.prefix(anchor.row) + Math.max(0, Math.min(1, anchor.ratio)) * this.height(anchor.row); }

  measure(row: number, height: number, protectedRows: ReadonlySet<number>) {
    if (!Number.isInteger(row) || row < 0 || row >= this.count || !Number.isFinite(height) || protectedRows.size > 12) return false;
    const next = Math.max(80, Math.min(maxHeight, height));
    const old = this.height(row);
    this.measured.delete(row); this.measured.set(row, next);
    this.add(row, next - old);
    while (this.measured.size > maxMeasured) {
      const victim = [...this.measured.keys()].find(index => index !== row && !protectedRows.has(index));
      if (victim === undefined) break;
      const previous = this.measured.get(victim)!;
      this.measured.delete(victim); this.add(victim, estimate - previous);
    }
    return old !== next;
  }

  window(row: number) {
    const start = Math.max(0, Math.min(this.count - 1, row) - 1);
    return { start, end: Math.min(this.count, start + 12) };
  }
}

import { useEffect, useState } from 'react';
import { api, request } from './api';
import { renderMarkdown } from './markdown';

type Document = { name: string; rel_path: string; library_name: string };
type Chunk = { lines: string[]; start_line: number; start_offset: number; total_lines: number; next_offset: number; has_more: boolean };
type Epub = { title: string; author: string; asset_base: string; chapters: { title: string; entry: string }[] };
type Cursor = { line: number; offset: number };
type Position = { start_line: number; byte_offset: number; chapter_index: number };
type PositionResponse = { position: Position | null; can_save: boolean; file_version?: string };
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

function DocumentReader({ document: item, active }: { document: Document; active: boolean }) {
  const epub = item.rel_path.toLowerCase().endsWith('.epub');
  const [book, setBook] = useState<Epub>();
  const [chapter, setChapter] = useState(0);
  const [cursor, setCursor] = useState<Cursor>({ line: 1, offset: 0 });
  const [history, setHistory] = useState<Cursor[]>([]);
  const [chunk, setChunk] = useState<Chunk>();
  const [html, setHTML] = useState<string>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [positionReady, setPositionReady] = useState(false);
  const [canSave, setCanSave] = useState(false);
  const [fileVersion, setFileVersion] = useState('');
  const [positionError, setPositionError] = useState('');

  useEffect(() => {
    if (!active || positionReady) return;
    const controller = new AbortController();
    api<PositionResponse>(`/api/media/text/progress?document_id=${encodeURIComponent(item.rel_path)}`, { signal: controller.signal })
      .then(data => {
        if (controller.signal.aborted) return;
        setCanSave(data.can_save); setFileVersion(data.file_version ?? '');
        if (data.position) {
          setCursor({ line: data.position.start_line, offset: data.position.byte_offset });
          setChapter(data.position.chapter_index);
        }
        setPositionReady(true);
      }).catch((e: Error) => { if (!controller.signal.aborted) { setPositionError(`位置恢复失败：${e.message}`); setPositionReady(true); } });
    return () => controller.abort();
  }, [active, item.rel_path, positionReady]);

  // EPUB metadata is independent of saved position, so load both concurrently.
  useEffect(() => {
    if (!active || !epub) return;
    const controller = new AbortController();
    setLoading(true); setError('');
    api<Epub>(`/api/media/text/epub/book?path=${encodeURIComponent(item.rel_path)}`, { signal: controller.signal })
      .then(next => { if (!controller.signal.aborted) setBook(next); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [active, item.rel_path, epub]);

  useEffect(() => {
    if (!active || epub || !positionReady) return;
    const controller = new AbortController();
    setLoading(true); setError('');
      const offset = cursor.offset >= 0 ? `&offset=${cursor.offset}` : '';
      api<Chunk>(`/api/media/text/chunk?path=${encodeURIComponent(item.rel_path)}&start=${cursor.line}&limit=100${offset}`, { signal: controller.signal })
        .then(async next => {
          if (controller.signal.aborted) return;
          const markup = item.rel_path.toLowerCase().endsWith('.md') ? await renderMarkdown(next.lines.join('\n')) : undefined;
          if (!controller.signal.aborted) { setChunk(next); setHTML(markup); }
        }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); })
        .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [active, item.rel_path, cursor, epub, positionReady]);

  const selectedChapter = book ? Math.min(chapter, book.chapters.length - 1) : chapter;
  useEffect(() => {
    if (!active || !positionReady || !canSave || loading || error || (epub ? !book : !chunk)) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      request('/api/media/text/progress', { method: 'POST', signal: controller.signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
        document_id: item.rel_path, start_line: epub ? 1 : chunk!.start_line,
        byte_offset: epub ? 0 : chunk!.start_offset, chapter_index: epub ? selectedChapter : 0, file_version: fileVersion,
      }) }).then(() => { if (!controller.signal.aborted) setPositionError(''); })
        .catch((e: Error) => { if (!controller.signal.aborted) setPositionError(`位置保存失败：${e.message}`); });
    }, 500);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [active, positionReady, canSave, loading, error, epub, book, chunk, item.rel_path, selectedChapter, fileVersion]);

  function previousPage() {
    const previous = history.at(-1) ?? { line: Math.max(1, cursor.line - 100), offset: -1 };
    setHistory(value => value.slice(0, -1)); setCursor(previous);
  }

  function nextPage() {
    if (!chunk?.has_more) return;
    setHistory(value => [...value.slice(-255), { line: chunk.start_line, offset: chunk.start_offset }]);
    setCursor({ line: chunk.start_line + chunk.lines.length, offset: chunk.next_offset });
  }

  const groupStart = Math.floor(selectedChapter / 100) * 100;
  return <section className="min-w-0 space-y-3">
    <h3 className="break-all text-lg font-semibold">{book?.title ?? item.name}</h3>
    {book?.author ? <p className="text-sm text-muted">{book.author}</p> : null}
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {positionError ? <p role="alert" className="whitespace-pre-wrap text-danger">{positionError}</p> : null}
    {loading || !positionReady ? <p role="status">正在读取…</p> : null}
    {epub && book ? <>
      <div className="flex flex-wrap items-center gap-3">
        <button className={button} disabled={!positionReady || selectedChapter === 0} onClick={() => setChapter(selectedChapter - 1)}>上一章</button>
        <select aria-label="章节" disabled={!positionReady} className="min-w-0 max-w-full rounded border border-line bg-surface p-2 text-sm" value={selectedChapter} onChange={event => setChapter(Number(event.target.value))}>
          {book.chapters.slice(groupStart, groupStart + 100).map((entry, index) => <option key={index + groupStart} value={index + groupStart}>{index + groupStart + 1}. {entry.title}</option>)}
        </select>
        <button className={button} disabled={!positionReady || selectedChapter + 1 >= book.chapters.length} onClick={() => setChapter(selectedChapter + 1)}>下一章</button>
        <label className="flex items-center gap-2 text-sm">跳至<input key={selectedChapter} disabled={!positionReady} className="w-20 rounded border border-line bg-surface p-2" type="number" min={1} max={book.chapters.length} defaultValue={selectedChapter + 1} onBlur={event => { const value = Number(event.target.value); if (Number.isInteger(value) && value >= 1 && value <= book.chapters.length) setChapter(value - 1); }} />/ {book.chapters.length} 章</label>
      </div>
      {active && positionReady ? <iframe className="h-[70vh] w-full rounded-lg border border-line bg-white" title={`${book.title} 第 ${selectedChapter + 1} 章`} sandbox="allow-same-origin" src={book.asset_base + book.chapters[selectedChapter].entry.split('/').map(encodeURIComponent).join('/')} /> : null}
    </> : null}
    {!epub && chunk ? <>
      <div className="flex flex-wrap items-center gap-3"><button className={button} disabled={loading || cursor.line === 1} onClick={previousPage}>上一页</button><span className="text-sm text-muted">{chunk.lines.length ? `第 ${chunk.start_line}–${chunk.start_line + chunk.lines.length - 1} 行` : '文件末尾'}</span><button className={button} disabled={loading || !chunk.has_more} onClick={nextPage}>下一页</button></div>
      {html !== undefined ? <article className="reader-container" dangerouslySetInnerHTML={{ __html: html }} /> : <pre className="whitespace-pre-wrap break-words rounded-lg border border-line bg-surface p-4 font-sans leading-relaxed">{chunk.lines.join('\n')}</pre>}
    </> : null}
  </section>;
}

// TextLibrary retains selection while hidden and aborts file/position requests.
export default function TextLibrary({ active }: { active: boolean }) {
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selected, setSelected] = useState<Document>();
  const [offset, setOffset] = useState(0);
  const [more, setMore] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [revision, setRevision] = useState(0);
  const [selectionRevision, setSelectionRevision] = useState(0);

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    setLoading(true); setError('');
    request(`/api/media/text/list?limit=100&offset=${offset}`, { signal: controller.signal })
      .then(async response => {
        const next = await response.json() as Document[];
        if (!controller.signal.aborted) { setDocuments(next); setMore(response.headers.get('X-Has-More') === 'true'); }
      }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [active, offset, revision]);

  return <div className="grid gap-5 lg:grid-cols-[minmax(12rem,1fr)_minmax(0,3fr)]">
    <aside className="space-y-3"><button className={button} disabled={loading} onClick={() => setRevision(value => value + 1)}>刷新文档</button>
      {error ? <p role="alert" className="text-danger">{error}</p> : null}
      {loading ? <p role="status">正在加载列表…</p> : !documents.length ? <p>当前页暂无文档</p> : null}
      {documents.map(item => <button key={item.rel_path} className={`${button} block w-full break-all text-left`} onClick={() => { setSelected(item); setSelectionRevision(value => value + 1); }}>{item.name}<span className="block text-xs text-muted">{item.library_name}</span></button>)}
      <div className="flex gap-3"><button className={button} disabled={loading || offset === 0} onClick={() => setOffset(value => Math.max(0, value - 100))}>上一页</button><button className={button} disabled={loading || !more} onClick={() => setOffset(value => value + 100)}>下一页</button></div>
    </aside>
    {selected ? <DocumentReader key={`${selected.rel_path}:${selectionRevision}`} document={selected} active={active} /> : <p className="text-muted">选择文档开始阅读</p>}
  </div>;
}

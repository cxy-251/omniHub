import { useEffect, useRef, useState } from 'react';
import { api } from './api';

type Definition = { id: string; name: string; category: string; write: boolean; parameters: { name: string; label: string; required: boolean }[] };
type Task = { id: string; name: string; running: boolean; exit_code: number };
type Entry = { id: number; timestamp: string; delta_ms: number; level: string; message: string; is_stack: boolean };
type Library = { id: string; name: string; read_only: boolean; is_online: boolean };
type Catalog = { definitions: Definition[]; tasks: Task[] };
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';
const input = 'rounded-md border border-line bg-background px-3 py-2 text-sm';

// Retained history is bounded; regex cannot block the React/DOM thread.
function useLogFilter(entries: Entry[], pattern: string) {
  const [result, setResult] = useState<{ ids?: Set<number>; error?: string }>({});
  const worker = useRef<Worker | null>(null), blocked = useRef(''), workerPattern = useRef('');
  useEffect(() => () => { worker.current?.terminate(); worker.current = null; }, []);
  useEffect(() => {
    if (workerPattern.current !== pattern) { worker.current?.terminate(); worker.current = null; workerPattern.current = pattern; blocked.current = ''; }
    if (!pattern) { setResult({}); return; }
    if (blocked.current === pattern) return;
    const current = worker.current ?? new Worker(new URL('./taskFilter.worker.ts', import.meta.url), { type: 'module' });
    worker.current = current;
    const timeout = window.setTimeout(() => {
      current.terminate(); worker.current = null; blocked.current = pattern;
      setResult({ error: '正则执行超时，请简化表达式' });
    }, 150);
    current.onmessage = (event: MessageEvent<{ ids?: number[]; error?: string }>) => {
      clearTimeout(timeout); setResult({ ids: new Set(event.data.ids ?? []), error: event.data.error });
    };
    current.onerror = () => { clearTimeout(timeout); current.terminate(); worker.current = null; blocked.current = pattern; setResult({ error: '日志过滤器不可用' }); };
    current.postMessage({ entries: entries.map(({ id, message }) => ({ id, message })), pattern });
    return () => { clearTimeout(timeout); current.onmessage = null; current.onerror = null; };
  }, [entries, pattern]);
  return result;
}

export default function TaskConsole() {
  const [catalog, setCatalog] = useState<Catalog>({ definitions: [], tasks: [] });
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [definitionID, setDefinitionID] = useState(''), [libraryID, setLibraryID] = useState('');
  const [directory, setDirectory] = useState(''), [parameters, setParameters] = useState<Record<string, string>>({});
  const [taskID, setTaskID] = useState(''), [logs, setLogs] = useState<Entry[]>([]);
  const [paused, setPaused] = useState(false), [level, setLevel] = useState('ALL'), [pattern, setPattern] = useState('');
  const viewport = useRef<HTMLDivElement>(null);
  const [follow, setFollow] = useState(true);
  const [expanded, setExpanded] = useState<number>(), [error, setError] = useState(''), [pending, setPending] = useState(false);
  const [connectionRevision, setConnectionRevision] = useState(0);
  const [revision, setRevision] = useState(0), [streamClosed, setStreamClosed] = useState(false);
  const retained = useRef<Entry[]>([]), dirty = useRef(false), pausedRef = useRef(false);
  const lifetime = useRef<AbortController | null>(null);
  const filter = useLogFilter(logs, pattern);
  const definition = catalog.definitions.find(item => item.id === definitionID);
  const task = catalog.tasks.find(item => item.id === taskID);
  const available = libraries.filter(item => item.is_online && (!definition?.write || !item.read_only));

  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    return () => { controller.abort(); lifetime.current = null; };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([api<Catalog>('/api/tasks/catalog', { signal: controller.signal }), api<{ libraries: Library[] }>('/api/library/list', { signal: controller.signal })])
      .then(([next, storage]) => { setCatalog(next); setLibraries(storage.libraries); setDefinitionID(old => old || next.definitions[0]?.id || ''); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => {
    pausedRef.current = paused;
    if (!paused) { setLogs([...retained.current]); dirty.current = false; }
  }, [paused]);
  useEffect(() => {
    if (!taskID) return;
    retained.current = []; setLogs([]); setExpanded(undefined); setStreamClosed(false);
    const source = new EventSource(`/api/tasks/logs?id=${encodeURIComponent(taskID)}`);
    let timer: number | undefined;
    const flush = () => {
      timer = undefined;
      if (dirty.current && !pausedRef.current) { setLogs([...retained.current]); dirty.current = false; }
    };
    source.onmessage = event => {
      try {
        const entry = JSON.parse(event.data) as Entry;
        if (!Number.isSafeInteger(entry.id) || entry.id <= 0 || typeof entry.message !== 'string' || entry.message.length > 4200) throw new Error('invalid log');
        const previous = retained.current.at(-1)?.id ?? 0;
        if (entry.id <= previous) return;
        retained.current.push(entry); if (retained.current.length > 512) retained.current.shift(); dirty.current = true;
        if (timer === undefined && !pausedRef.current) timer = window.setTimeout(flush, 250);
      } catch { setError('任务日志格式无效'); source.close(); setStreamClosed(true); }
    };
    const finish = () => { source.close(); clearTimeout(timer); flush(); setStreamClosed(true); setRevision(value => value + 1); };
    source.addEventListener('end', finish);
    source.onerror = () => { finish(); setError('日志连接中断，任务状态以列表为准；可重新连接'); };
    return () => { source.close(); clearTimeout(timer); retained.current = []; };
  }, [taskID, connectionRevision]);

  useEffect(() => { if (follow && viewport.current) viewport.current.scrollTop = viewport.current.scrollHeight; }, [logs, follow]);

  async function run() {
    if (!definition || !available.some(item => item.id === libraryID) || pending) return;
    const signal = lifetime.current?.signal;
    if (!signal) return;
    setPending(true); setError('');
    try {
      const next = await api<Task>('/api/tasks/run', { method: 'POST', signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ definition: definition.id, library_id: libraryID, directory, parameters }) });
      setCatalog(old => ({ ...old, tasks: [next, ...old.tasks.filter(item => item.id !== next.id)].slice(0, 16) }));
      setTaskID(next.id);
    } catch (e) { if (!signal.aborted) setError((e as Error).message); }
    finally { if (!signal.aborted) setPending(false); }
  }
  async function stop() {
    const signal = lifetime.current?.signal; if (!signal || !task?.running || pending) return;
    setPending(true); setError('');
    try { await api('/api/tasks/stop', { method: 'POST', signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: task.id }) }); }
    catch (e) { if (!signal.aborted) setError((e as Error).message); }
    finally { if (!signal.aborted) setPending(false); }
  }
  const visible = logs.filter(entry => (level === 'ALL' || level === entry.level) && (!pattern || (!filter.error && filter.ids?.has(entry.id)))).slice(-128);
  return <div className="space-y-4">
    <div className="flex flex-wrap items-end gap-3 rounded-lg border border-line bg-panel p-4">
      <label className="flex flex-col gap-2">任务<select className={input} value={definitionID} onChange={e => { setDefinitionID(e.target.value); setParameters({}); setLibraryID(''); }}>{catalog.definitions.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <label className="flex flex-col gap-2">资源库<select className={input} value={libraryID} onChange={e => setLibraryID(e.target.value)}><option value="">选择资源库</option>{available.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <label className="flex flex-col gap-2">{definition?.category ?? '分类'} 下的目录<input className={input} maxLength={4096} value={directory} placeholder="留空使用分类根目录" onChange={e => setDirectory(e.target.value)} /></label>
      {definition?.parameters.map(parameter => <label className="flex flex-col gap-2" key={parameter.name}>{parameter.label}<input className={input} required={parameter.required} maxLength={1024} value={parameters[parameter.name] ?? ''} onChange={e => setParameters(old => ({ ...old, [parameter.name]: e.target.value }))} /></label>)}
      <button className={button} disabled={pending || !definition || !available.some(item => item.id === libraryID) || definition.parameters.some(p => p.required && !parameters[p.name]?.trim())} onClick={run}>启动任务</button>
      <button className={button} disabled={pending || !task?.running} onClick={stop}>终止所选任务</button>
      <button className={button} onClick={() => { setError(''); setRevision(value => value + 1); }}>刷新状态</button>
    </div>
    {!catalog.definitions.length ? <p className="text-muted">尚未注册脚本，请在服务端配置目录的 tasks.json 注册任务后重启服务。</p> : null}
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    <div className="flex flex-wrap items-center gap-3">
      <label>任务记录 <select className={input} value={taskID} onChange={e => setTaskID(e.target.value)}><option value="">选择任务查看日志</option>{catalog.tasks.map(item => <option key={item.id} value={item.id}>{item.name} · {item.running ? '运行中' : `退出 ${item.exit_code}`}</option>)}</select></label>
      {streamClosed && task?.running ? <button className={button} onClick={() => setConnectionRevision(value => value + 1)}>重新连接日志</button> : null}
      <button className={button} onClick={() => setPaused(value => !value)}>{paused ? '恢复日志' : '暂停日志'}</button>
      <button className={button} onClick={() => { retained.current = []; setLogs([]); setExpanded(undefined); dirty.current = false; }}>清空视口</button>
      <label><input type="checkbox" checked={follow} onChange={e => setFollow(e.target.checked)} /> 跟随末尾</label>
      <select aria-label="日志级别" className={input} value={level} onChange={e => setLevel(e.target.value)}>{['ALL', 'ERROR', 'WARN', 'INFO', 'DEBUG'].map(value => <option key={value}>{value}</option>)}</select>
      <input aria-label="日志正则" className={input} maxLength={128} value={pattern} onChange={e => setPattern(e.target.value)} placeholder="正则过滤（忽略大小写）" />
    </div>
    {filter.error ? <p role="alert" className="text-danger">{filter.error}</p> : null}
    <p className="text-sm text-muted">保留最近 512 行，显示最近 128 条匹配项；暂停时继续保留有界日志。点击日志展开前后五行上下文。</p>
    <div ref={viewport} className="max-h-[65vh] overflow-auto rounded-lg border border-line bg-background p-3 font-mono text-xs">
      {visible.map(entry => <div key={entry.id} className="border-b border-line py-2">
        <button className="w-full text-left" onClick={() => setExpanded(old => old === entry.id ? undefined : entry.id)}><span className="text-muted">{new Date(entry.timestamp).toLocaleTimeString()} +{entry.delta_ms}ms </span><span className={entry.level === 'ERROR' ? 'text-danger' : 'text-primary'}>[{entry.level}] </span>{entry.message.slice(0, entry.is_stack || entry.message.length > 500 ? 140 : 500)}</button>
        {entry.is_stack || entry.message.length > 500 ? <details><summary>完整行</summary><pre className="whitespace-pre-wrap break-all">{entry.message}</pre></details> : null}
        {expanded === entry.id ? <pre className="mt-2 whitespace-pre-wrap break-all text-muted">{logs.slice(Math.max(0, logs.indexOf(entry) - 5), logs.indexOf(entry) + 6).map(item => `${item.id === entry.id ? '›' : ' '} [${item.level}] ${item.message}`).join('\n')}</pre> : null}
      </div>)}
    </div>
  </div>;
}

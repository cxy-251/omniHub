import { useEffect, useRef, useState } from 'react';
import { api } from './api';

/** Session policy is fetched independently from accounts and permissions. */
export default function SessionSettings() {
  const [days, setDays] = useState(''), [saved, setSaved] = useState('');
  const [error, setError] = useState(''), [notice, setNotice] = useState(''), [pending, setPending] = useState(false);
  const [revision, setRevision] = useState(0);
  const lifetime = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    setError('');
    api<{ idle_days: number }>('/api/admin/sessions', { signal: controller.signal })
      .then(policy => { if (!controller.signal.aborted) { setDays(String(policy.idle_days)); setSaved(String(policy.idle_days)); } })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => { controller.abort(); lifetime.current = null; };
  }, [revision]);
  const valid = /^\d+$/.test(days) && Number(days) >= 1 && Number(days) <= 365;
  async function save() {
    const signal = lifetime.current?.signal;
    if (!valid || !signal || pending) return;
    setPending(true); setError(''); setNotice('');
    try {
      await api('/api/admin/sessions', { method: 'POST', signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ idle_days: Number(days) }) });
      if (!signal.aborted) { setSaved(days); setNotice('登录保持天数已保存，无需重启。'); }
    } catch (e) { if (!signal.aborted) setError((e as Error).message); }
    finally { if (!signal.aborted) setPending(false); }
  }
  return <>
    <section className="session-panel">
      <div className="session-main">
        <form className="session-form" data-dirty={days !== saved} onSubmit={e => { e.preventDefault(); void save(); }}>
          <label htmlFor="idle-days">连续未使用</label>
          <input id="idle-days" aria-label="登录保持天数" aria-describedby="session-help" type="number" min={1} max={365} step={1} required disabled={!saved || pending} value={days} onChange={e => { setDays(e.target.value); setNotice(''); }} />
          <span>天后退出</span><button className="workbench-button primary" disabled={!saved || pending || !valid}>{pending ? '保存中…' : '保存'}</button>
        </form>
        {!saved && !error ? <p role="status" className="settings-description">正在读取登录设置…</p> : null}
        {error ? <div role="alert" className="settings-error"><p>{error}</p><button className="workbench-button" onClick={() => setRevision(v => v + 1)}>重新读取</button></div> : null}
        {notice ? <p role="status" className="settings-notice">{notice}</p> : null}
      </div>
      <p id="session-help" className="session-help">可设 1–365 天，正常访问自动续期；已过期的登录需重新登录。</p>
    </section>
  </>;
}

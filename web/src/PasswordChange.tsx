import { useState, type FormEvent } from 'react';
import { post } from './api';

// PasswordChange keeps secrets in this mounted form only; changing the password
// revokes all account sessions and remembered devices, including this browser.
export default function PasswordChange({ onCancel }: { onCancel: () => void }) {
  const [current, setCurrent] = useState(''), [password, setPassword] = useState('');
  const [pending, setPending] = useState(false), [error, setError] = useState('');
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending) return;
    setPending(true); setError('');
    try {
      await post('/api/auth/password', { current_password: current, new_password: password });
      window.location.reload();
    } catch (e) { setError((e as Error).message); }
    finally { setCurrent(''); setPassword(''); setPending(false); }
  }
  const input = 'rounded-md border border-line bg-background px-3 py-2';
  return <form className="mb-6 space-y-3 rounded-lg border border-line bg-panel p-4" onSubmit={submit}>
    <p>修改密码后，所有登录会话与信任设备都需要重新登录。</p>
    {error ? <p role="alert" className="text-danger">{error}</p> : null}
    <div className="flex flex-wrap items-end gap-3">
      <label className="flex flex-col gap-2">当前密码<input className={input} type="password" autoComplete="current-password" required maxLength={72} value={current} onChange={e => setCurrent(e.target.value)} /></label>
      <label className="flex flex-col gap-2">新密码<input className={input} type="password" autoComplete="new-password" required maxLength={72} value={password} onChange={e => setPassword(e.target.value)} /></label>
      <button className={input} disabled={pending}>{pending ? '修改中…' : '修改并重新登录'}</button>
      <button className={input} type="button" disabled={pending} onClick={onCancel}>取消</button>
    </div>
  </form>;
}

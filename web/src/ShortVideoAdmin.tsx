import { useCallback, useState } from 'react';
import { api, post } from './api';
import { useVisiblePolling } from './useVisiblePolling';
import DirectoryPicker from './DirectoryPicker';
import { button, primaryButton } from './ui';
import PhoneThumbnails, { type PhoneReview } from './PhoneThumbnails';

type Stats = { videos: number; videos_probed: number; albums: number; thumbnails: number; thumbnail_failures: number; albums_imported: number; albums_on_disk: number; albums_cleanable: number };
type Worker = { thumbnails_enabled: boolean; job: string; done: number; total: number; last_job: string; last_error: string; last_note: string; detail?: string; logs?: {time: string; message: string}[]; phone_review?: PhoneReview };
type Library = { library_id: string; name: string; stats: Stats; error?: string; copy_source?: boolean };
type Status = { worker: Worker; libraries: Library[] | null; copy_targets: { library_id: string; name: string }[] | null; phone_maintenance?: boolean };
type Plan = { files: number; bytes: number; creators: string[] | number | null; conflicts: string[] | null; phone_free?: number; not_on_phone?: number; not_on_keep?: number };
// review：先读出计划给人看，确认后才执行；start 为空表示没有要做的
type Review = { text: string; warn: string; start: string; action: string; body: unknown };

const jobNames: Record<string, string> = { queued: '等待任务', 'phone-thumbs-history': '读取手机回收记录', 'phone-thumbs-plan': '核对手机旧封面', 'phone-thumbs-clean': '回收手机旧封面', 'phone-thumbs-restore': '恢复手机旧封面', scan: '扫描', import: '图集入库', clean: '清理已入库原目录', thumbnails: '生成缩略图', merge: '合并其他设备的数据', copy: '补充到其他资源库', phone: '同步到手机', 'clean-phone': '清理已同步到手机的视频' };
const gb = (bytes: number) => `${(bytes / 1e9).toFixed(1)} GB`;
const creatorsText = (list: string[]) => list.length ? `\n${list.slice(0, 8).join('、')}${list.length > 8 ? ' 等' : ''}` : '';
const conflictsText = (list: string[]) => list.length ? `${list.length} 个同名文件大小不同，不会覆盖：${list.slice(0, 5).join('、')}` : '';

// ShortVideoAdmin 管理短视频索引：缩略图开关、扫描、图集入库与校验后清理原目录、合并其他设备的数据，
// 以及从下载落地的来源库补充到 SD 卡、同步到手机（与桌面端同一组接口）。有任务时每 2 秒刷新进度。
export default function ShortVideoAdmin() {
  const [status, setStatus] = useState<Status>();
  const [error, setError] = useState('');
  const [confirmClean, setConfirmClean] = useState(false);
  const [review, setReview] = useState<Review>();
  const [loading, setLoading] = useState('');
  const [merging, setMerging] = useState(false), [mergeTarget, setMergeTarget] = useState('');

  const load = useCallback((signal?: AbortSignal) => api<Status>('/api/media/shortvideo/admin/status', { signal })
    .then(next => { setStatus(next); setError(''); })
    .catch((e: Error) => { if (!signal?.aborted) setError(e.message); }), []);

  const libraries = status?.libraries ?? [];
  // 来源库（下载落地的中转库）不生成缩略图、不入库图集，统计里不算它
  const source = libraries.find(l => l.copy_source);
  const processed = libraries.filter(l => !l.copy_source);
  const w = status?.worker;
  const busy = !!w?.job;
  const thumbsRunning = !!w?.thumbnails_enabled && processed.some(l => !l.error && l.stats.thumbnails + l.stats.thumbnail_failures < l.stats.videos + l.stats.albums);
  useVisiblePolling(load, busy || thumbsRunning ? 2000 : null);

  async function act(action: string, body: unknown = {}) {
    try { await post(`/api/media/shortvideo/admin/${action}`, body); } catch (e) { setError((e as Error).message); }
    void load();
  }
  async function plan(action: string, body: unknown, message: string, build: (p: Plan) => Review) {
    setReview(undefined); setLoading(message);
    try { setReview(build(await post<Plan>(`/api/media/shortvideo/admin/${action}`, body))); }
    catch (e) { setReview({ text: (e as Error).message, warn: '', start: '', action: '', body: {} }); }
    finally { setLoading(''); }
  }
  const planCopy = (target: { library_id: string; name: string }) => plan('copy-plan', { to: target.library_id }, '正在比对…', p => {
    const creators = (p.creators as string[] | null) ?? [];
    return { text: `${source!.name} → ${target.name}：${p.files > 0 ? `${creators.length} 个博主，复制 ${p.files} 个文件（${gb(p.bytes)}）${creatorsText(creators)}` : '没有要补充的。'}`,
      warn: conflictsText(p.conflicts ?? []), start: p.files > 0 ? '开始补充' : '', action: 'copy', body: { to: target.library_id } };
  });
  const planPhone = () => plan('phone-plan', {}, '正在连接手机、读取手机上的文件清单（文件多时要十几秒）…', p => {
    const creators = (p.creators as string[] | null) ?? [];
    const free = p.phone_free ?? 0;
    return { text: `${source!.name} → 手机：${p.files > 0 ? `${creators.length} 个博主，复制 ${p.files} 个文件（${gb(p.bytes)}）${creatorsText(creators)}\n传输时别拔线；断了重新点一次，从断点接着传。` : '手机上已经都有了。'}`,
      warn: [free && p.bytes > free - 10 * 1073741824 ? `手机只剩 ${gb(free)}，放不下这次的 ${gb(p.bytes)}：会传到手机剩 10 GB 左右时停下，没传的下次接着传。` : free ? `手机剩余 ${gb(free)}。` : '', conflictsText(p.conflicts ?? [])].filter(Boolean).join('\n'),
      start: p.files > 0 ? '开始同步到手机' : '', action: 'phone', body: {} };
  });
  const planClean = () => plan('clean-plan', {}, '正在连接手机、核对哪些视频手机上已经有了…', p => {
    const kept = [p.not_on_phone ? `手机上还没有（或大小不同）的 ${p.not_on_phone} 个` : '', p.not_on_keep ? `SD 卡已有的博主那边还没补上的 ${p.not_on_keep} 个` : ''].filter(Boolean).join('，');
    return { text: p.files > 0 ? `移进回收站：${p.creators} 个博主的 ${p.files} 个文件（${gb(p.bytes)}），都已在手机上（同名同大小）。downloaded.txt 全部保留。` : '没有可以清理的。',
      warn: kept ? `保留不动：${kept}。` : '', start: p.files > 0 ? '移进回收站' : '', action: 'clean-phone', body: {} };
  });

  const cleanable = processed.reduce((n, l) => n + (l.error ? 0 : l.stats.albums_cleanable), 0);
  const importable = processed.reduce((n, l) => n + (l.error ? 0 : l.stats.albums_on_disk - l.stats.albums_cleanable), 0);
  const mergeTargets = libraries.filter(l => !l.error);
  return <section className="space-y-4">
    {error ? <p role="alert" className="text-danger">{error}</p> : null}
    <div className="space-y-3 rounded-lg border border-line bg-panel p-4">
      <label className="flex items-center gap-3"><input type="checkbox" checked={!!w?.thumbnails_enabled} disabled={!w} onChange={e => void act('thumbnails', { enabled: e.target.checked })} />
        <span>生成缩略图</span></label>
      <details className="settings-help"><summary>缩略图生成说明</summary><p>后台逐个生成并存入数据库，已有旧封面直接转存。手机电池温度到 40°C 暂停，降到 37°C 以下继续。关闭后停止生成，已有缩略图仍可使用。</p></details>
      <div className="flex flex-wrap gap-2">
        <button className={button} disabled={busy} onClick={() => void act('scan')}>重新扫描</button>
        <button className={button} disabled={busy || importable <= 0} onClick={() => void act('import')}>图集入库（{importable}）</button>
        <button className={button} disabled={busy || cleanable === 0} onClick={() => setConfirmClean(true)}>清理已入库原目录（{cleanable}）</button>
        <button className={button} disabled={busy || !mergeTargets.length} onClick={() => { setMerging(m => !m); setMergeTarget(t => t || mergeTargets[0]?.library_id || ''); }}>合并其他设备的数据…</button>
        {busy && w?.job !== 'thumbnails' ? <button className={button} onClick={() => void act('cancel')}>取消当前任务</button> : null}
      </div>
      {confirmClean ? <div className="rounded-md border border-primary p-3 text-sm">
        <p>逐个核对原目录与 albums.db 中的入库内容（文件名、数量、SHA-256），完全一致的目录移入系统回收站；有任何差异的保留不动。背景音乐 mp3 不受影响。</p>
        <div className="mt-2 flex flex-wrap gap-2"><button className={primaryButton} onClick={() => { setConfirmClean(false); void act('clean'); }}>校验并移入回收站</button><button className={button} onClick={() => setConfirmClean(false)}>取消</button></div>
      </div> : null}
      {merging ? <div className="space-y-2 rounded-md border border-line p-3 text-sm">
        <p className="text-muted">视频和没入库的图集是文件，直接拷进对应博主目录再「重新扫描」即可。已入库并清理了原目录的图集只在数据库里：选一个装着另一台设备 albums.db（同目录的 index.db、thumbnails.db 一起）的文件夹并进来。同一作品内容不同时保留本库的。</p>
        {mergeTargets.length > 1 ? <label className="flex items-center gap-2">合并到<select className="rounded border border-line bg-background px-2 py-1" value={mergeTarget} onChange={e => setMergeTarget(e.target.value)}>{mergeTargets.map(l => <option key={l.library_id} value={l.library_id}>{l.name}</option>)}</select></label> : null}
        <DirectoryPicker onPick={path => { setMerging(false); void act('merge', { library_id: mergeTarget, path }); }} onCancel={() => setMerging(false)} />
      </div> : null}
      {source ? <div className="space-y-2 border-t border-line pt-3">
        <h3 className="font-semibold">短视频移动</h3>
        <div className="flex flex-wrap gap-2">
          {(status?.copy_targets ?? []).map(t => <button key={t.library_id} className={button} disabled={busy || !!loading} onClick={() => void planCopy(t)}>{source.name} → {t.name}</button>)}
          <button className={button} disabled={busy || !!loading} onClick={() => void planPhone()}>{source.name} → 手机（数据线）</button>
          <button className={button} disabled={busy || !!loading} onClick={() => void planClean()}>清理已同步到手机的视频</button>
        </div>
        <p className="whitespace-pre-line text-sm text-muted">{`新下载的视频在「${source.name}」，这里只做中转：不生成缩略图、不入库图集。每个按钮先列出要做的事，确认后才执行；中断后再点一次接着做。只处理浏览器插件下载的平台（抖音、快手、TikTok、小红书），MV 等不动。
• 补到 SD 卡：只补 SD 卡已有的博主。• 补到手机：所有博主都补；数据线断了会等手机重新连上接着传。目标没有的文件才复制，写完核对大小再放进去；同名但大小不同的不覆盖，只列出来；不删除目标的任何文件。
• downloaded.txt 只留在「${source.name}」（浏览器插件按它判断下过没有）。
• 清理：手机上有同名同大小的文件、且 SD 卡已有的博主那边也补上了，才移进回收站；downloaded.txt 全部保留。`}</p>
      </div> : null}
      {loading ? <p role="status" className="text-sm">{loading}</p> : null}
      {status?.phone_maintenance ? <PhoneThumbnails busy={busy} review={w?.phone_review} act={act} /> : null}
      {w?.logs?.length ? <details><summary className="cursor-pointer text-sm">过程日志（最近 {w.logs.length} 条）</summary><pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-background p-2 text-xs">{w.logs.map(l => `${l.time} ${l.message}`).join('\n')}</pre></details> : null}
      {review ? <div className="space-y-2 rounded-md border border-primary p-3 text-sm">
        <p className="whitespace-pre-line break-all">{review.text}</p>
        {review.warn ? <p className="whitespace-pre-line break-all text-[#d29922]">{review.warn}</p> : null}
        <div className="flex flex-wrap gap-2">
          {review.start ? <button className={primaryButton} disabled={busy} onClick={() => { void act(review.action, review.body); setReview(undefined); }}>{review.start}</button> : null}
          <button className={button} onClick={() => setReview(undefined)}>{review.start ? '取消' : '知道了'}</button>
        </div>
      </div> : null}
      <p className="whitespace-pre-line text-sm" role="status">
        {!w ? '正在读取…' : w.job && w.job !== 'thumbnails' ? `正在${jobNames[w.job] ?? w.job}：${w.done}/${w.total}${w.detail ? `\n${w.detail}` : ''}` : w.detail ? w.detail : w.last_job ? `上次${jobNames[w.last_job] ?? w.last_job}：${w.last_note || '完成'}` : '空闲'}
      </p>
      {w?.last_error ? <p className="whitespace-pre-wrap break-all text-sm text-danger">{w.last_error}</p> : null}
    </div>
    <div className="grid gap-3 md:grid-cols-2">{libraries.map(lib => <article key={lib.library_id} className="rounded-lg border border-line bg-panel p-4 text-sm">
      <h3 className="mb-2 font-semibold">{lib.name}</h3>
      {lib.error ? <p className="text-muted">{lib.error}</p> : lib.copy_source ? <ul className="space-y-1">
        <li>视频 {lib.stats.videos}，图集 {lib.stats.albums}</li>
        <li className="text-muted">新下载的中转区：不生成缩略图、不入库图集</li>
      </ul> : <ul className="space-y-1">
        <li>视频 {lib.stats.videos}，图集 {lib.stats.albums}</li>
        <li>编码/尺寸已探测 {lib.stats.videos_probed} / {lib.stats.videos}（后台自动进行，用于竖屏与浏览器解码过滤）</li>
        <li>缩略图 {lib.stats.thumbnails} / {lib.stats.videos + lib.stats.albums}{lib.stats.thumbnail_failures ? `（${lib.stats.thumbnail_failures} 个源文件无法解码）` : ''}</li>
        <li>图集已入库 {lib.stats.albums_imported}，原目录仍在磁盘 {lib.stats.albums_on_disk}</li>
      </ul>}
    </article>)}</div>
  </section>;
}

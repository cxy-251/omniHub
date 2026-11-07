import { useCallback, useEffect, useRef, useState } from 'react';
import { api, post, request } from './api';
import { button, chip, primaryButton, select } from './ui';

export interface PDFDocInfo {
  id: string;
  name: string;
  size_bytes: number;
  pages: number;
  page_width_pt: number;
  page_height_pt: number;
  title?: string;
  author?: string;
  encrypted: boolean;
  created_at: string;
}

interface PageSpecItem {
  key: string;
  doc_id: string;
  doc_name: string;
  page: number;
  rotate: number;
  selected: boolean;
}

type WorkMode = 'arrange' | 'reader' | 'split' | 'appearance' | 'security';

const fmtBytes = (n: number) =>
  n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(2)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;

export default function PdfTools() {
  const [docs, setDocs] = useState<PDFDocInfo[]>([]);
  const [sharedPdfs, setSharedPdfs] = useState<string[]>([]);
  const [activeId, setActiveId] = useState<string>('');
  const [mode, setMode] = useState<WorkMode>('arrange');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');

  // Arrange & Merge queue
  const [pageQueue, setPageQueue] = useState<PageSpecItem[]>([]);
  const [arrangeOutName, setArrangeOutName] = useState('');
  const [appendDocId, setAppendDocId] = useState('');

  // Reader & Text extraction
  const [readerPage, setReaderPage] = useState(1);
  const [readerDpi, setReaderDpi] = useState(130);
  const [extractedText, setExtractedText] = useState('');
  const [textScope, setTextScope] = useState<'page' | 'all'>('page');
  const [loadingText, setLoadingText] = useState(false);

  // Split & Image export
  const [splitMode, setSplitMode] = useState<'range' | 'every_n'>('range');
  const [splitRange, setSplitRange] = useState('1-2');
  const [splitEveryN, setSplitEveryN] = useState(2);
  const [exportDpi, setExportDpi] = useState(150);

  // Watermark & Optimize
  const [wmText, setWmText] = useState('OMNIHUB · 内部资料');
  const [wmColor, setWmColor] = useState('#475569');
  const [wmFontSize, setWmFontSize] = useState(32);
  const [wmAngle, setWmAngle] = useState(-32);
  const [wmOpacity, setWmOpacity] = useState(0.22);
  const [optQuality, setOptQuality] = useState<'screen' | 'ebook' | 'printer'>('ebook');
  const [optGrayscale, setOptGrayscale] = useState(false);

  // Security
  const [secMode, setSecMode] = useState<'encrypt' | 'decrypt'>('encrypt');
  const [secPassword, setSecPassword] = useState('');

  const pdfInputRef = useRef<HTMLInputElement>(null);
  const imgInputRef = useRef<HTMLInputElement>(null);
  const wmCanvasRef = useRef<HTMLCanvasElement>(null);

  const activeDoc = docs.find(d => d.id === activeId) || docs[0];

  const resetQueueFromDoc = useCallback((doc: PDFDocInfo | undefined) => {
    if (!doc) {
      setPageQueue([]);
      return;
    }
    const items: PageSpecItem[] = [];
    for (let p = 1; p <= doc.pages; p++) {
      items.push({
        key: `${doc.id}-p${p}-${Math.random().toString(36).slice(2, 7)}`,
        doc_id: doc.id,
        doc_name: doc.name,
        page: p,
        rotate: 0,
        selected: false,
      });
    }
    setPageQueue(items);
    const base = doc.name.replace(/\.pdf$/i, '');
    setArrangeOutName(`${base}-edited.pdf`);
    setReaderPage(1);
    setSplitRange(`1-${Math.min(2, doc.pages)}`);
  }, []);

  const loadWorkspace = useCallback(async (preferId?: string) => {
    try {
      const res = await api<{ docs: PDFDocInfo[]; shared_pdfs: string[] }>('/api/tools/pdf/docs');
      const list = res.docs || [];
      setDocs(list);
      setSharedPdfs(res.shared_pdfs || []);
      if (preferId && list.some(d => d.id === preferId)) {
        setActiveId(preferId);
        resetQueueFromDoc(list.find(d => d.id === preferId));
      } else if (!list.some(d => d.id === activeId)) {
        const first = list[0];
        setActiveId(first?.id || '');
        resetQueueFromDoc(first);
      }
    } catch (e) {
      setError((e as Error).message);
    }
  }, [activeId, resetQueueFromDoc]);

  useEffect(() => {
    void loadWorkspace();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const selectDoc = (doc: PDFDocInfo) => {
    setActiveId(doc.id);
    setError('');
    setNotice('');
    resetQueueFromDoc(doc);
  };

  const handleUploadPDFs = async (files: FileList | null) => {
    if (!files?.length) return;
    setBusy(true);
    setError('');
    setNotice('');
    let lastCreated: PDFDocInfo | undefined;
    try {
      for (const file of Array.from(files)) {
        const res = await request(`/api/tools/pdf/upload?filename=${encodeURIComponent(file.name)}`, {
          method: 'POST',
          body: file,
        });
        lastCreated = (await res.json()) as PDFDocInfo;
      }
      if (lastCreated) {
        setNotice(`已载入 ${files.length} 个 PDF 文档`);
        await loadWorkspace(lastCreated.id);
      }
    } catch (e) {
      setError(`上传失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
      if (pdfInputRef.current) pdfInputRef.current.value = '';
    }
  };

  const handleCreateSample = async () => {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const doc = await post<PDFDocInfo>('/api/tools/pdf/sample', {});
      setNotice(`已生成 6 页示例文档：${doc.name}`);
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`生成示例失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleImportShared = async (name: string) => {
    if (!name) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const doc = await post<PDFDocInfo>('/api/tools/pdf/import-shared', { name });
      setNotice(`已从共享文件导入：${doc.name}`);
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`导入失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleImagesToPDF = async (files: FileList | null) => {
    if (!files?.length) return;
    setBusy(true);
    setError('');
    setNotice('正在将图片合成为 PDF…');
    try {
      const dataUrls: string[] = [];
      for (const file of Array.from(files)) {
        const url = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(String(reader.result));
          reader.onerror = () => reject(new Error('读取图片失败'));
          reader.readAsDataURL(file);
        });
        dataUrls.push(url);
      }
      const doc = await post<PDFDocInfo>('/api/tools/pdf/images-to-pdf', {
        images: dataUrls,
        out_name: `images-${files.length}p.pdf`,
      });
      setNotice(`已将 ${files.length} 张图片合成为 PDF：${doc.name}`);
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`图片合成 PDF 失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
      if (imgInputRef.current) imgInputRef.current.value = '';
    }
  };

  const handleSaveToShared = async (doc: PDFDocInfo) => {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const res = await post<{ saved_name: string }>('/api/tools/pdf/save-shared', {
        id: doc.id,
        name: doc.name,
      });
      setNotice(`已保存至「共享文件」：${res.saved_name}`);
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`保存失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleDeleteDoc = async (id: string) => {
    setBusy(true);
    setError('');
    try {
      await post('/api/tools/pdf/delete', { id });
      await loadWorkspace();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  // --- Arrange & Merge operations ---
  const selectedCount = pageQueue.filter(p => p.selected).length;

  const toggleSelectAllPages = () => {
    const allSel = pageQueue.length > 0 && selectedCount === pageQueue.length;
    setPageQueue(prev => prev.map(p => ({ ...p, selected: !allSel })));
  };

  const rotateSelectedOrAll = (delta: number) => {
    setPageQueue(prev =>
      prev.map(p => {
        if (selectedCount > 0 && !p.selected) return p;
        return { ...p, rotate: (((p.rotate + delta) % 360) + 360) % 360 };
      }),
    );
  };

  const rotateSinglePage = (idx: number, delta: number) => {
    setPageQueue(prev =>
      prev.map((p, i) => (i === idx ? { ...p, rotate: (((p.rotate + delta) % 360) + 360) % 360 } : p)),
    );
  };

  const moveSinglePage = (idx: number, dir: -1 | 1) => {
    const target = idx + dir;
    if (target < 0 || target >= pageQueue.length) return;
    setPageQueue(prev => {
      const copy = [...prev];
      const [item] = copy.splice(idx, 1);
      copy.splice(target, 0, item);
      return copy;
    });
  };

  const deleteSinglePage = (idx: number) => {
    if (pageQueue.length <= 1) {
      setError('至少需要保留 1 页');
      return;
    }
    setPageQueue(prev => prev.filter((_, i) => i !== idx));
  };

  const deleteSelectedPages = () => {
    const remaining = pageQueue.filter(p => !p.selected);
    if (remaining.length === 0) {
      setError('不能删除全部页面，至少需保留 1 页');
      return;
    }
    setPageQueue(remaining);
  };

  const keepOnlySelectedPages = () => {
    const kept = pageQueue.filter(p => p.selected);
    if (kept.length === 0) return;
    setPageQueue(kept.map(p => ({ ...p, selected: false })));
  };

  const reversePageOrder = () => {
    setPageQueue(prev => [...prev].reverse());
  };

  const appendOtherDocPages = () => {
    const targetDoc = docs.find(d => d.id === appendDocId);
    if (!targetDoc) return;
    const extra: PageSpecItem[] = [];
    for (let p = 1; p <= targetDoc.pages; p++) {
      extra.push({
        key: `${targetDoc.id}-p${p}-${Math.random().toString(36).slice(2, 7)}`,
        doc_id: targetDoc.id,
        doc_name: targetDoc.name,
        page: p,
        rotate: 0,
        selected: false,
      });
    }
    setPageQueue(prev => [...prev, ...extra]);
    setNotice(`已将「${targetDoc.name}」的 ${targetDoc.pages} 页追加到编排末尾`);
  };

  const applyArrange = async () => {
    if (!pageQueue.length) return;
    setBusy(true);
    setError('');
    setNotice('正在按当前页面顺序与旋转角度生成新 PDF…');
    try {
      const doc = await post<PDFDocInfo>('/api/tools/pdf/arrange', {
        specs: pageQueue.map(p => ({ doc_id: p.doc_id, page: p.page, rotate: p.rotate })),
        out_name: arrangeOutName.trim() || 'arranged.pdf',
      });
      setNotice(`已生成新文档：${doc.name}（共 ${doc.pages} 页）`);
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`页面重排失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  // --- Reader & Text extraction ---
  const fetchExtractedText = useCallback(async () => {
    if (!activeDoc) return;
    setLoadingText(true);
    setError('');
    try {
      const pg = textScope === 'all' ? 0 : readerPage;
      const res = await api<{ text: string }>(`/api/tools/pdf/text?id=${encodeURIComponent(activeDoc.id)}&page=${pg}`);
      setExtractedText(res.text || '（该页未提取到可识别文本）');
    } catch (e) {
      setError(`提取文本失败：${(e as Error).message}`);
    } finally {
      setLoadingText(false);
    }
  }, [activeDoc, readerPage, textScope]);

  useEffect(() => {
    if (mode === 'reader' && activeDoc) {
      void fetchExtractedText();
    }
  }, [mode, activeDoc, readerPage, textScope, fetchExtractedText]);

  // --- Split ---
  const handleSplit = async () => {
    if (!activeDoc) return;
    setBusy(true);
    setError('');
    setNotice('正在拆分 PDF…');
    try {
      const res = await post<{ docs: PDFDocInfo[] }>('/api/tools/pdf/split', {
        id: activeDoc.id,
        mode: splitMode,
        range: splitRange,
        every_n: splitEveryN,
      });
      const created = res.docs || [];
      setNotice(`拆分完成，已生成 ${created.length} 个新 PDF 文档`);
      await loadWorkspace(created[0]?.id || activeDoc.id);
    } catch (e) {
      setError(`拆分失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  // --- Watermark preview & apply ---
  useEffect(() => {
    if (mode !== 'appearance') return;
    const canvas = wmCanvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);

    const text = wmText.trim() || 'WATERMARK';
    ctx.save();
    ctx.globalAlpha = wmOpacity;
    ctx.fillStyle = wmColor;
    ctx.font = `bold ${wmFontSize}px system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';

    const rad = (wmAngle * Math.PI) / 180;
    for (let row = 0; row < 3; row++) {
      for (let col = 0; col < 2; col++) {
        const cx = w * (0.28 + col * 0.44);
        const cy = h * (0.2 + row * 0.3);
        ctx.save();
        ctx.translate(cx, cy);
        ctx.rotate(rad);
        ctx.fillText(text, 0, 0);
        ctx.restore();
      }
    }
    ctx.restore();
  }, [mode, wmText, wmColor, wmFontSize, wmAngle, wmOpacity]);

  const handleApplyWatermark = async () => {
    if (!activeDoc) return;
    const canvas = wmCanvasRef.current;
    const pngDataURL = canvas ? canvas.toDataURL('image/png') : '';
    setBusy(true);
    setError('');
    setNotice('正在叠加透明水印图层…');
    try {
      const doc = await post<PDFDocInfo>('/api/tools/pdf/watermark', {
        id: activeDoc.id,
        text: wmText,
        opacity: wmOpacity,
        png_data_url: pngDataURL,
      });
      setNotice(`已生成带水印文档：${doc.name}`);
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`添加水印失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleOptimize = async () => {
    if (!activeDoc) return;
    setBusy(true);
    setError('');
    setNotice('正在执行 Ghostscript 页面重采样与色彩转换…');
    try {
      const doc = await post<PDFDocInfo>('/api/tools/pdf/optimize', {
        id: activeDoc.id,
        quality: optQuality,
        grayscale: optGrayscale,
      });
      const ratio = activeDoc.size_bytes > 0 ? Math.round((doc.size_bytes / activeDoc.size_bytes) * 100) : 100;
      setNotice(`已生成优化文档：${doc.name}（体积 ${fmtBytes(doc.size_bytes)}，为原文件 ${ratio}%）`);
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`压缩/灰度转换失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  // --- Security ---
  const handleSecurity = async () => {
    if (!activeDoc) return;
    if (!secPassword.trim()) {
      setError('请输入密码');
      return;
    }
    setBusy(true);
    setError('');
    setNotice(secMode === 'encrypt' ? '正在执行 AES-256 加密…' : '正在解密 PDF…');
    try {
      const doc = await post<PDFDocInfo>('/api/tools/pdf/security', {
        id: activeDoc.id,
        mode: secMode,
        password: secPassword,
      });
      setNotice(`${secMode === 'encrypt' ? '加密' : '解密'}成功：${doc.name}`);
      setSecPassword('');
      await loadWorkspace(doc.id);
    } catch (e) {
      setError(`${secMode === 'encrypt' ? '加密' : '解密'}失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-3">
      {/* 顶部文件栏与快捷导入 */}
      <div className="rounded-lg border border-line bg-panel p-3 space-y-2.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h3 className="text-sm font-semibold">LocalPDF · 本地离线 PDF 阅读与页面加工台</h3>
            <p className="text-xs text-muted">
              全部拆分、合并、旋转、水印、灰度压缩、AES-256 加解密与图片互转均在本机完成
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            <input
              ref={pdfInputRef}
              type="file"
              accept=".pdf,application/pdf"
              multiple
              className="hidden"
              onChange={e => void handleUploadPDFs(e.target.files)}
            />
            <input
              ref={imgInputRef}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              multiple
              className="hidden"
              onChange={e => void handleImagesToPDF(e.target.files)}
            />
            <button className={primaryButton} disabled={busy} onClick={() => pdfInputRef.current?.click()}>
              上传 PDF 文件
            </button>
            <button className={button} disabled={busy} onClick={() => imgInputRef.current?.click()}>
              多图合成 PDF
            </button>
            <button className={button} disabled={busy} onClick={() => void handleCreateSample()}>
              生成 6 页示例 PDF
            </button>
            {sharedPdfs.length > 0 ? (
              <select
                className={select}
                defaultValue=""
                disabled={busy}
                onChange={e => {
                  const val = e.target.value;
                  e.target.value = '';
                  void handleImportShared(val);
                }}
              >
                <option value="" disabled>
                  从共享文件导入 PDF ({sharedPdfs.length})…
                </option>
                {sharedPdfs.map(name => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            ) : null}
          </div>
        </div>

        {error ? <p className="rounded border border-danger/40 bg-danger/10 px-2.5 py-1.5 text-xs text-danger">{error}</p> : null}
        {notice ? <p className="rounded border border-primary/40 bg-primary/10 px-2.5 py-1.5 text-xs text-primary">{notice}</p> : null}

        {/* 工作区已加载文档列表 */}
        {docs.length > 0 ? (
          <div className="space-y-1.5 pt-1">
            <div className="text-xs text-muted">当前工作区文档（点击切换当前操作对象）：</div>
            <div className="flex flex-wrap gap-1.5">
              {docs.map(d => {
                const isCurrent = activeDoc?.id === d.id;
                return (
                  <div
                    key={d.id}
                    className={`flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs ${
                      isCurrent ? 'border-primary bg-primary/10 text-primary' : 'border-line bg-background text-foreground'
                    }`}
                  >
                    <button className="font-medium hover:underline text-left" onClick={() => selectDoc(d)}>
                      {d.encrypted ? '🔒 ' : '📄 '}
                      {d.name}
                    </button>
                    <span className="text-muted">
                      ({d.pages}页 · {fmtBytes(d.size_bytes)})
                    </span>
                    <a
                      className="text-primary hover:underline ml-1"
                      href={`/api/tools/pdf/download?id=${encodeURIComponent(d.id)}`}
                      download={d.name}
                      title="下载此 PDF"
                    >
                      下载
                    </a>
                    <button
                      className="text-muted hover:text-primary"
                      onClick={() => void handleSaveToShared(d)}
                      disabled={busy}
                      title="保存到共享文件夹"
                    >
                      存共享
                    </button>
                    <button
                      className="text-muted hover:text-danger"
                      onClick={() => void handleDeleteDoc(d.id)}
                      disabled={busy}
                      title="从工作区移除"
                    >
                      ×
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          <div className="rounded border border-dashed border-line py-6 text-center text-sm text-muted">
            工作区暂无 PDF，请点击上方「上传 PDF 文件」或「生成 6 页示例 PDF」开始操作
          </div>
        )}
      </div>

      {activeDoc ? (
        <>
          {/* 子功能模式切换 */}
          <div className="flex flex-wrap items-center gap-1.5">
            {(
              [
                ['arrange', '📑 页面重排 · 旋转 · 合并'],
                ['reader', '🔍 高清阅读 · 提取文本'],
                ['split', '✂️ 拆分提取 · 导出图片'],
                ['appearance', '💧 水印叠加 · 压缩灰度'],
                ['security', '🔒 密码加密 · 解密解锁'],
              ] as const
            ).map(([k, label]) => (
              <button key={k} className={chip(mode === k)} onClick={() => setMode(k)}>
                {label}
              </button>
            ))}
          </div>

          {/* 模式 1：页面重排、旋转、删除与多文档合并 */}
          {mode === 'arrange' ? (
            <div className="rounded-lg border border-line bg-panel p-3 space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line pb-2.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  <button className={button} onClick={toggleSelectAllPages}>
                    {selectedCount === pageQueue.length && pageQueue.length > 0 ? '取消全选' : `全选 (${pageQueue.length}页)`}
                  </button>
                  <button className={button} onClick={() => rotateSelectedOrAll(-90)}>
                    ↺ {selectedCount > 0 ? `选中左转90° (${selectedCount})` : '全部左转90°'}
                  </button>
                  <button className={button} onClick={() => rotateSelectedOrAll(90)}>
                    ↻ {selectedCount > 0 ? `选中右转90° (${selectedCount})` : '全部右转90°'}
                  </button>
                  <button className={button} onClick={() => rotateSelectedOrAll(180)}>
                    ↕ 翻转180°
                  </button>
                  <button className={button} onClick={reversePageOrder}>
                    ⇄ 倒序排列
                  </button>
                  {selectedCount > 0 ? (
                    <>
                      <button className={button} onClick={keepOnlySelectedPages}>
                        仅保留选中 ({selectedCount})
                      </button>
                      <button className={button} onClick={deleteSelectedPages}>
                        删除选中 ({selectedCount})
                      </button>
                    </>
                  ) : null}
                  <button className={button} onClick={() => resetQueueFromDoc(activeDoc)}>
                    重置编排
                  </button>
                </div>

                {/* 追加其他文档页实现多 PDF 合并 */}
                {docs.length > 1 ? (
                  <div className="flex items-center gap-1.5">
                    <select
                      className={select}
                      value={appendDocId}
                      onChange={e => setAppendDocId(e.target.value)}
                    >
                      <option value="">选择要合并追加的文档…</option>
                      {docs.map(d => (
                        <option key={d.id} value={d.id}>
                          {d.name} ({d.pages}页)
                        </option>
                      ))}
                    </select>
                    <button
                      className={button}
                      disabled={!appendDocId}
                      onClick={appendOtherDocPages}
                    >
                      + 追加到末尾
                    </button>
                  </div>
                ) : null}
              </div>

              {/* 缩略图卡片网格 */}
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
                {pageQueue.map((item, idx) => (
                  <div
                    key={item.key}
                    className={`group flex flex-col justify-between rounded-lg border p-2 transition ${
                      item.selected ? 'border-primary bg-primary/10' : 'border-line bg-background'
                    }`}
                  >
                    <div className="flex items-center justify-between gap-1 pb-1.5 text-xs">
                      <label className="flex items-center gap-1.5 cursor-pointer font-medium">
                        <input
                          type="checkbox"
                          checked={item.selected}
                          onChange={e =>
                            setPageQueue(prev =>
                              prev.map((p, i) => (i === idx ? { ...p, selected: e.target.checked } : p)),
                            )
                          }
                        />
                        <span>#{idx + 1}</span>
                      </label>
                      <span className="truncate text-[11px] text-muted" title={`${item.doc_name} 第 ${item.page} 页`}>
                        原P{item.page}
                        {item.rotate ? ` · +${item.rotate}°` : ''}
                      </span>
                    </div>

                    <div
                      className="relative flex aspect-[3/4] items-center justify-center overflow-hidden rounded border border-line/60 bg-panel cursor-pointer"
                      onClick={() =>
                        setPageQueue(prev =>
                          prev.map((p, i) => (i === idx ? { ...p, selected: !p.selected } : p)),
                        )
                      }
                    >
                      <img
                        src={`/api/tools/pdf/render?id=${encodeURIComponent(item.doc_id)}&page=${item.page}&dpi=72`}
                        alt={`Page ${item.page}`}
                        loading="lazy"
                        className="max-h-full max-w-full object-contain transition-transform duration-200"
                        style={{ transform: `rotate(${item.rotate}deg)` }}
                      />
                    </div>

                    <div className="flex items-center justify-between gap-1 pt-2 text-xs">
                      <button
                        className="rounded border border-line px-1.5 py-0.5 hover:border-primary disabled:opacity-35"
                        disabled={idx === 0}
                        onClick={() => moveSinglePage(idx, -1)}
                        title="前移一页"
                      >
                        ←
                      </button>
                      <button
                        className="rounded border border-line px-1.5 py-0.5 hover:border-primary"
                        onClick={() => rotateSinglePage(idx, 90)}
                        title="顺时针旋转 90°"
                      >
                        ↻
                      </button>
                      <button
                        className="rounded border border-line px-1.5 py-0.5 hover:border-primary disabled:opacity-35"
                        disabled={idx === pageQueue.length - 1}
                        onClick={() => moveSinglePage(idx, 1)}
                        title="后移一页"
                      >
                        →
                      </button>
                      <button
                        className="rounded border border-line px-1.5 py-0.5 text-muted hover:border-danger hover:text-danger"
                        onClick={() => deleteSinglePage(idx)}
                        title="删除此页"
                      >
                        删
                      </button>
                    </div>
                  </div>
                ))}
              </div>

              {/* 底部输出栏 */}
              <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line pt-2.5">
                <div className="text-xs text-muted">
                  当前编排共 <strong>{pageQueue.length}</strong> 页
                  {selectedCount > 0 ? `（已勾选 ${selectedCount} 页）` : ''}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    className={`${select} w-56`}
                    value={arrangeOutName}
                    onChange={e => setArrangeOutName(e.target.value)}
                    placeholder="输出文件名.pdf"
                  />
                  <button className={primaryButton} disabled={busy || !pageQueue.length} onClick={() => void applyArrange()}>
                    生成重排/合并后的新 PDF
                  </button>
                </div>
              </div>
            </div>
          ) : null}

          {/* 模式 2：高清阅读与文本提取 */}
          {mode === 'reader' ? (
            <div className="grid gap-3 lg:grid-cols-[1fr_22rem]">
              <div className="rounded-lg border border-line bg-panel p-3 space-y-2.5">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-1.5">
                    <button
                      className={button}
                      disabled={readerPage <= 1}
                      onClick={() => setReaderPage(p => Math.max(1, p - 1))}
                    >
                      上一页
                    </button>
                    <span className="text-xs font-mono px-2">
                      第 {readerPage} / {activeDoc.pages} 页
                    </span>
                    <button
                      className={button}
                      disabled={readerPage >= activeDoc.pages}
                      onClick={() => setReaderPage(p => Math.min(activeDoc.pages, p + 1))}
                    >
                      下一页
                    </button>
                  </div>
                  <div className="flex items-center gap-1.5 text-xs">
                    <span className="text-muted">渲染清晰度</span>
                    <select
                      className={select}
                      value={readerDpi}
                      onChange={e => setReaderDpi(Number(e.target.value))}
                    >
                      <option value={96}>标准 (96 DPI)</option>
                      <option value={130}>高清 (130 DPI)</option>
                      <option value={180}>超清 (180 DPI)</option>
                    </select>
                    <a
                      className={button}
                      href={`/api/tools/pdf/download?id=${encodeURIComponent(activeDoc.id)}&inline=1`}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      浏览器原生打开
                    </a>
                  </div>
                </div>

                <div className="flex max-h-[72vh] justify-center overflow-auto rounded border border-line bg-background p-3">
                  <img
                    src={`/api/tools/pdf/render?id=${encodeURIComponent(activeDoc.id)}&page=${readerPage}&dpi=${readerDpi}`}
                    alt={`Page ${readerPage}`}
                    className="max-w-full rounded shadow"
                  />
                </div>
              </div>

              <div className="rounded-lg border border-line bg-panel p-3 flex flex-col space-y-2.5">
                <div className="flex items-center justify-between gap-2">
                  <h4 className="text-sm font-semibold">文档信息与文本提取</h4>
                  <div className="flex gap-1">
                    <button
                      className={chip(textScope === 'page')}
                      onClick={() => setTextScope('page')}
                    >
                      当前页
                    </button>
                    <button
                      className={chip(textScope === 'all')}
                      onClick={() => setTextScope('all')}
                    >
                      全文
                    </button>
                  </div>
                </div>

                <div className="rounded border border-line bg-background p-2 text-xs space-y-1">
                  <div>
                    <span className="text-muted">文件名：</span>
                    {activeDoc.name}
                  </div>
                  <div>
                    <span className="text-muted">页面尺寸：</span>
                    {Math.round(activeDoc.page_width_pt)} × {Math.round(activeDoc.page_height_pt)} pt（共 {activeDoc.pages} 页）
                  </div>
                  {activeDoc.title ? (
                    <div>
                      <span className="text-muted">标题：</span>
                      {activeDoc.title}
                    </div>
                  ) : null}
                  {activeDoc.author ? (
                    <div>
                      <span className="text-muted">作者：</span>
                      {activeDoc.author}
                    </div>
                  ) : null}
                </div>

                <textarea
                  readOnly
                  value={loadingText ? '正在提取文本…' : extractedText}
                  className="min-h-72 flex-1 rounded border border-line bg-background p-2.5 font-mono text-xs"
                />

                <div className="flex gap-2">
                  <button
                    className={button}
                    onClick={() => {
                      if (!extractedText) return;
                      const blob = new Blob([extractedText], { type: 'text/plain;charset=utf-8' });
                      const a = document.createElement('a');
                      a.href = URL.createObjectURL(blob);
                      a.download = `${activeDoc.name.replace(/\.pdf$/i, '')}.txt`;
                      a.click();
                      URL.revokeObjectURL(a.href);
                    }}
                  >
                    导出 .txt 文件
                  </button>
                </div>
              </div>
            </div>
          ) : null}

          {/* 模式 3：拆分提取与导出图片 */}
          {mode === 'split' ? (
            <div className="grid gap-3 md:grid-cols-2">
              <div className="rounded-lg border border-line bg-panel p-4 space-y-3">
                <h4 className="text-sm font-semibold">✂️ 拆分 PDF 页面</h4>
                <div className="flex gap-2">
                  <button
                    className={chip(splitMode === 'range')}
                    onClick={() => setSplitMode('range')}
                  >
                    按页码区间提取
                  </button>
                  <button
                    className={chip(splitMode === 'every_n')}
                    onClick={() => setSplitMode('every_n')}
                  >
                    按固定页数均分
                  </button>
                </div>

                {splitMode === 'range' ? (
                  <div className="space-y-1.5 text-xs">
                    <label className="block text-muted">
                      输入页码或区间（当前文档共 {activeDoc.pages} 页，例如 <code className="font-mono">1-3, 5, 6-z</code>）：
                    </label>
                    <input
                      className={`${select} w-full font-mono`}
                      value={splitRange}
                      onChange={e => setSplitRange(e.target.value)}
                      placeholder="1-3, 5"
                    />
                  </div>
                ) : (
                  <div className="space-y-1.5 text-xs">
                    <label className="block text-muted">每隔多少页拆分为一个独立 PDF：</label>
                    <input
                      type="number"
                      min={1}
                      max={activeDoc.pages}
                      className={`${select} w-32`}
                      value={splitEveryN}
                      onChange={e => setSplitEveryN(Math.max(1, Number(e.target.value)))}
                    />
                  </div>
                )}

                <button className={primaryButton} disabled={busy} onClick={() => void handleSplit()}>
                  执行拆分并加入工作区
                </button>
              </div>

              <div className="rounded-lg border border-line bg-panel p-4 space-y-3">
                <h4 className="text-sm font-semibold">🖼️ 导出全部页面为 PNG 图片包 (.zip)</h4>
                <p className="text-xs text-muted">
                  将「{activeDoc.name}」的全部 {activeDoc.pages} 页渲染为 PNG 图片并打包为 ZIP 下载。
                </p>
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-muted">导出分辨率：</span>
                  <select
                    className={select}
                    value={exportDpi}
                    onChange={e => setExportDpi(Number(e.target.value))}
                  >
                    <option value={96}>96 DPI（轻量快速）</option>
                    <option value={150}>150 DPI（清晰推荐）</option>
                    <option value={200}>200 DPI（高分辨率）</option>
                  </select>
                </div>
                <div>
                  <a
                    className={primaryButton}
                    href={`/api/tools/pdf/export-images?id=${encodeURIComponent(activeDoc.id)}&dpi=${exportDpi}`}
                  >
                    打包下载全部页面图片 (.zip)
                  </a>
                </div>
              </div>
            </div>
          ) : null}

          {/* 模式 4：水印叠加、压缩与灰度化 */}
          {mode === 'appearance' ? (
            <div className="grid gap-3 md:grid-cols-2">
              <div className="rounded-lg border border-line bg-panel p-4 space-y-3">
                <h4 className="text-sm font-semibold">💧 页面水印叠加（支持中英文与自定义角度）</h4>
                <div className="grid gap-2 sm:grid-cols-2 text-xs">
                  <label className="space-y-1 sm:col-span-2">
                    <span className="text-muted">水印文字</span>
                    <input
                      className={`${select} w-full`}
                      value={wmText}
                      onChange={e => setWmText(e.target.value)}
                    />
                  </label>
                  <label className="space-y-1">
                    <span className="text-muted">字号 ({wmFontSize}px)</span>
                    <input
                      type="range"
                      min={16}
                      max={64}
                      value={wmFontSize}
                      onChange={e => setWmFontSize(Number(e.target.value))}
                      className="w-full"
                    />
                  </label>
                  <label className="space-y-1">
                    <span className="text-muted">旋转角度 ({wmAngle}°)</span>
                    <input
                      type="range"
                      min={-75}
                      max={75}
                      value={wmAngle}
                      onChange={e => setWmAngle(Number(e.target.value))}
                      className="w-full"
                    />
                  </label>
                  <label className="space-y-1">
                    <span className="text-muted">不透明度 ({Math.round(wmOpacity * 100)}%)</span>
                    <input
                      type="range"
                      min={0.06}
                      max={0.7}
                      step={0.02}
                      value={wmOpacity}
                      onChange={e => setWmOpacity(Number(e.target.value))}
                      className="w-full"
                    />
                  </label>
                  <label className="space-y-1">
                    <span className="text-muted">水印颜色</span>
                    <div className="flex items-center gap-2">
                      <input
                        type="color"
                        value={wmColor}
                        onChange={e => setWmColor(e.target.value)}
                        className="h-7 w-12 cursor-pointer rounded border border-line bg-background"
                      />
                      <span className="font-mono">{wmColor}</span>
                    </div>
                  </label>
                </div>

                <div className="space-y-1">
                  <div className="text-xs text-muted">实时透明水印图层预览：</div>
                  <div className="flex justify-center rounded border border-line bg-background p-2">
                    <canvas
                      ref={wmCanvasRef}
                      width={595}
                      height={842}
                      className="h-52 w-auto rounded border border-dashed border-line bg-white"
                    />
                  </div>
                </div>

                <button className={primaryButton} disabled={busy} onClick={() => void handleApplyWatermark()}>
                  叠加水印并生成新 PDF
                </button>
              </div>

              <div className="rounded-lg border border-line bg-panel p-4 space-y-3">
                <h4 className="text-sm font-semibold">🗜️ PDF 压缩与灰度化（Ghostscript 引擎）</h4>
                <p className="text-xs text-muted">
                  对文档内嵌图像进行重采样压缩，或将彩色扫描件转为纯黑白灰度文档以减小体积。
                </p>

                <div className="space-y-2 text-xs">
                  <label className="block space-y-1">
                    <span className="text-muted">压缩预设档位：</span>
                    <select
                      className={`${select} w-full`}
                      value={optQuality}
                      onChange={e => setOptQuality(e.target.value as 'screen' | 'ebook' | 'printer')}
                    >
                      <option value="screen">高压缩 · 屏幕阅读 (72 DPI，体积最小)</option>
                      <option value="ebook">均衡压缩 · 电子书模式 (150 DPI，推荐)</option>
                      <option value="printer">高质量 · 打印级优化 (300 DPI)</option>
                    </select>
                  </label>

                  <label className="flex items-center gap-2 pt-1 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={optGrayscale}
                      onChange={e => setOptGrayscale(e.target.checked)}
                    />
                    <span>同时将全部页面转换为黑白灰度（DeviceGray）</span>
                  </label>
                </div>

                <button className={primaryButton} disabled={busy} onClick={() => void handleOptimize()}>
                  执行压缩 / 灰度转换
                </button>
              </div>
            </div>
          ) : null}

          {/* 模式 5：加密与解密 */}
          {mode === 'security' ? (
            <div className="rounded-lg border border-line bg-panel p-4 max-w-xl space-y-3">
              <h4 className="text-sm font-semibold">🔒 PDF 密码保护与解锁（qpdf AES-256）</h4>
              <div className="flex gap-2">
                <button
                  className={chip(secMode === 'encrypt')}
                  onClick={() => setSecMode('encrypt')}
                >
                  设置访问密码（AES-256 加密）
                </button>
                <button
                  className={chip(secMode === 'decrypt')}
                  onClick={() => setSecMode('decrypt')}
                >
                  移除密码保护（解密解锁）
                </button>
              </div>

              <div className="space-y-1.5 text-xs">
                <label className="block text-muted">
                  {secMode === 'encrypt' ? '输入要设置的打开密码：' : '输入原 PDF 的密码以移除保护：'}
                </label>
                <input
                  type="password"
                  className={`${select} w-full`}
                  value={secPassword}
                  onChange={e => setSecPassword(e.target.value)}
                  placeholder="输入密码…"
                />
              </div>

              <button className={primaryButton} disabled={busy || !secPassword.trim()} onClick={() => void handleSecurity()}>
                {secMode === 'encrypt' ? '加密并生成受保护 PDF' : '解密并生成无密码 PDF'}
              </button>
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

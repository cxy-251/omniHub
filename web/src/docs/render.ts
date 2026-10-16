// 文档渲染：Markdown（marked）与 RST（移植自 omni-deck reader.js 的 parseRstToHtml），
// 公式用 KaTeX，图表用 Mermaid；输出经 DOMPurify 清理。依赖都按需加载（版本由 pnpm-lock 固定）。
// 文档之间的相对链接渲染成 <a data-doc="…">，由阅读器拦截跳转；外部链接新窗口打开。
import type DOMPurifyType from 'dompurify';

type KaTeX = typeof import('katex');
let katexPromise: Promise<KaTeX> | undefined;
let purifyPromise: Promise<typeof DOMPurifyType> | undefined;
async function loadKatex() {
  katexPromise ??= Promise.all([import('katex'), import('katex/dist/katex.min.css')]).then(([m]) => m.default as unknown as KaTeX);
  return katexPromise;
}
async function loadPurify() {
  purifyPromise ??= import('dompurify').then(m => m.default);
  return purifyPromise;
}

export const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

// 文档生成时 "\\to" "\\text" "\\frac" 之类常被当成字符串转义写坏成 Tab/换页符等控制字符，
// 公式里不可能出现这些字符，渲染前还原回反斜杠命令。
function repairTexEscapes(tex: string) {
  return tex.replace(/[\t\f\x08\v\r](?=[a-zA-Z])/g, c => '\\' + ({ '\t': 't', '\f': 'f', '\x08': 'b', '\v': 'v', '\r': 'r' } as Record<string, string>)[c]);
}
function renderTex(katex: KaTeX, tex: string, display: boolean) {
  try {
    const html = katex.renderToString(repairTexEscapes(tex).trim(), { displayMode: display, throwOnError: false, strict: false });
    return display ? `<div class="katex-display">${html}</div>` : html;
  } catch {
    return `<code>${escapeHtml(tex)}</code>`;
  }
}

const isDocLink = (href: string) => !/^[a-z]+:/i.test(href) && !href.startsWith('#') && /\.(md|markdown|rst|txt)(#.*)?$/i.test(href);

async function sanitize(html: string) {
  const purify = await loadPurify();
  return purify.sanitize(html, { ADD_ATTR: ['data-doc', 'target'], ADD_TAGS: ['semantics', 'annotation'] });
}

// ---------- Markdown ----------
export async function renderMarkdownDoc(text: string): Promise<string> {
  const [{ marked }, katex] = await Promise.all([import('marked'), loadKatex()]);
  const held: string[] = [];
  const hold = (html: string) => `\u0000${held.push(html) - 1}\u0000`;
  // 代码块与行内代码里的 $ 不是公式：先拆开，只在其余部分里找公式。
  // 开头的 YAML front matter（--- … ---，文档站点用的元数据）不显示。
  const parts = text.replace(/\r\n/g, '\n').replace(/^---\n[\s\S]*?\n---\n/, '').split(/(```[\s\S]*?```|`[^`\n]+`)/g);
  const prepared = parts.map((part, i) => {
    if (i % 2 === 1) {
      const mermaid = part.match(/^```mermaid\s*\n([\s\S]*?)```$/);
      return mermaid ? hold(`<div class="mermaid">${escapeHtml(mermaid[1].trim())}</div>`) : part;
    }
    return part
      .replace(/\$\$([\s\S]+?)\$\$|\\\[([\s\S]+?)\\\]/g, (_, a, b) => hold(renderTex(katex, a ?? b, true)))
      .replace(/(?<![\\$\w])\$(?![ \n])([^$\n]+?)(?<![ \\])\$(?![\d\w])|\\\(([\s\S]+?)\\\)/g, (_, a, b) => hold(renderTex(katex, a ?? b, false)));
  }).join('');
  // 标题后的 {#id}（pandoc 写法）：去掉文字，渲染成锚点 id。
  const withIds = prepared.replace(/^(#{1,6} .*?)\s*\{#([\w.-]+)\}[ \t]*$/gm, (_, head: string, id: string) => `${head} <span id="${id}"></span>`);
  let html = await marked.parse(withIds);
  html = html.replace(/\u0000(\d+)\u0000/g, (_, n) => held[Number(n)]);
  html = html.replace(/<a href="([^"]+)"/g, (m, href) => isDocLink(href) ? `<a href="#" data-doc="${href}"` : /^https?:/i.test(href) ? `<a href="${href}" target="_blank" rel="noopener noreferrer"` : m);
  return sanitize(html);
}

// ---------- RST ----------
export async function renderRstDoc(rst: string): Promise<string> {
  const katex = await loadKatex();
  const lines = rst.replace(/\r\n/g, '\n').split('\n');
  const html: string[] = [];
  let i = 0;
  const indented = (l: string) => l.startsWith('   ') || l.startsWith('\t') || !l.trim();

  function inline(text: string) {
    const held: string[] = [];
    const hold = (h: string) => `\u0000${held.push(h) - 1}\u0000`;
    // 先收起 ``行内代码``，再提取公式：:math:`…`（标准 RST）、$$…$$ 与 $…$（很多文档混用了 Markdown 写法）
    let s = text.replace(/``([^`]+)``/g, (_, code) => hold(`<code>${escapeHtml(code)}</code>`));
    s = s.replace(/:math:`([^`]+)`/g, (_, code) => hold(renderTex(katex, code, false)));
    s = s.replace(/\$\$([\s\S]+?)\$\$/g, (_, code) => hold(renderTex(katex, code, true)));
    s = s.replace(/(?<![\\$\w])\$(?![ \n])([^$\n]+?)(?<![ \\])\$(?![\d\w])/g, (_, code) => hold(renderTex(katex, code, false)));
    s = escapeHtml(s);
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    // 链接 `文字 <地址>`_：文档链接站内跳转，其余新窗口打开
    s = s.replace(/`([^`]+?)\s+&lt;([^&]+?)&gt;`__?/g, (_, label, href) => isDocLink(href)
      ? `<a href="#" data-doc="${href}">📄 ${label}</a>`
      : `<a href="${href}" target="_blank" rel="noopener noreferrer">${label} ↗</a>`);
    s = s.replace(/`(https?:[^`]+)`_/g, '<a href="$1" target="_blank" rel="noopener noreferrer">$1 ↗</a>');
    return s.replace(/\u0000(\d+)\u0000/g, (_, n) => held[Number(n)]);
  }
  function block(): string[] {
    const out: string[] = [];
    i++;
    while (i < lines.length && indented(lines[i])) { out.push(lines[i]); i++; }
    return out;
  }

  while (i < lines.length) {
    const line = lines[i], trimmed = line.trim();
    if (!trimmed) { i++; continue; }
    // 双线标题（上下都有装饰线）
    if (i + 2 < lines.length && /^[=\-~^]{3,}$/.test(trimmed) && /^[=\-~^]{3,}$/.test(lines[i + 2].trim()) && trimmed[0] === lines[i + 2].trim()[0]) {
      const tag = trimmed[0] === '=' ? 'h1' : 'h2';
      html.push(`<${tag}>${inline(lines[i + 1].trim())}</${tag}>`);
      i += 3; continue;
    }
    // 单线下划线标题
    if (i + 1 < lines.length && /^[=\-~^`#"']{3,}$/.test(lines[i + 1].trim()) && !/^[=\-~^`#"']{3,}$/.test(trimmed)) {
      const c = lines[i + 1].trim()[0];
      const tag = c === '=' ? 'h2' : c === '-' ? 'h3' : 'h4';
      html.push(`<${tag}>${inline(trimmed)}</${tag}>`);
      i += 2; continue;
    }
    // .. list-table::
    const table = trimmed.match(/^\.\.\s+list-table::\s*(.*)$/i);
    if (table) {
      const body = block().map(l => l.trim()).filter(Boolean);
      const rows: string[][] = [];
      let row: string[] | null = null, cell: string[] = [];
      for (const tl of body) {
        if (/^:(widths|header-rows|align|class|width):/.test(tl)) continue;
        if (tl.startsWith('* -')) { if (row) { if (cell.length) row.push(cell.join(' ')); rows.push(row); } row = []; cell = [tl.slice(3).trim()]; }
        else if (tl.startsWith('- ')) { if (cell.length && row) row.push(cell.join(' ')); cell = [tl.slice(2).trim()]; }
        else cell.push(tl);
      }
      if (row) { if (cell.length) row.push(cell.join(' ')); rows.push(row); }
      let t = '<table>' + (table[1] ? `<caption>${inline(table[1])}</caption>` : '');
      if (rows.length) {
        t += '<thead><tr>' + rows[0].map(c => `<th>${inline(c)}</th>`).join('') + '</tr></thead><tbody>';
        t += rows.slice(1).map(r => '<tr>' + r.map(c => `<td>${inline(c)}</td>`).join('') + '</tr>').join('') + '</tbody>';
      }
      html.push(t + '</table>'); continue;
    }
    // .. mermaid:: / .. code-block:: mermaid
    if (/^\.\.\s+(?:mermaid::|code-block::\s*mermaid|code::\s*mermaid)/i.test(trimmed)) {
      const code = block().map(l => l.replace(/^ {3}|^\t/, '')).join('\n').trim();
      html.push(`<div class="mermaid">${escapeHtml(code)}</div>`); continue;
    }
    // .. math::（公式体 = 比指令行缩进更深的连续行，指令本身可能缩进在列表项里）
    if (/^\.\.\s+math::/i.test(trimmed)) {
      const base = (line.match(/^ */) ?? [''])[0].length;
      const deeper = (l: string) => (l.match(/^ */) ?? [''])[0].length > base || (l.startsWith('\t') && base === 0);
      i++;
      const body: string[] = [];
      while (i < lines.length && (!lines[i].trim() || deeper(lines[i]))) { body.push(lines[i].replace(/^ +/, '').trimEnd()); i++; }
      // 先还原被写坏的转义再去首尾空白：\text 写坏成的 Tab 正好在行首，先 trim 会把它吃掉。
      html.push(renderTex(katex, repairTexEscapes(body.filter(l => !/^:[a-z-]+:/.test(l)).join('\n')), true)); continue;
    }
    // .. code-block:: / .. code::
    const code = trimmed.match(/^\.\.\s+(?:code-block|code|sourcecode)::\s*([\w-]*)$/i);
    if (code) {
      const body = block().filter(l => !/^\s+:[a-z-]+:/.test(l)).map(l => l.replace(/^ {3}|^\t/, '')).join('\n').trim();
      html.push(`<pre><code class="lang-${code[1] || 'text'}">${escapeHtml(body)}</code></pre>`); continue;
    }
    // 提示框
    const adm = trimmed.match(/^\.\.\s+(note|warning|tip|important|danger|caution|attention|hint|seealso)::\s*(.*)$/i);
    if (adm) {
      const first = adm[2].trim();
      const body = block().map(l => l.trim()).filter(Boolean);
      const titles: Record<string, string> = { note: '注意', warning: '警告', tip: '提示', important: '重要', danger: '危险', caution: '小心', attention: '注意', hint: '提示', seealso: '参见' };
      html.push(`<div class="admonition ${adm[1].toLowerCase()}"><p class="admonition-title">${titles[adm[1].toLowerCase()]}</p><p>${inline([first, ...body].filter(Boolean).join(' '))}</p></div>`); continue;
    }
    // 图片（文档目录里的相对图片暂不提供，显示说明文字）
    const img = trimmed.match(/^\.\.\s+(?:image|figure)::\s*(.+)$/i);
    if (img) {
      let caption = '';
      for (const l of block().map(x => x.trim())) if (l.startsWith(':alt:')) caption ||= l.slice(5).trim(); else if (l && !l.startsWith(':')) caption = l;
      html.push(/^https?:/i.test(img[1]) ? `<img src="${escapeHtml(img[1].trim())}" alt="${escapeHtml(caption)}">` : `<p class="doc-missing-image">🖼 ${escapeHtml(caption || img[1])}</p>`);
      continue;
    }
    // 其它指令（toctree、contents……）跳过
    if (/^\.\.\s+[\w-]+::/.test(trimmed) || /^\.\.\s/.test(trimmed) || trimmed === '..') { block(); continue; }
    // :: 结尾的段落后面是字面块
    // 列表
    if (/^[*\-+]\s+/.test(trimmed)) {
      const items: string[] = [];
      while (i < lines.length && /^[*\-+]\s+/.test(lines[i].trim())) {
        let item = lines[i].trim().replace(/^[*\-+]\s+/, '');
        i++;
        while (i < lines.length && lines[i].startsWith('  ') && lines[i].trim() && !/^[*\-+]\s+/.test(lines[i].trim())) { item += ' ' + lines[i].trim(); i++; }
        items.push(item);
      }
      html.push('<ul>' + items.map(x => `<li>${inline(x)}</li>`).join('') + '</ul>'); continue;
    }
    if (/^(\d+|#)\.\s+/.test(trimmed)) {
      const items: string[] = [];
      while (i < lines.length && /^(\d+|#)\.\s+/.test(lines[i].trim())) { items.push(lines[i].trim().replace(/^(\d+|#)\.\s+/, '')); i++; }
      html.push('<ol>' + items.map(x => `<li>${inline(x)}</li>`).join('') + '</ol>'); continue;
    }
    // 段落；以 :: 结尾时后面的缩进块是字面代码
    const para = [trimmed];
    i++;
    while (i < lines.length && lines[i].trim() && !lines[i].trim().startsWith('..') && !/^[=\-~^]{3,}$/.test(lines[i].trim()) && !/^[*\-+]\s+/.test(lines[i].trim()) && !/^\d+\.\s+/.test(lines[i].trim())) { para.push(lines[i].trim()); i++; }
    let text = para.join(' ');
    const literal = text.endsWith('::');
    if (literal) text = text.endsWith(' ::') ? text.slice(0, -3) : text.slice(0, -1);
    if (text) html.push(`<p>${inline(text)}</p>`);
    if (literal) {
      while (i < lines.length && !lines[i].trim()) i++;
      const body: string[] = [];
      while (i < lines.length && (lines[i].startsWith(' ') || lines[i].startsWith('\t') || !lines[i].trim())) { body.push(lines[i]); i++; }
      const indent = Math.min(...body.filter(l => l.trim()).map(l => (l.match(/^\s*/) ?? [''])[0].length));
      html.push(`<pre><code>${escapeHtml(body.map(l => l.slice(Number.isFinite(indent) ? indent : 0)).join('\n').trimEnd())}</code></pre>`);
    }
  }
  return sanitize(html.join('\n'));
}

// 渲染完插入页面后：画 Mermaid 图（严格安全模式）。
let mermaidReady: Promise<typeof import('mermaid').default> | undefined;
export async function drawDiagrams(container: HTMLElement) {
  const nodes = [...container.querySelectorAll<HTMLElement>('.mermaid:not([data-processed])')];
  if (!nodes.length) return;
  mermaidReady ??= import('mermaid').then(m => { m.default.initialize({ startOnLoad: false, theme: 'dark', securityLevel: 'strict', flowchart: { htmlLabels: false } }); return m.default; });
  const mermaid = await mermaidReady;
  try { await mermaid.run({ nodes }); } catch { /* 图表语法错误时保留源码 */ }
}

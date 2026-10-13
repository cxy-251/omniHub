import { request } from '../src/api';
let markedReady;
async function renderMarkdown(raw) {
 if (!markedReady) markedReady = new Promise((resolve, reject) => {
  const script = document.createElement('script');
  script.src = '/vendor/marked.min.js';
  script.onload = resolve; script.onerror = () => { markedReady = undefined; reject(new Error('Markdown 渲染器加载失败')); };
  document.head.appendChild(script);
 });
 const [{ default: DOMPurify }] = await Promise.all([import('dompurify'), markedReady]);
 return DOMPurify.sanitize(window.marked.parse(raw));
}
// omniHub 媒体专区交互逻辑
let lifetime = new AbortController();
const fetch = (input, options = {}) => request(input, { ...options, signal: lifetime.signal });
const MediaHub = {
  dispose() { this.saveAudioProgress(true, true); lifetime.abort();
    if (this.audioTimer) clearInterval(this.audioTimer);
    if (this.mangaScrollTimer) clearTimeout(this.mangaScrollTimer);
    this.audioTimer = null;
    if (this.audioPlayer) { this.audioPlayer.onloadedmetadata = null; this.audioPlayer.pause(); this.audioPlayer.removeAttribute('src'); this.audioPlayer.load(); this.audioPlayer = null; }
    this.currentAudioId = '';
    this.currentSubtab = 'gallery';
    document.querySelectorAll('video').forEach(video => { video.pause(); video.removeAttribute('src'); video.load(); });
  },
  currentSubtab: 'gallery',
  audioPlayer: null,
  currentAudioId: '',
  audioTimer: null,
  mangaScrollTimer: null,
  currentMangaId: '',

  init() {
    lifetime = new AbortController();
    this.renderContainer();
    this.bindEvents();
    this.initAudioBar();
  },

  renderContainer() {
    const container = document.getElementById('media-container');
    if (!container) return;

    container.innerHTML = `
      <div class="media-subnav">
        <button data-sub="gallery" class="active">🖼️ 相册画廊</button>
        <button data-sub="shortvideo">🎬 短视频与图集</button>
        <button data-sub="text">📖 文本阅读器</button>
        <button data-sub="manga">📚 漫画条漫</button>
        <button data-sub="audio">🎧 音频专辑</button>
      </div>

      <!-- 文本阅读器 -->
      <div id="subpane-text" class="media-subpane">
        <div style="display:grid; grid-template-columns: 280px 1fr; gap:16px;">
          <div style="background:var(--surface); border:1px solid var(--border); border-radius:6px; padding:12px; max-height:650px; overflow-y:auto;">
            <h4 style="margin:0 0 8px 0; color:var(--accent);">📚 小说与文档</h4>
            <div id="text-file-list" style="display:flex; flex-direction:column; gap:4px; font-size:13px; color:var(--text-muted);">
              正在获取文本列表…
            </div>
          </div>
          <div>
            <div class="reader-toolbar">
              <div style="display:flex; gap:8px;">
                <input type="text" id="text-path-input" placeholder="输入相对路径" style="width: 300px;">
                <button id="btn-text-load" class="btn-primary">加载</button>
              </div>
              <div style="display:flex; gap:8px;">
                <button id="btn-text-prev" class="btn-primary">上一页</button>
                <span id="text-page-info" style="font-size:13px; align-self:center;">-</span>
                <button id="btn-text-next" class="btn-primary">下一页</button>
              </div>
            </div>
            <div id="reader-content" class="reader-container">请从左侧选择文档或在上方输入路径进行阅读</div>
          </div>
        </div>
      </div>

      <!-- 漫画专区 -->
      <div id="subpane-manga" class="media-subpane">
        <!-- 书架视图 -->
        <div id="manga-shelf-view">
          <div style="margin-bottom:12px; display:flex; justify-content:space-between; align-items:center;">
            <span style="font-size:14px; color:var(--text-muted);">已索引漫画书架 (点击直接阅读条漫)</span>
            <button id="btn-manga-refresh" class="btn-primary" style="padding:4px 10px;">🔄 刷新书架</button>
          </div>
          <div id="manga-shelf-grid" class="gallery-grid">
            <div style="color:var(--text-muted);">正在加载漫画书架…</div>
          </div>
        </div>

        <!-- 条漫阅读视口 -->
        <div id="manga-reader-view" style="display:none;">
          <div style="margin-bottom: 8px; display:flex; gap:8px; align-items:center;">
            <button id="btn-manga-back" class="btn-primary" style="padding:4px 12px;">🔙 返回书架</button>
            <span id="manga-title-badge" style="font-weight:bold; color:#58a6ff;">-</span>
            <span id="manga-scroll-badge" style="font-size:12px; color:var(--text-muted);">断点: 0.0%</span>
          </div>
          <div id="manga-waterfall-box" class="manga-waterfall"></div>
        </div>
      </div>

      <div id="subpane-audio" class="media-subpane"><div id="audio-browser"></div></div>
      <!-- 全格式音频浮动底栏 -->
      <div id="audio-bar" class="audio-player-bar" style="display:none;">
        <div class="audio-track-info">
          <span id="audio-title" class="audio-track-title">未播放音频</span>
          <span id="audio-album" class="audio-track-album">无专辑</span>
        </div>
        <div class="audio-controls">
          <button id="audio-play-pause" class="btn-primary" style="padding:4px 10px;">▶</button>
          <input type="range" id="audio-progress" min="0" max="100" value="0">
          <span id="audio-error" role="status"></span>
          <span id="audio-time" style="font-size:12px; min-width:80px; text-align:right;">00:00 / 00:00</span>
        </div>
      </div>
    `;
  },

  bindEvents() {
    // 子导航切换
    document.querySelectorAll('.media-subnav button').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const sub = e.target.getAttribute('data-sub');
        if (sub) this.switchSubtab(sub);
      });
    });

    // 文本分页
    this.textState = { path: '', start: 1, offset: 0, nextOffset: 0, nextStart: 1, history: [], limit: 100, hasMore: false, sequence: 0 };
    const btnTextLoad = document.getElementById('btn-text-load');
    if (btnTextLoad) {
      btnTextLoad.addEventListener('click', () => {
        this.selectTextFile(document.getElementById('text-path-input').value.trim());
      });
    }
    const btnTextPrev = document.getElementById('btn-text-prev');
    if (btnTextPrev) {
      btnTextPrev.addEventListener('click', () => {
        if (this.textState.start > 1) {
          const previous = this.textState.history.pop();
          this.textState.start = previous?.start ?? Math.max(1, this.textState.start - this.textState.limit);
          this.textState.offset = previous?.offset ?? -1;
          this.loadTextChunk();
        }
      });
    }
    const btnTextNext = document.getElementById('btn-text-next');
    if (btnTextNext) {
      btnTextNext.addEventListener('click', () => {
        if (this.textState.hasMore) {
          this.textState.history.push({ start: this.textState.start, offset: this.textState.offset });
          if (this.textState.history.length > 256) this.textState.history.shift();
          this.textState.start = this.textState.nextStart;
          this.textState.offset = this.textState.nextOffset;
          this.loadTextChunk();
        }
      });
    }

    // 漫画书架与条漫导航
    const btnMangaRefresh = document.getElementById('btn-manga-refresh');
    if (btnMangaRefresh) {
      btnMangaRefresh.addEventListener('click', () => this.loadMangaShelf());
    }
    const btnMangaBack = document.getElementById('btn-manga-back');
    if (btnMangaBack) {
      btnMangaBack.addEventListener('click', () => this.closeMangaBook());
    }

    const btnManga = document.getElementById('btn-manga-load');
    if (btnManga) {
      btnManga.addEventListener('click', () => {
        const path = document.getElementById('manga-path-input').value.trim();
        if (path) this.loadManga(path);
      });
    }

    // 条漫垂直滚动监听防抖上报（第 7.3.3 节）
    const mangaBox = document.getElementById('manga-waterfall-box');
    if (mangaBox) {
      mangaBox.addEventListener('scroll', () => {
        if (!this.currentMangaId) return;
        const ratio = mangaBox.scrollTop / Math.max(1, (mangaBox.scrollHeight - mangaBox.clientHeight));
        document.getElementById('manga-scroll-badge').textContent = `断点: ${(ratio * 100).toFixed(1)}%`;

        clearTimeout(this.mangaScrollTimer);
        this.mangaScrollTimer = setTimeout(() => {
          this.saveMangaProgress(this.currentMangaId, ratio);
        }, 500);
      });
    }
  },

  switchSubtab(subId) {
    this.currentSubtab = subId;
    window.dispatchEvent(new CustomEvent('omni-media-tab', { detail: subId }));
    document.querySelectorAll('.media-subnav button').forEach(b => {
      b.classList.toggle('active', b.getAttribute('data-sub') === subId);
    });
    document.querySelectorAll('.media-subpane').forEach(p => {
      p.classList.toggle('active', p.id === `subpane-${subId}`);
    });
    if (subId === 'manga') this.loadMangaShelf();
    if (subId === 'text') this.loadTextList();
  },

  // --- 文本阅读器实现 ---
  async loadTextChunk() {
    const contentEl = document.getElementById('reader-content');
    const pageInfo = document.getElementById('text-page-info');
    if (!this.textState.path) return;

    const state = this.textState;
    const sequence = ++state.sequence;
    const current = () => this.textState === state && state.sequence === sequence && !lifetime.signal.aborted;
    const previousButton = document.getElementById('btn-text-prev');
    const nextButton = document.getElementById('btn-text-next');
    previousButton.disabled = true; nextButton.disabled = true;

    contentEl.textContent = '加载中…';
    try {
      const cursor = state.offset >= 0 ? `&offset=${state.offset}` : '';
      const res = await fetch(`/api/media/text/chunk?path=${encodeURIComponent(state.path)}&start=${state.start}&limit=${state.limit}${cursor}`);
      const data = await res.json();
      if (!current()) return;

      state.hasMore = data.has_more;
      state.nextOffset = data.next_offset;
      state.nextStart = data.start_line + data.lines.length;
      pageInfo.textContent = data.lines.length ? `行号: ${data.start_line} - ${state.nextStart - 1}${data.total_lines >= 0 ? ` (共 ${data.total_lines} 行)` : ''}` : '已到文件末尾';

      // 使用离线 marked.min.js 进行富文本与图表排版 (第 6.1 节)
      const rawText = data.lines.join('\n');
      if (state.path.toLowerCase().endsWith('.md')) {
        const html = await renderMarkdown(rawText);
        if (current()) contentEl.innerHTML = html;
      } else {
        contentEl.innerHTML = data.lines.map(line => `<div>${line.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;") || '&nbsp;'}</div>`).join('');
      }
    } catch (e) {
 if (e.name === 'AbortError') return;
      if (current()) contentEl.textContent = `加载错误: ${e.message}`;
    } finally {
      if (current()) { previousButton.disabled = state.start <= 1; nextButton.disabled = !state.hasMore; }
    }
  },

  async loadTextList(offset = 0) {
    const listEl = document.getElementById('text-file-list');
    if (!listEl) return;
    try {
      const res = await fetch('/api/media/text/list?limit=100&offset=' + offset);
      const hasMore = res.headers.get('X-Has-More') === 'true';
      const items = await res.json();
      if (!Array.isArray(items) || items.length === 0) {
        listEl.innerHTML = '<div style="color:var(--text-muted)">暂无可用文本</div>';
        return;
      }
      listEl.replaceChildren();
      for (const item of items) {
        const button = document.createElement('button'); button.className = 'btn-primary';
        button.textContent = `${item.name} · ${item.library_name}`; button.title = item.rel_path;
        button.addEventListener('click', () => this.selectTextFile(item.rel_path)); listEl.append(button);
      }
      for (const [label, next, show] of [['上一页', Math.max(0, offset - 100), offset > 0], ['下一页', offset + 100, hasMore]]) {
        if (!show) continue;
        const button = document.createElement('button'); button.textContent = label; button.className = 'btn-primary';
        button.addEventListener('click', () => this.loadTextList(next)); listEl.append(button);
      }
    } catch (e) {
 if (e.name === 'AbortError') return;
      listEl.textContent = `加载失败: ${e.message}`;
    }
  },

  selectTextFile(relPath) {
    const inp = document.getElementById('text-path-input');
    if (inp) inp.value = relPath;
    this.textState = { ...this.textState, path: relPath, start: 1, offset: 0, nextOffset: 0, nextStart: 1, history: [], hasMore: false, sequence: 0 };
    this.loadTextChunk();
  },

  // --- 漫画书架与条漫实现 ---
  async loadMangaShelf(offset = 0) {
    const grid = document.getElementById('manga-shelf-grid');
    if (!grid) return;
    try {
      const res = await fetch('/api/media/manga/list?limit=100&offset=' + offset);
      const hasMore = res.headers.get('X-Has-More') === 'true';
      const mangas = await res.json();
      if (!Array.isArray(mangas) || mangas.length === 0) {
        grid.innerHTML = '<div style="color:var(--text-muted)">书架暂无漫画</div>';
        return;
      }
      grid.replaceChildren();
      for (const m of mangas) {
        const logical = m.logical_path || 'media_library/manga/' + m.rel_path;
        const card = document.createElement('div');
        card.className = 'gallery-item'; card.style.aspectRatio = '3/4';
        card.addEventListener('click', () => this.openMangaBook(encodeURIComponent(logical), encodeURIComponent(m.title)));
        const image = document.createElement('img');
        image.src = '/api/media/manga/cover?path=' + encodeURIComponent(logical); image.loading = 'lazy'; image.alt = m.title;
        const info = document.createElement('div'); info.className = 'overlay-info';
        const title = document.createElement('span'); title.textContent = m.title;
        const subtitle = document.createElement('span'); subtitle.textContent = `${m.library_name || ''} · 共 ${m.total_pages} 页`;
        info.append(title, subtitle); card.append(image, info); grid.append(card);
      }
      const navigation = document.createElement('div');
      for (const [label, next, show] of [['上一页', Math.max(0, offset - 100), offset > 0], ['下一页', offset + 100, hasMore]]) {
        if (!show) continue;
        const button = document.createElement('button'); button.className = 'btn-primary'; button.textContent = label;
        button.addEventListener('click', () => this.loadMangaShelf(next)); navigation.append(button);
      }
      grid.append(navigation);
    } catch (e) {
 if (e.name === 'AbortError') return;
      grid.innerHTML = `<div style="color:var(--danger)">加载书架失败: ${e.message}</div>`;
    }
  },

  openMangaBook(encodedRelPath, encodedTitle) {
    const relPath = decodeURIComponent(encodedRelPath);
    const title = decodeURIComponent(encodedTitle);
    const shelf = document.getElementById('manga-shelf-view');
    const reader = document.getElementById('manga-reader-view');
    if (shelf && reader) {
      shelf.style.display = 'none';
      reader.style.display = 'block';
    }
    this.loadManga(relPath, title);
  },

  closeMangaBook() {
    const shelf = document.getElementById('manga-shelf-view');
    const reader = document.getElementById('manga-reader-view');
    if (shelf && reader) {
      shelf.style.display = 'block';
      reader.style.display = 'none';
    }
  },

  async loadManga(relPath, title) {
    const fullRelPath = relPath.includes(':') || relPath.startsWith('media_library/manga/') ? relPath : 'media_library/manga/' + relPath;
    this.currentMangaId = fullRelPath;
    const box = document.getElementById('manga-waterfall-box');
    const badge = document.getElementById('manga-title-badge');
    badge.textContent = title || relPath;
    box.innerHTML = '<div style="color:var(--text-muted); text-align:center; padding-top:100px;">正在提取 CBZ 页面流…</div>';

    // 历史阅读进度
    let savedRatio = 0;
    try {
      const progRes = await fetch(`/api/media/manga/progress?manga_id=${encodeURIComponent(fullRelPath)}`);
      if (progRes.ok) {
        const prog = await progRes.json();
        if (prog && prog.scroll_offset_ratio) {
          savedRatio = prog.scroll_offset_ratio;
        }
      }
    } catch (_) {}

    // 获取 CBZ 内真实图片条目列表
    try {
      const pagesRes = await fetch(`/api/media/manga/pages?path=${encodeURIComponent(fullRelPath)}`);
      const entries = await pagesRes.json();
      if (!Array.isArray(entries) || entries.length === 0) {
        box.innerHTML = '<div style="color:var(--danger); text-align:center; padding-top:100px;">CBZ 包内未发现有效图片页面</div>';
        return;
      }

      box.innerHTML = entries.map((entry, idx) => `
        <img src="/api/media/manga/page?path=${encodeURIComponent(fullRelPath)}&entry=${encodeURIComponent(entry)}" loading="lazy" alt="Page ${idx + 1}">
      `).join('');

      if (savedRatio > 0) {
        setTimeout(() => {
          box.scrollTop = savedRatio * (box.scrollHeight - box.clientHeight);
        }, 300);
      }
    } catch (e) {
 if (e.name === 'AbortError') return;
      box.innerHTML = `<div style="color:var(--danger); text-align:center; padding-top:100px;">读取 CBZ 失败: ${e.message}</div>`;
    }
  },

  async saveMangaProgress(mangaId, ratio) {
    try {
      await fetch('/api/media/manga/progress', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          manga_id: mangaId,
          chapter_index: 0,
          page_index: 0,
          scroll_offset_ratio: ratio,
        })
      });
    } catch (e) {
 if (e.name === 'AbortError') return;
      console.warn('Failed to save manga progress', e);
    }
  },

  showAudioBar() { const bar = document.getElementById('audio-bar'); if (bar) bar.style.display = 'flex'; },

  // --- 全格式音频底栏 ---
  initAudioBar() {
    const player = new Audio(); this.audioPlayer = player;
    this.audioLoggedIn = false; this.audioSequence = 0; this.audioRestoring = false;
    this.audioLastSaved = 0;
    fetch('/api/auth/status').then(r => r.json()).then(status => {
      if (this.audioPlayer === player) this.audioLoggedIn = status.user?.id > 0;
    }).catch(() => { if (this.audioPlayer === player) this.audioLoggedIn = false; });
    const options = { signal: lifetime.signal };
    const playBtn = document.getElementById('audio-play-pause');
    const slider = document.getElementById('audio-progress');
    const timeLbl = document.getElementById('audio-time');
    playBtn.addEventListener('click', () => {
      if (player.paused) this.startAudioPlayback(player, this.audioSequence);
      else player.pause();
    }, options);
    player.addEventListener('timeupdate', () => {
      if (this.audioPlayer !== player || !Number.isFinite(player.duration) || player.duration <= 0) return;
      slider.value = (player.currentTime / player.duration) * 100;
      timeLbl.textContent = this.fmtTime(player.currentTime) + ' / ' + this.fmtTime(player.duration);
    }, options);
    slider.addEventListener('change', () => {
      if (Number.isFinite(player.duration) && player.duration > 0) {
        this.audioUserSeeked = true;
        player.currentTime = (slider.value / 100) * player.duration;
        this.saveAudioProgress(true);
      }
    }, options);
    player.addEventListener('playing', () => {
      playBtn.textContent = '⏸';
      if (this.audioTimer) clearInterval(this.audioTimer);
      this.audioTimer = setInterval(() => this.saveAudioProgress(), 30000);
    }, options);
    const paused = () => {
      playBtn.textContent = '▶';
      if (this.audioTimer) clearInterval(this.audioTimer);
      this.audioTimer = null;
      this.saveAudioProgress(true);
    };
    player.addEventListener('pause', paused, options);
    player.addEventListener('ended', paused, options);
    player.addEventListener('error', () => this.audioError('当前浏览器无法解码此音频'), options);
  },

  audioError(message) {
    const error = document.getElementById('audio-error');
    if (error) error.textContent = message;
  },

  saveAudioProgress(force = false, keepalive = false) {
    const player = this.audioPlayer;
    if (!this.audioLoggedIn || !player || !this.currentAudioId || this.audioRestoring || !Number.isFinite(player.currentTime)) return;
    const now = Date.now();
    if (!force && now - this.audioLastSaved < 30000) return;
    this.audioLastSaved = now;
    const id = this.currentAudioId;
    const options = { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ audio_id: id, offset: player.currentTime }) };
    const pending = keepalive ? globalThis.fetch('/api/media/audio/progress', { ...options, keepalive: true, credentials: 'same-origin' }) : fetch('/api/media/audio/progress', options);
    pending.then(response => { if (!response.ok) throw new Error('断点保存失败'); }).catch(error => {
      if (!keepalive && error.name !== 'AbortError' && this.currentAudioId === id) this.audioError(error.message);
    });
  },

  startAudioPlayback(player, sequence) {
    player.play().catch(error => {
      if (this.audioPlayer === player && this.audioSequence === sequence) this.audioError('无法播放音频：' + error.message);
    });
  },

  playAudio(path, title, album) {
    this.saveAudioProgress(true);
    const player = this.audioPlayer;
    if (!player) return;
    const sequence = ++this.audioSequence;
    this.audioRestoring = true; this.audioUserSeeked = false; this.audioLastSaved = 0;
    this.audioError('');
    document.getElementById('audio-bar').style.display = 'flex';
    this.currentAudioId = path;
    document.getElementById('audio-title').textContent = title || path;
    document.getElementById('audio-album').textContent = album || '专辑';
    player.onloadedmetadata = null;
    player.src = '/api/media/audio/stream?path=' + encodeURIComponent(path);
    this.startAudioPlayback(player, sequence);
    const current = () => this.audioPlayer === player && this.audioSequence === sequence;
    fetch('/api/media/audio/progress?audio_id=' + encodeURIComponent(path)).then(r => r.json()).then(data => {
      if (!current()) return;
      const restore = () => {
        if (!current()) return;
        if (!this.audioUserSeeked && Number.isFinite(data.offset) && data.offset > 0 && Number.isFinite(player.duration)) player.currentTime = Math.min(data.offset, Math.max(0, player.duration - 0.5));
        this.audioRestoring = false;
      };
      if (player.readyState >= 1) restore(); else player.onloadedmetadata = restore;
    }).catch(error => {
      if (current() && error.name !== 'AbortError') { this.audioRestoring = false; this.audioError('无法恢复断点：' + error.message); }
    });
  },

  fmtTime(sec) {
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  }
};

globalThis.MediaHub = MediaHub;
export default MediaHub;

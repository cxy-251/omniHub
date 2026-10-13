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
  dispose() { lifetime.abort();
    this.pauseMatrixVideos();
    if (this.audioTimer) clearInterval(this.audioTimer);
    if (this.mangaScrollTimer) clearTimeout(this.mangaScrollTimer);
    if (this.galleryScrollHandler) window.removeEventListener('scroll', this.galleryScrollHandler);
    this.scrollBound = false;
    this.audioTimer = null;
    if (this.audioPlayer) { this.audioPlayer.pause(); this.audioPlayer.removeAttribute('src'); this.audioPlayer.load(); this.audioPlayer = null; }
    this.currentAudioId = '';
    this.galleryState = { offset: 0, limit: 48, hasMore: true, isLoading: false, initialized: false };
    this.currentSubtab = 'gallery';
    document.querySelectorAll('video').forEach(video => { video.pause(); video.removeAttribute('src'); video.load(); });
  },
  currentSubtab: 'gallery',
  audioPlayer: null,
  currentAudioId: '',
  audioTimer: null,
  mangaScrollTimer: null,
  currentMangaId: '',

  galleryState: {
    offset: 0,
    limit: 48,
    hasMore: true,
    isLoading: false,
    initialized: false
  },

  init() {
    lifetime = new AbortController();
    this.renderContainer();
    this.bindEvents();
    this.initAudioBar();
  },

  activate() {
    if (this.currentSubtab === 'gallery' && !this.galleryState.initialized) {
      this.loadGallery(true);
    }
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
      </div>

      <!-- 相册画廊 -->
      <div id="subpane-gallery" class="media-subpane active">
        <div class="gallery-controls" style="margin-bottom: 12px; display: flex; gap: 8px;">
          <button id="btn-gallery-refresh" class="btn-primary">🔄 刷新相册</button>
          <label style="display:flex; align-items:center; gap:4px; font-size:13px;">
            <input type="checkbox" id="gallery-include-trash"> 查看废纸篓
          </label>
        </div>
        <div id="gallery-list" class="gallery-grid"></div>
      </div>

      <!-- 短视频多联放映矩阵 (第 11.1 节) -->
      <div id="subpane-shortvideo" class="media-subpane">
        <div class="matrix-toolbar">
          <label style="display:flex; align-items:center; gap:6px; font-size:13px;">
            <span>分屏布局:</span>
            <select id="matrix-layout-select">
              <option value="2">2 屏并排</option>
              <option value="3" selected>3 屏并排 (Deck宽屏)</option>
            </select>
          </label>
          <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
            <input type="checkbox" id="matrix-focus-audio" checked>
            <span>🎯 焦点出声 (点击视频独占出声)</span>
          </label>
          <button id="btn-matrix-random-all" class="btn-primary" style="padding:2px 10px;">🎲 全屏随机切片</button>
          <button id="btn-matrix-accompany" class="btn-primary" style="padding:2px 10px;" onclick="MediaHub.toggleAudioAccompany()">🎵 伴听音频</button>
        </div>
        <div id="matrix-slots-container" class="matrix-container matrix-grid-3"></div>
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

      <!-- 全格式音频浮动底栏 -->
      <div id="audio-bar" class="audio-player-bar" style="display:none;">
        <div class="audio-track-info">
          <span id="audio-title" class="audio-track-title">未播放音频</span>
          <span id="audio-album" class="audio-track-album">无专辑</span>
        </div>
        <div class="audio-controls">
          <button id="audio-play-pause" class="btn-primary" style="padding:4px 10px;">▶</button>
          <input type="range" id="audio-progress" min="0" max="100" value="0">
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

    // 画廊刷新
    const btnRef = document.getElementById('btn-gallery-refresh');
    if (btnRef) btnRef.addEventListener('click', () => this.loadGallery());
    const chkTrash = document.getElementById('gallery-include-trash');
    if (chkTrash) chkTrash.addEventListener('change', () => this.loadGallery());

    // 短视频多联放映矩阵事件 (第 11.1 节)
    const selectLayout = document.getElementById('matrix-layout-select');
    if (selectLayout) {
      selectLayout.addEventListener('change', (e) => {
        this.setMatrixLayout(parseInt(e.target.value, 10));
      });
    }

    const btnRandomAll = document.getElementById('btn-matrix-random-all');
    if (btnRandomAll) {
      btnRandomAll.addEventListener('click', () => {
        this.randomizeAllSlots();
      });
    }

    // 文本分页
    this.textState = { path: '', start: 0, limit: 100, hasMore: false };
    const btnTextLoad = document.getElementById('btn-text-load');
    if (btnTextLoad) {
      btnTextLoad.addEventListener('click', () => {
        this.textState.path = document.getElementById('text-path-input').value.trim();
        this.textState.start = 0;
        this.loadTextChunk();
      });
    }
    const btnTextPrev = document.getElementById('btn-text-prev');
    if (btnTextPrev) {
      btnTextPrev.addEventListener('click', () => {
        if (this.textState.start >= this.textState.limit) {
          this.textState.start -= this.textState.limit;
          this.loadTextChunk();
        }
      });
    }
    const btnTextNext = document.getElementById('btn-text-next');
    if (btnTextNext) {
      btnTextNext.addEventListener('click', () => {
        if (this.textState.hasMore) {
          this.textState.start += this.textState.limit;
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
    if (this.currentSubtab === 'shortvideo' && subId !== 'shortvideo') {
      this.pauseMatrixVideos();
    }
    this.currentSubtab = subId;
    document.querySelectorAll('.media-subnav button').forEach(b => {
      b.classList.toggle('active', b.getAttribute('data-sub') === subId);
    });
    document.querySelectorAll('.media-subpane').forEach(p => {
      p.classList.toggle('active', p.id === `subpane-${subId}`);
    });
    if (subId === 'gallery') this.loadGallery(true);
    if (subId === 'shortvideo') this.initMatrix();
    if (subId === 'manga') this.loadMangaShelf();
    if (subId === 'text') this.loadTextList();
  },

  pauseMatrixVideos() {
    const container = document.getElementById('matrix-slots-container');
    if (container) {
      container.querySelectorAll('video').forEach(v => v.pause());
    }
  },

  // --- 短视频多联放映矩阵实现 (第 11.1 节) ---
  matrixState: {
    slotCount: 3,
    channels: []
  },

  async initMatrix() {
    if (this.matrixState.channels.length === 0) {
      try {
        const res = await fetch('/api/media/shortvideo/channels');
        this.matrixState.channels = await res.json();
      } catch (_) {
        this.matrixState.channels = ['快手', '抖音', 'TikTok'];
      }
    }
    this.renderMatrixSlots();
  },

  setMatrixLayout(count) {
    this.matrixState.slotCount = count;
    const container = document.getElementById('matrix-slots-container');
    if (container) {
      container.className = `matrix-container matrix-grid-${count}`;
    }
    this.renderMatrixSlots();
  },

  renderMatrixSlots() {
    const container = document.getElementById('matrix-slots-container');
    if (!container) return;
    const count = this.matrixState.slotCount;
    const channels = this.matrixState.channels.length > 0 ? this.matrixState.channels : ['快手', '抖音', 'TikTok'];

    container.innerHTML = Array.from({ length: count }).map((_, i) => {
      const defaultChan = channels[i % channels.length] || channels[0];
      return `
        <div class="matrix-slot ${i === 0 ? 'focus-active' : ''}" id="matrix-slot-${i}" onclick="MediaHub.focusSlot(${i})">
          <div class="matrix-slot-header">
            <select id="slot-chan-${i}" onchange="MediaHub.changeSlotChannel(${i}, this.value)" onclick="event.stopPropagation()">
              ${channels.map(c => `<option value="${c}" ${c === defaultChan ? 'selected' : ''}>${c}</option>`).join('')}
            </select>
            <button class="btn-primary" id="slot-play-btn-${i}" style="padding:2px 8px;" onclick="event.stopPropagation(); MediaHub.toggleSlotPause(${i})">⏸️</button>
            <button class="btn-primary" style="padding:2px 8px;" onclick="event.stopPropagation(); MediaHub.randomSlotVideo(${i})">🎲 换片</button>
          </div>
          <video id="slot-video-${i}" autoplay loop ${i === 0 ? '' : 'muted'} playsinline></video>
        </div>
      `;
    }).join('');

    for (let i = 0; i < count; i++) {
      const defaultChan = channels[i % channels.length] || channels[0];
      this.randomSlotVideo(i, defaultChan);
    }
  },

  async randomSlotVideo(slotIndex, channel) {
    const chan = channel || document.getElementById(`slot-chan-${slotIndex}`)?.value;
    if (!chan) return;
    const v = document.getElementById(`slot-video-${slotIndex}`);
    if (!v) return;

    try {
      const res = await fetch(`/api/media/shortvideo/random?channel=${encodeURIComponent(chan)}`);
      if (res.ok) {
        const data = await res.json();
        v.src = `/api/media/shortvideo/stream?path=${encodeURIComponent(data.path)}`;
        v.play().catch(() => {});
      }
    } catch (_) {}
  },

  changeSlotChannel(slotIndex, channel) {
    this.randomSlotVideo(slotIndex, channel);
  },

  randomizeAllSlots() {
    for (let i = 0; i < this.matrixState.slotCount; i++) {
      this.randomSlotVideo(i);
    }
  },

  focusSlot(slotIndex) {
    const focusAudio = document.getElementById('matrix-focus-audio')?.checked;
    const count = this.matrixState.slotCount;

    for (let i = 0; i < count; i++) {
      const slotEl = document.getElementById(`matrix-slot-${i}`);
      const videoEl = document.getElementById(`slot-video-${i}`);
      if (!slotEl || !videoEl) continue;

      if (i === slotIndex) {
        slotEl.classList.add('focus-active');
        if (focusAudio) videoEl.muted = false;
      } else {
        slotEl.classList.remove('focus-active');
        if (focusAudio) videoEl.muted = true;
      }
    }
  },

  toggleSlotPause(slotIndex) {
    const v = document.getElementById(`slot-video-${slotIndex}`);
    const btn = document.getElementById(`slot-play-btn-${slotIndex}`);
    if (!v) return;
    if (v.paused) {
      v.play();
      if (btn) btn.textContent = '⏸️';
    } else {
      v.pause();
      if (btn) btn.textContent = '▶️';
    }
  },

  toggleAudioAccompany() {
    const audioBar = document.getElementById('audio-bar');
    if (!audioBar) return;
    const isHidden = audioBar.style.display === 'none';
    audioBar.style.display = isHidden ? 'flex' : 'none';
    if (isHidden) {
      const container = document.getElementById('matrix-slots-container');
      if (container) {
        container.querySelectorAll('video').forEach(v => v.muted = true);
      }
    }
  },

  // --- 画廊实现 (严格 48 张分页与追加加载) ---
  async loadGallery(reset = false) {
    const listEl = document.getElementById('gallery-list');
    if (!listEl) return;

    if (reset) {
      this.galleryState.offset = 0;
      this.galleryState.hasMore = true;
      this.galleryState.initialized = true;
      listEl.innerHTML = '';
      this.bindGalleryScroll();
    }

    if (this.galleryState.isLoading || !this.galleryState.hasMore) return;
    this.galleryState.isLoading = true;

    const incTrash = document.getElementById('gallery-include-trash')?.checked ? '1' : '0';
    try {
      const res = await fetch(`/api/media/gallery/list?include_deleted=${incTrash}&limit=${this.galleryState.limit}&offset=${this.galleryState.offset}`);
      const photos = await res.json();
      this.galleryState.isLoading = false;

      if (!Array.isArray(photos) || photos.length === 0) {
        this.galleryState.hasMore = false;
        if (this.galleryState.offset === 0) {
          listEl.innerHTML = '<div style="color:var(--text-muted)">相册暂无照片</div>';
        }
        return;
      }

      this.galleryState.offset += photos.length;
      if (photos.length < this.galleryState.limit) {
        this.galleryState.hasMore = false;
      }

      const html = photos.map(p => `
        <div class="gallery-item" style="aspect-ratio: ${p.width || 4} / ${p.height || 3}; background-color: ${p.color_hex || 'var(--surface)'};" onclick="MediaHub.viewOriginal('${p.id}', '${p.volume}')">
          <img src="/api/media/gallery/thumbnail?id=${p.id}" loading="lazy" alt="${p.filename}">
          <div class="overlay-info">
            <span>${p.filename}</span>
            <span>${(p.size_bytes / 1024).toFixed(0)} KB</span>
          </div>
        </div>
      `).join('');
      listEl.insertAdjacentHTML('beforeend', html);
    } catch (e) {
 if (e.name === 'AbortError') return;
      this.galleryState.isLoading = false;
      if (this.galleryState.offset === 0) {
        listEl.innerHTML = `<div style="color:var(--danger)">加载失败: ${e.message}</div>`;
      }
    }
  },

  bindGalleryScroll() {
    if (this.scrollBound) return;
    this.scrollBound = true;
    this.galleryScrollHandler = () => {
      if (this.currentSubtab !== 'gallery') return;
      if (window.innerHeight + window.scrollY >= document.body.offsetHeight - 500) {
        this.loadGallery(false);
      }
    };
    window.addEventListener('scroll', this.galleryScrollHandler, { passive: true });
  },

  viewOriginal(id, vol) {
    window.open(`/api/media/gallery/original?id=${id}&vol=${vol}`, '_blank');
  },

  // --- 文本阅读器实现 ---
  async loadTextChunk() {
    const contentEl = document.getElementById('reader-content');
    const pageInfo = document.getElementById('text-page-info');
    if (!this.textState.path) return;

    contentEl.textContent = '加载中…';
    try {
      const res = await fetch(`/api/media/text/chunk?path=${encodeURIComponent(this.textState.path)}&start=${this.textState.start}&limit=${this.textState.limit}`);
      const data = await res.json();

      this.textState.hasMore = data.has_more;
      pageInfo.textContent = `行号: ${data.start_line + 1} - ${data.start_line + data.lines.length} (总共: ${data.total_lines})`;

      // 使用离线 marked.min.js 进行富文本与图表排版 (第 6.1 节)
      const rawText = data.lines.join('\n');
      if (this.textState.path.toLowerCase().endsWith('.md')) {
        contentEl.innerHTML = await renderMarkdown(rawText);
      } else {
        contentEl.innerHTML = data.lines.map(line => `<div>${line.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;") || '&nbsp;'}</div>`).join('');
      }
    } catch (e) {
 if (e.name === 'AbortError') return;
      contentEl.textContent = `加载错误: ${e.message}`;
    }
  },

  async loadTextList() {
    const listEl = document.getElementById('text-file-list');
    if (!listEl) return;
    try {
      const res = await fetch('/api/media/text/list');
      const items = await res.json();
      if (!Array.isArray(items) || items.length === 0) {
        listEl.innerHTML = '<div style="color:var(--text-muted)">暂无可用文本</div>';
        return;
      }
      listEl.innerHTML = items.map(item => `
        <div style="cursor:pointer; padding:6px 8px; border-radius:4px; background:var(--background); overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" onclick="MediaHub.selectTextFile('${item.rel_path}')" title="${item.rel_path}">
          📄 ${item.name}
        </div>
      `).join('');
    } catch (e) {
 if (e.name === 'AbortError') return;
      listEl.innerHTML = `<div style="color:var(--danger)">加载失败: ${e.message}</div>`;
    }
  },

  selectTextFile(relPath) {
    const inp = document.getElementById('text-path-input');
    if (inp) inp.value = relPath;
    this.textState.path = relPath;
    this.textState.start = 0;
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

  // --- 全格式音频底栏 ---
  initAudioBar() {
    this.audioPlayer = new Audio();
    const bar = document.getElementById('audio-bar');
    const playBtn = document.getElementById('audio-play-pause');
    const slider = document.getElementById('audio-progress');
    const timeLbl = document.getElementById('audio-time');

    playBtn.addEventListener('click', () => {
      if (this.audioPlayer.paused) {
        this.audioPlayer.play();
        playBtn.textContent = '⏸';
      } else {
        this.audioPlayer.pause();
        playBtn.textContent = '▶';
      }
    });

    this.audioPlayer.addEventListener('timeupdate', () => {
      if (!this.audioPlayer.duration) return;
      slider.value = (this.audioPlayer.currentTime / this.audioPlayer.duration) * 100;
      timeLbl.textContent = `${this.fmtTime(this.audioPlayer.currentTime)} / ${this.fmtTime(this.audioPlayer.duration)}`;
    });

    slider.addEventListener('change', () => {
      if (this.audioPlayer.duration) {
        this.audioPlayer.currentTime = (slider.value / 100) * this.audioPlayer.duration;
      }
    });

    // 每 5 秒上报断点
    this.audioTimer = setInterval(() => {
      if (!this.audioPlayer.paused && this.currentAudioId) {
        fetch('/api/media/audio/progress', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            audio_id: this.currentAudioId,
            offset: this.audioPlayer.currentTime,
          })
        }).catch(() => {});
      }
    }, 5000);
  },

  playAudio(path, title, album) {
    const bar = document.getElementById('audio-bar');
    bar.style.display = 'flex';
    this.currentAudioId = path;
    document.getElementById('audio-title').textContent = title || path;
    document.getElementById('audio-album').textContent = album || '专辑';
    this.audioPlayer.src = `/api/media/audio/stream?path=${encodeURIComponent(path)}`;

    // 查询断点恢复
    fetch(`/api/media/audio/progress?audio_id=${encodeURIComponent(path)}`)
      .then(r => r.json())
      .then(d => {
        if (d && d.offset > 0) {
          this.audioPlayer.currentTime = d.offset;
        }
        this.audioPlayer.play();
        document.getElementById('audio-play-pause').textContent = '⏸';
      })
      .catch(() => {
        this.audioPlayer.play();
        document.getElementById('audio-play-pause').textContent = '⏸';
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

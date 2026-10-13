import { request } from '../src/api';
// omniHub Web 游戏专区交互逻辑 (仅展示与运行 web_only 游戏)
let lifetime = new AbortController();
const fetch = (input, options = {}) => request(input, { ...options, signal: lifetime.signal });
const GamesHub = {
  dispose() { lifetime.abort();
 const frame = document.getElementById('game-runner-frame'); if (frame) frame.src = 'about:blank';
  },
  initialized: false,
  allGames: [],
  currentCategory: 'ALL',

  init() {
    lifetime = new AbortController();
    this.renderContainer();
    this.bindEvents();
  },

  activate() {
    if (this.initialized) return;
    this.initialized = true;
    this.loadGames();
  },

  renderContainer() {
    const container = document.getElementById('games-container');
    if (!container) return;

    container.innerHTML = `
      <div class="games-subnav">
        <button class="active" data-cat="ALL">全部网页游戏</button>
        <button data-cat="rpg">RPG Maker (47款)</button>
        <button data-cat="slg">Web SLG (18款)</button>
        <button data-cat="retro">街机掌机 (70款)</button>
        <button data-cat="flash">Flash (2款)</button>
      </div>

      <div id="games-grid" class="games-grid">
        <div style="color:var(--text-muted)">正在加载网页与模拟器游戏…</div>
      </div>

      <!-- 内嵌运行器全屏容器 -->
      <div id="game-runner-modal" class="game-runner-modal">
        <div class="game-runner-header">
          <span id="game-runner-title" style="font-weight:600; color:#fff;">正在运行游戏</span>
          <div>
            <button id="btn-game-fullscreen" class="btn-primary" style="padding:2px 8px; margin-right:8px;">全屏</button>
            <button id="btn-game-close" class="btn-primary" style="background-color:var(--danger); padding:2px 8px;">退出</button>
          </div>
        </div>
        <iframe id="game-runner-frame" class="game-runner-frame" allow="autoplay; gamepad; keyboard" src="about:blank"></iframe>
      </div>
    `;
  },

  bindEvents() {
    document.querySelectorAll('.games-subnav button').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const cat = e.target.getAttribute('data-cat');
        this.currentCategory = cat;
        document.querySelectorAll('.games-subnav button').forEach(b => b.classList.remove('active'));
        e.target.classList.add('active');
        this.renderGamesList();
      });
    });

    const btnClose = document.getElementById('btn-game-close');
    if (btnClose) {
      btnClose.addEventListener('click', () => {
        const modal = document.getElementById('game-runner-modal');
        const frame = document.getElementById('game-runner-frame');
        frame.src = 'about:blank';
        modal.style.display = 'none';
      });
    }

    const btnFs = document.getElementById('btn-game-fullscreen');
    if (btnFs) {
      btnFs.addEventListener('click', () => {
        const frame = document.getElementById('game-runner-frame');
        if (frame.requestFullscreen) frame.requestFullscreen();
      });
    }
  },

  async loadGames() {
    const grid = document.getElementById('games-grid');
    if (!grid) return;

    try {
      // 严格仅拉取 target=web_only 游戏
      const res = await fetch('/api/games/list?target=web_only');
      const data = await res.json();
      this.allGames = Array.isArray(data) ? data : [];
      this.renderGamesList();
    } catch (e) {
 if (e.name === 'AbortError') return;
      grid.innerHTML = `<div style="color:var(--danger)">加载游戏失败: ${e.message}</div>`;
    }
  },

  renderGamesList() {
    const grid = document.getElementById('games-grid');
    if (!grid) return;

    let filtered = this.allGames;
    if (this.currentCategory !== 'ALL') {
      filtered = this.allGames.filter(g => g.category === this.currentCategory);
    }

    if (filtered.length === 0) {
      grid.innerHTML = '<div style="color:var(--text-muted)">当前分类暂无游戏</div>';
      return;
    }

    grid.innerHTML = filtered.map(g => `
      <div class="game-card" onclick="GamesHub.launchGame('${g.id}', '${g.category}', '${this.escapeAttr(g.name)}')">
        <div>
          <div class="game-card-title" title="${this.escapeAttr(g.name)}">${this.escapeHTML(g.name)}</div>
          <div class="game-card-meta">
            <span>编号: ${g.id}</span>
            <span class="badge badge-muted">${g.category}</span>
          </div>
        </div>
        <button class="btn-primary" style="width:100%; margin-top:8px;">▶ 启动</button>
      </div>
    `).join('');
  },

  launchGame(id, category, name) {
    const modal = document.getElementById('game-runner-modal');
    const title = document.getElementById('game-runner-title');
    const frame = document.getElementById('game-runner-frame');

    title.textContent = `[${category.toUpperCase()}] ${name}`;
    modal.style.display = 'flex';

    // 映射到对应的 Web 模拟器或运行器
    if (category === 'flash') {
      frame.src = `/players/flash.html?id=${encodeURIComponent(id)}`;
    } else if (category === 'retro') {
      frame.src = `/players/retro.html?id=${encodeURIComponent(id)}`;
    } else {
      frame.src = `/standalone_games/${category}_games/${encodeURIComponent(id)}/index.html`;
    }
  },

  escapeHTML(str) {
    return (str || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  },

  escapeAttr(str) {
    return (str || '').replace(/"/g, '&quot;');
  }
};

globalThis.GamesHub = GamesHub;
export default GamesHub;

import { request } from '../src/api';
// omniHub 媒体专区交互逻辑
let lifetime = new AbortController();
const fetch = (input, options = {}) => request(input, { ...options, signal: lifetime.signal });
const MediaHub = {
  dispose() { this.saveAudioProgress(true, true); lifetime.abort();
    if (this.audioTimer) clearInterval(this.audioTimer);
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
  },

  showAudioBar() { const bar = document.getElementById('audio-bar'); if (bar) bar.style.display = 'flex'; },

  // --- 全格式音频底栏 ---
  initAudioBar() {
    const player = new Audio(); this.audioPlayer = player;
    this.audioUserID = 0; this.audioSequence = 0; this.audioRestoring = false;
    this.audioLastSaved = 0;
    fetch('/api/auth/status').then(r => r.json()).then(status => {
      if (this.audioPlayer === player) this.audioUserID = status.user?.id || 0;
    }).catch(() => { if (this.audioPlayer === player) this.audioUserID = 0; });
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
    if (!this.audioUserID || !player || !this.currentAudioId || this.audioRestoring || !Number.isFinite(player.currentTime)) return;
    const now = Date.now();
    if (!force && now - this.audioLastSaved < 30000) return;
    this.audioLastSaved = now;
    const id = this.currentAudioId;
    const options = { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Omni-Account-ID': String(this.audioUserID) }, body: JSON.stringify({ audio_id: id, offset: player.currentTime }) };
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

  updateAudioLabels(path, title, album) {
    if (this.currentAudioId !== path || !this.audioPlayer) return;
    document.getElementById('audio-title').textContent = title;
    document.getElementById('audio-album').textContent = album;
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

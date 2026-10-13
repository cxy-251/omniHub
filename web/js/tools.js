// omniHub 局域网小工具交互逻辑
const ToolsHub = {
  initialized: false,
  init() {
    this.renderContainer();
    this.bindEvents();
  },

  activate() {
    this.loadClipboard();
    this.loadMetrics();
    this.loadSharedFiles();
    this.initWebShell();
    this.loadMegaStatus();
    if (!this.metricsTimer) {
      this.metricsTimer = setInterval(() => this.loadMetrics(), 3000);
    }
  },

  deactivate() {
    if (this.metricsTimer) {
      clearInterval(this.metricsTimer);
      this.metricsTimer = null;
    }
    if (this.megaTimer) {
      clearInterval(this.megaTimer);
      this.megaTimer = null;
    }
  },

  renderContainer() {
    const container = document.getElementById('tools-container');
    if (!container) return;

    container.innerHTML = `
      <div class="tools-grid">
        <!-- 剪贴板 -->
        <div class="tool-card">
          <h3>📋 跨设备双向剪贴板</h3>
          <div style="display:flex; gap:6px;">
            <input type="text" id="clip-input" placeholder="输入要投递的文字或链接" style="flex-grow:1;">
            <button id="btn-clip-send" class="btn-primary">发送</button>
          </div>
          <div id="clip-list" class="clipboard-list">
            <div style="color:var(--text-muted)">正在加载剪贴板…</div>
          </div>
        </div>

        <!-- 共享文件 -->
        <div class="tool-card">
          <h3>📁 局域网共享文件 (media_library/shared/)</h3>
          <div style="display:flex; gap:6px;">
            <input type="file" id="shared-file-input" style="flex-grow:1;">
            <button id="btn-shared-upload" class="btn-primary">分块上传</button>
          </div>
          <div style="display:flex; justify-content:space-between; align-items:center;">
            <button id="btn-shared-zip" class="btn-primary" style="padding:2px 8px;">📦 打包下载全部 (ZIP)</button>
            <button id="btn-shared-refresh" class="btn-primary" style="padding:2px 8px; background-color:var(--border);">🔄 刷新</button>
          </div>
          <div id="shared-file-list" class="clipboard-list" style="max-height:120px;"></div>
        </div>

        <!-- 硬件监控看板 -->
        <div class="tool-card">
          <h3>📊 只读硬件监控看板</h3>
          <div class="metrics-box">
            <div class="metrics-item">
              <div>CPU 占用</div>
              <div id="metric-cpu" class="metrics-val">0%</div>
            </div>
            <div class="metrics-item">
              <div>可用内存</div>
              <div id="metric-mem" class="metrics-val">0 MB</div>
            </div>
            <div class="metrics-item">
              <div>磁盘可用</div>
              <div id="metric-disk" class="metrics-val">0 GB</div>
            </div>
            <div class="metrics-item">
              <div>网络下载/上传</div>
              <div id="metric-net" class="metrics-val">0 / 0 KB/s</div>
            </div>
          </div>
        </div>

        <!-- 网络诊断与 WOL -->
        <div class="tool-card">
          <h3>🌐 网络测速、唤醒与局域网扫描 (第 11.3 节)</h3>
          <div style="display:flex; gap:6px; align-items:center;">
            <button id="btn-speed-test" class="btn-primary">🚀 开始测速</button>
            <span id="speed-result" style="font-size:13px; color:var(--text-muted);">-</span>
            <button id="btn-lan-scan" class="btn-primary" style="margin-left:auto;">🔍 扫描局域网</button>
          </div>
          <div style="display:flex; gap:6px; align-items:center; margin-top:8px;">
            <input type="text" id="wol-mac-input" placeholder="目标 PC MAC 地址 (如 AA:BB:CC:DD:EE:FF)" style="flex-grow:1;">
            <button id="btn-wol-send" class="btn-primary">唤醒</button>
          </div>
          <div id="lan-devices-list" class="clipboard-list" style="max-height:90px; margin-top:8px; display:none;"></div>
        </div>

        <!-- MEGA 客户端面板 (第 10.2 节) -->
        <div class="tool-card">
          <h3>☁️ MEGA 客户端与配额避让</h3>
          <div style="display:flex; flex-direction:column; gap:6px;">
            <input type="text" id="mega-url-input" placeholder="MEGA 分享链接 (mega.nz/...)" style="width:100%;">
            <div style="display:flex; gap:6px;">
              <input type="text" id="mega-path-input" placeholder="自定义保存路径 (默认 ~/Downloads)" style="flex-grow:1;">
              <button id="btn-mega-add" class="btn-primary">添加下载</button>
            </div>
          </div>
          <div id="mega-quota-warning" style="display:none; color:#f85149; font-size:12px; background:rgba(248,81,73,0.1); padding:4px 8px; border-radius:4px; margin-top:4px;">
            ⚠️ 配额超限避让中: 任务挂起，等待恢复倒计时
          </div>
          <div id="mega-tasks-list" class="clipboard-list" style="max-height:110px; margin-top:4px;">
            <div style="color:var(--text-muted)">暂无活动 MEGA 任务</div>
          </div>
        </div>

        <!-- WebShell 终端 (第 11.4 节 - xterm.js 离线集成) -->
        <div class="tool-card" style="grid-column: 1 / -1;">
          <h3>💻 应急 WebShell 终端 (仅 Admin 可用，基于 xterm.js)</h3>
          <div id="webshell-terminal" style="padding: 8px; background: #0d1117; border-radius: 6px; border: 1px solid var(--border); min-height: 180px;"></div>
          <div style="display:flex; gap:6px; margin-top:8px;">
            <input type="text" id="webshell-input" placeholder="输入命令 (如 uptime, uname -a)" style="flex-grow:1; font-family:monospace;">
            <button id="btn-webshell-exec" class="btn-primary">执行</button>
          </div>
        </div>
      </div>
    `;
  },

  bindEvents() {
    // 剪贴板
    const btnClip = document.getElementById('btn-clip-send');
    if (btnClip) {
      btnClip.addEventListener('click', () => {
        const inp = document.getElementById('clip-input');
        if (inp.value.trim()) {
          this.sendClipboard(inp.value.trim());
          inp.value = '';
        }
      });
    }

    // 共享文件刷新与打包
    const btnRefreshFiles = document.getElementById('btn-shared-refresh');
    if (btnRefreshFiles) btnRefreshFiles.addEventListener('click', () => this.loadSharedFiles());
    const btnZip = document.getElementById('btn-shared-zip');
    if (btnZip) btnZip.addEventListener('click', () => window.open('/api/tools/shared/zip', '_blank'));

    // 分块上传
    const btnUpload = document.getElementById('btn-shared-upload');
    if (btnUpload) btnUpload.addEventListener('click', () => this.uploadSharedFile());

    // 测速
    const btnSpeed = document.getElementById('btn-speed-test');
    if (btnSpeed) btnSpeed.addEventListener('click', () => this.runSpeedTest());

    // WOL 与 LAN 扫描
    const btnWOL = document.getElementById('btn-wol-send');
    if (btnWOL) {
      btnWOL.addEventListener('click', () => {
        const mac = document.getElementById('wol-mac-input').value.trim();
        if (mac) this.sendWOL(mac);
      });
    }
    const btnLANScan = document.getElementById('btn-lan-scan');
    if (btnLANScan) {
      btnLANScan.addEventListener('click', () => this.scanLAN());
    }

    // MEGA 下载添加
    const btnMega = document.getElementById('btn-mega-add');
    if (btnMega) {
      btnMega.addEventListener('click', () => {
        const inp = document.getElementById('mega-url-input');
        const pathInp = document.getElementById('mega-path-input');
        if (inp.value.trim()) {
          this.addMegaDownload(inp.value.trim(), pathInp ? pathInp.value.trim() : '');
          inp.value = '';
        }
      });
    }

    // WebShell
    const btnShell = document.getElementById('btn-webshell-exec');
    const shellInput = document.getElementById('webshell-input');
    if (btnShell && shellInput) {
      btnShell.addEventListener('click', () => {
        const cmd = shellInput.value.trim();
        if (cmd) {
          this.execShell(cmd);
          shellInput.value = '';
        }
      });
      shellInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          const cmd = shellInput.value.trim();
          if (cmd) {
            this.execShell(cmd);
            shellInput.value = '';
          }
        }
      });
    }
  },

  // --- 剪贴板 ---
  async loadClipboard() {
    const list = document.getElementById('clip-list');
    if (!list) return;

    try {
      const res = await fetch('/api/tools/clipboard');
      const items = await res.json();
      if (!Array.isArray(items) || items.length === 0) {
        list.innerHTML = '<div style="color:var(--text-muted)">剪贴板为空</div>';
        return;
      }

      list.innerHTML = items.map(it => `
        <div class="clipboard-item">
          <span>${this.escapeHTML(it.content)}</span>
          <button class="btn-primary" style="padding:2px 6px;" onclick="ToolsHub.copyText('${this.escapeHTML(it.content)}')">复制</button>
        </div>
      `).join('');
    } catch (_) {}
  },

  async sendClipboard(text) {
    try {
      await fetch('/api/tools/clipboard', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: text })
      });
      this.loadClipboard();
    } catch (_) {}
  },

  copyText(txt) {
    navigator.clipboard.writeText(txt).then(() => alert('已复制到系统剪贴板'));
  },

  // --- 共享文件 ---
  async loadSharedFiles() {
    const list = document.getElementById('shared-file-list');
    if (!list) return;

    try {
      const res = await fetch('/api/tools/shared/files');
      const files = await res.json();
      if (!Array.isArray(files) || files.length === 0) {
        list.innerHTML = '<div style="color:var(--text-muted)">暂无共享文件</div>';
        return;
      }

      list.innerHTML = files.map(f => `
        <div class="clipboard-item">
          <span>${this.escapeHTML(f.filename)}</span>
          <span style="color:var(--text-muted); font-size:11px;">${(f.size / 1024).toFixed(1)} KB</span>
        </div>
      `).join('');
    } catch (_) {}
  },

  async uploadSharedFile() {
    const fileInput = document.getElementById('shared-file-input');
    if (!fileInput || !fileInput.files || fileInput.files.length === 0) return;

    const file = fileInput.files[0];
    const chunkSize = 1024 * 512; // 512KB 分块
    const totalChunks = Math.ceil(file.size / chunkSize);

    for (let i = 0; i < totalChunks; i++) {
      const chunk = file.slice(i * chunkSize, Math.min(file.size, (i + 1) * chunkSize));
      const res = await fetch(`/api/tools/shared/upload?filename=${encodeURIComponent(file.name)}&chunk_index=${i}`, {
        method: 'POST',
        body: chunk
      });
      if (!res.ok) {
        alert('分块上传失败');
        return;
      }
    }

    alert('文件上传成功！');
    fileInput.value = '';
    this.loadSharedFiles();
  },

  // --- 硬件看板 ---
  async loadMetrics() {
    try {
      const res = await fetch('/api/tools/metrics');
      const m = await res.json();
      document.getElementById('metric-cpu').textContent = `${m.cpu_percent.toFixed(1)}%`;
      document.getElementById('metric-mem').textContent = `${(m.mem_available_bytes / 1024 / 1024).toFixed(0)} MB`;
      document.getElementById('metric-disk').textContent = `${(m.disk_free_bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
      document.getElementById('metric-net').textContent = `${m.net_rx_kbps.toFixed(0)} / ${m.net_tx_kbps.toFixed(0)} KB/s`;
    } catch (_) {}
  },

  // --- 网络测速与 WOL ---
  async runSpeedTest() {
    const resLbl = document.getElementById('speed-result');
    resLbl.textContent = '正在测速…';

    const start = performance.now();
    try {
      const res = await fetch('/api/tools/speed');
      const buf = await res.arrayBuffer();
      const elapsed = (performance.now() - start) / 1000;
      const mbps = (buf.byteLength * 8) / (elapsed * 1000 * 1000);
      resLbl.textContent = `下行速率: ${mbps.toFixed(2)} Mbps (${(buf.byteLength / 1024 / 1024).toFixed(1)}MB in ${elapsed.toFixed(1)}s)`;
    } catch (e) {
      resLbl.textContent = `测速失败: ${e.message}`;
    }
  },

  async sendWOL(mac) {
    try {
      const res = await fetch('/api/tools/wol', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mac })
      });
      if (res.ok) {
        alert('Magic Packet 唤醒包已发送');
      } else {
        alert('发送失败');
      }
    } catch (e) {
      alert(`异常: ${e.message}`);
    }
  },

  // --- WebShell (第 11.4 节 - xterm.js 原生挂载) ---
  term: null,

  initWebShell() {
    if (this.term) return;
    const termContainer = document.getElementById('webshell-terminal');
    if (!termContainer || !window.Terminal) return;

    this.term = new window.Terminal({
      cursorBlink: true,
      rows: 10,
      theme: { background: '#0d1117', foreground: '#58a6ff' }
    });
    this.term.open(termContainer);
    this.term.writeln('omniHub WebShell [受限应急调试模式 - xterm.js 已就绪]');
    this.term.write('$ ');
  },

  async execShell(cmd) {
    if (this.term) {
      this.term.writeln(`\r\n$ ${cmd}`);
    }
    try {
      const res = await fetch('/api/tools/webshell', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: cmd })
      });
      const data = await res.json();
      if (this.term) {
        const lines = (data.output || '').split('\n');
        for (const line of lines) {
          this.term.writeln(line);
        }
        this.term.write('$ ');
      }
    } catch (e) {
      if (this.term) {
        this.term.writeln(`执行异常: ${e.message}`);
        this.term.write('$ ');
      }
    }
  },

  // --- 局域网设备扫描 (第 11.3.1 节) ---
  async scanLAN() {
    const listEl = document.getElementById('lan-devices-list');
    if (!listEl) return;
    listEl.style.display = 'block';
    listEl.innerHTML = '<div style="color:var(--text-muted)">正在扫描局域网活跃设备…</div>';

    try {
      const res = await fetch('/api/tools/lanscan');
      const devices = await res.json();
      if (!Array.isArray(devices) || devices.length === 0) {
        listEl.innerHTML = '<div style="color:var(--text-muted)">未发现局域网活跃设备</div>';
        return;
      }
      listEl.innerHTML = devices.map(d => `
        <div class="clipboard-item" style="cursor:pointer;" onclick="document.getElementById('wol-mac-input').value = '${d.mac}'" title="点击填入 MAC 地址">
          <span>🖥️ ${this.escapeHTML(d.hostname || d.ip)} (${d.ip})</span>
          <span style="font-family:monospace; font-size:11px; color:var(--text-muted);">${d.mac} [${d.device}]</span>
        </div>
      `).join('');
    } catch (e) {
      listEl.innerHTML = `<div style="color:var(--danger)">扫描失败: ${e.message}</div>`;
    }
  },

  // --- MEGA 客户端 (第 10.2 节) ---
  async loadMegaStatus() {
    const list = document.getElementById('mega-tasks-list');
    const warning = document.getElementById('mega-quota-warning');
    if (!list) return;

    try {
      const res = await fetch('/api/mega/status');
      const data = await res.json();
      if (warning) {
        warning.style.display = (data.quota === 'exceeded' || data.quota === 'cooldown') ? 'block' : 'none';
      }

      if (!Array.isArray(data.tasks) || data.tasks.length === 0) {
        list.innerHTML = '<div style="color:var(--text-muted)">暂无活动 MEGA 任务</div>';
      } else {
        list.innerHTML = data.tasks.map(t => `
          <div class="clipboard-item">
            <span>${this.escapeHTML(t.id || t.source)}</span>
            <span style="font-size:11px; color:${t.status === 'downloading' ? '#58a6ff' : 'var(--text-muted)'}">${t.status} (${((t.progress || 0) * 100).toFixed(0)}%)</span>
          </div>
        `).join('');
      }

      if (!this.megaTimer) {
        this.megaTimer = setInterval(() => this.loadMegaStatus(), 3000);
      }
    } catch (_) {}
  },

  async addMegaDownload(url, destDir) {
    try {
      const payload = { source: url };
      if (destDir) payload.dest_dir = destDir;
      const res = await fetch('/api/mega/download', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        this.loadMegaStatus();
      } else {
        alert('添加 MEGA 下载失败');
      }
    } catch (_) {
      alert('网络异常');
    }
  },

  escapeHTML(str) {
    return (str || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
};

window.addEventListener('DOMContentLoaded', () => {
  ToolsHub.init();
});

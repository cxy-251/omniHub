import { request } from '../src/api';
// omniHub 任务调试台交互逻辑
let lifetime = new AbortController();
const fetch = (input, options = {}) => request(input, { ...options, signal: lifetime.signal });
const TasksHub = {
  dispose() { lifetime.abort();
 if (this.eventSource) { this.eventSource.close(); this.eventSource = null; }
 this.allLogs = []; this.bufferedLogs = [];
  },
  activeTaskId: null,
  eventSource: null,
  isPaused: false,
  filterLevel: 'ALL',
  regexFilter: null,
  allLogs: [],
  bufferedLogs: [],
  expandedContextId: null,

  init() {
    lifetime = new AbortController();
    this.renderContainer();
    this.bindEvents();
  },

  renderContainer() {
    const container = document.getElementById('tasks-container');
    if (!container) return;

    container.innerHTML = `
      <div class="task-console-layout">
        <div class="task-controls-bar">
          <input type="text" id="task-script-path" placeholder="脚本路径 (如 /bin/sh 或 features/manga/scripts/pack.py)" style="width:360px;">
          <input type="text" id="task-script-args" placeholder="参数 (空格分隔)" style="width:200px;">
          <button id="btn-task-start" class="btn-primary">▶ 启动任务</button>
          <button id="btn-task-stop" class="btn-primary" style="background-color:var(--danger);" disabled>⏹ 终止</button>
          <span id="task-status-badge" class="badge badge-muted">未运行</span>
        </div>

        <div class="log-viewport-container">
          <div class="viewport-toolbar">
            <div class="viewport-filters">
              <button class="level-badge-btn active" data-lvl="ALL">全部</button>
              <button class="level-badge-btn" data-lvl="INFO">INFO</button>
              <button class="level-badge-btn" data-lvl="WARN">WARN</button>
              <button class="level-badge-btn" data-lvl="ERROR">ERROR</button>
              <button class="level-badge-btn" data-lvl="DEBUG">DEBUG</button>
              <input type="text" id="log-regex-input" placeholder="输入正则实时过滤…" style="width:160px; padding:2px 6px;">
            </div>
            <div class="viewport-actions">
              <button id="btn-log-pause" class="btn-primary" style="padding:2px 8px;">⏸ 暂停</button>
              <button id="btn-log-clear" class="btn-primary" style="padding:2px 8px; background-color:var(--border);">🗑 清空</button>
            </div>
          </div>
          <div id="viewport-log-list" class="viewport-log-list"></div>
        </div>
      </div>
    `;
  },

  bindEvents() {
    const btnStart = document.getElementById('btn-task-start');
    const btnStop = document.getElementById('btn-task-stop');
    const btnPause = document.getElementById('btn-log-pause');
    const btnClear = document.getElementById('btn-log-clear');
    const regexInput = document.getElementById('log-regex-input');

    if (btnStart) btnStart.addEventListener('click', () => this.startTask());
    if (btnStop) btnStop.addEventListener('click', () => this.stopTask());

    if (btnPause) {
      btnPause.addEventListener('click', () => {
        this.isPaused = !this.isPaused;
        btnPause.textContent = this.isPaused ? '▶ 恢复' : '⏸ 暂停';
        if (!this.isPaused && this.bufferedLogs.length > 0) {
          this.bufferedLogs.forEach(e => this.appendLogEntry(e));
          this.bufferedLogs = [];
        }
      });
    }

    if (btnClear) {
      btnClear.addEventListener('click', () => {
        this.allLogs = [];
        this.bufferedLogs = [];
        document.getElementById('viewport-log-list').innerHTML = '';
      });
    }

    if (regexInput) {
      regexInput.addEventListener('input', (e) => {
        const val = e.target.value.trim();
        try {
          this.regexFilter = val ? new RegExp(val, 'i') : null;
        } catch (_) {
          this.regexFilter = null;
        }
        this.renderAllLogs();
      });
    }

    document.querySelectorAll('.level-badge-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        this.filterLevel = e.target.getAttribute('data-lvl');
        document.querySelectorAll('.level-badge-btn').forEach(b => b.classList.remove('active'));
        e.target.classList.add('active');
        this.renderAllLogs();
      });
    });
  },

  async startTask() {
    const script = document.getElementById('task-script-path').value.trim();
    const argsRaw = document.getElementById('task-script-args').value.trim();
    if (!script) return;

    const args = argsRaw ? argsRaw.split(' ') : [];
    const taskId = `task_${Date.now()}`;

    try {
      const res = await fetch('/api/tasks/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: taskId, script, args })
      });
      const data = await res.json();
      if (!res.ok) {
        alert(data.error || '启动失败');
        return
      }

      this.activeTaskId = taskId;
      document.getElementById('task-status-badge').textContent = '运行中';
      document.getElementById('task-status-badge').className = 'badge badge-success';
      document.getElementById('btn-task-start').disabled = true;
      document.getElementById('btn-task-stop').disabled = false;

      this.subscribeLogs(taskId);
    } catch (e) {
 if (e.name === 'AbortError') return;
      alert(`启动异常: ${e.message}`);
    }
  },

  async stopTask() {
    if (!this.activeTaskId) return;
    try {
      await fetch('/api/tasks/stop', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: this.activeTaskId })
      });
    } catch (e) {
 if (e.name === 'AbortError') return;
      console.warn('Failed to stop task', e);
    }
  },

  subscribeLogs(taskId) {
    if (this.eventSource) this.eventSource.close();
    this.eventSource = new EventSource(`/api/tasks/logs?id=${encodeURIComponent(taskId)}`);

    this.eventSource.onmessage = (e) => {
      try {
        const entry = JSON.parse(e.data);
        this.allLogs.push(entry);
 if (this.allLogs.length > 512) this.allLogs.shift();
        if (this.isPaused) {
          this.bufferedLogs.push(entry);
 if (this.bufferedLogs.length > 512) this.bufferedLogs.shift();
        } else {
          this.appendLogEntry(entry);
        }
      } catch (_) {}
    };

    this.eventSource.addEventListener('end', () => {
      document.getElementById('task-status-badge').textContent = '已退出';
      document.getElementById('task-status-badge').className = 'badge badge-muted';
      document.getElementById('btn-task-start').disabled = false;
      document.getElementById('btn-task-stop').disabled = true;
      if (this.eventSource) this.eventSource.close();
    });

    this.eventSource.onerror = () => {
      if (this.eventSource) this.eventSource.close();
      document.getElementById('btn-task-start').disabled = false;
      document.getElementById('btn-task-stop').disabled = true;
    };
  },

  matchesFilter(entry) {
    if (this.filterLevel !== 'ALL' && entry.level !== this.filterLevel) {
      return false;
    }
    if (this.regexFilter && !this.regexFilter.test(entry.message)) {
      return false;
    }
    return true;
  },

  appendLogEntry(entry) {
    if (!this.matchesFilter(entry)) return;
    const list = document.getElementById('viewport-log-list');
    const el = document.createElement('div');
    el.innerHTML = this.renderRowHTML(entry);
    list.appendChild(el.firstElementChild);
 while (list.children.length > 512) list.firstElementChild.remove();
    list.scrollTop = list.scrollHeight;
  },

  renderAllLogs() {
    const list = document.getElementById('viewport-log-list');
    if (!list) return;
    list.innerHTML = this.allLogs
      .filter(e => this.matchesFilter(e))
      .map(e => this.renderRowHTML(e))
      .join('');
  },

  renderRowHTML(entry) {
    const timeStr = new Date(entry.timestamp).toLocaleTimeString();
    const deltaStr = `+${entry.delta_ms}ms`;

    let contentHTML = `<span class="log-msg">${this.escapeHTML(entry.message)}</span>`;
    if (entry.is_stack) {
      contentHTML = `
        <div class="log-stack-fold" onclick="TasksHub.toggleStack(event, ${entry.id})">
          <span>▶ [堆栈折叠] ${this.escapeHTML(entry.message.substring(0, 60))}…</span>
          <pre style="display:none; margin:4px 0 0 0;">${this.escapeHTML(entry.message)}</pre>
        </div>
      `;
    }

    let contextHTML = '';
    if (this.expandedContextId === entry.id) {
      const idx = this.allLogs.findIndex(l => l.id === entry.id);
      const start = Math.max(0, idx - 5);
      const end = Math.min(this.allLogs.length, idx + 6);
      const ctxLines = this.allLogs.slice(start, end).map(l =>
        `<div>${l.id === entry.id ? '👉 ' : '   '}[${l.level}] ${this.escapeHTML(l.message)}</div>`
      ).join('');
      contextHTML = `<div class="log-context-box"><strong>前后 5 行上下文:</strong>${ctxLines}</div>`;
    }

    return `
      <div class="log-row" data-id="${entry.id}" onclick="TasksHub.toggleContext(${entry.id})">
        <span class="log-time">${timeStr}</span>
        <span class="log-delta">${deltaStr}</span>
        <span class="log-lvl log-lvl-${entry.level}">[${entry.level}]</span>
        ${contentHTML}
      </div>
      ${contextHTML}
    `;
  },

  toggleContext(id) {
    this.expandedContextId = (this.expandedContextId === id) ? null : id;
    this.renderAllLogs();
  },

  toggleStack(ev, id) {
    ev.stopPropagation();
    const pre = ev.currentTarget.querySelector('pre');
    if (pre) {
      const isHidden = pre.style.display === 'none';
      pre.style.display = isHidden ? 'block' : 'none';
    }
  },

  escapeHTML(str) {
    return (str || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
};

globalThis.TasksHub = TasksHub;
export default TasksHub;

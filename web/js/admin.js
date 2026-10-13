import { request } from '../src/api';
// omniHub 权限管理与矩阵配置交互逻辑
let lifetime = new AbortController();
const fetch = (input, options = {}) => request(input, { ...options, signal: lifetime.signal });
const AdminHub = {
  dispose() { lifetime.abort();
 this.initialized = false;
  },
  sections: [
    { id: 'gallery', name: '🖼️ 相册画廊' },
    { id: 'shortvideo', name: '🎬 短视频与图集' },
    { id: 'text', name: '📖 文本小说' },
    { id: 'audio', name: '🎵 全格式音频' },
    { id: 'manga', name: '📚 漫画条漫' },
    { id: 'games', name: '🕹️ 独立与模拟器游戏' },
    { id: 'tools', name: '🛠️ 局域网小工具' },
    { id: 'tasks', name: '💻 任务调试台' },
    { id: 'admin', name: '⚙️ 权限管理后台' }
  ],

  initialized: false,
  init() {
    lifetime = new AbortController();
    this.renderContainer();
  },

  activate() {
    if (this.initialized) return;
    this.initialized = true;
    this.loadPermissions();
    this.loadUsers();
  },

  renderContainer() {
    const container = document.getElementById('admin-container');
    if (!container) return;

    container.innerHTML = `
      <div class="admin-layout">
        <div class="admin-card">
          <h3>分权矩阵准入配置 (即时生效)</h3>
          <table class="permission-table">
            <thead>
              <tr>
                <th>分区名称</th>
                <th>最低准入要求</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody id="permission-table-body">
              <tr><td colspan="3" style="color:var(--text-muted)">正在加载矩阵…</td></tr>
            </tbody>
          </table>
        </div>

        <div class="admin-card">
          <h3>用户等级管理与即时会话失效</h3>
          <table class="permission-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>用户名</th>
                <th>当前角色</th>
                <th>等级 (0:Guest, 1~4:VIP0~3, 99:Admin)</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody id="user-table-body">
              <tr><td colspan="6" style="color:var(--text-muted)">正在加载用户…</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    `;
  },

  async loadPermissions() {
    const tbody = document.getElementById('permission-table-body');
    if (!tbody) return;

    try {
      const res = await fetch('/api/admin/permissions');
      if (!res.ok) {
        tbody.innerHTML = '<tr><td colspan="3" style="color:var(--danger)">仅管理员可访问此面板</td></tr>';
        return;
      }
      const matrix = await res.json();

      tbody.innerHTML = this.sections.map(s => {
        const lvl = matrix[s.id] || 0;
        return `
          <tr>
            <td><strong>${s.name}</strong> (${s.id})</td>
            <td>
              <select id="perm-select-${s.id}">
                <option value="0" ${lvl === 0 ? 'selected' : ''}>Guest (公开)</option>
                <option value="1" ${lvl === 1 ? 'selected' : ''}>VIP 0</option>
                <option value="2" ${lvl === 2 ? 'selected' : ''}>VIP 1</option>
                <option value="3" ${lvl === 3 ? 'selected' : ''}>VIP 2</option>
                <option value="4" ${lvl === 4 ? 'selected' : ''}>VIP 3</option>
                <option value="99" ${lvl === 99 ? 'selected' : ''}>Admin 专属</option>
              </select>
            </td>
            <td>
              <button class="btn-primary" onclick="AdminHub.savePermission('${s.id}')">保存</button>
            </td>
          </tr>
        `;
      }).join('');
    } catch (e) {
 if (e.name === 'AbortError') return;
      tbody.innerHTML = `<tr><td colspan="3" style="color:var(--danger)">加载失败: ${e.message}</td></tr>`;
    }
  },

  async savePermission(sectionId) {
    const sel = document.getElementById(`perm-select-${sectionId}`);
    if (!sel) return;
    const reqLvl = parseInt(sel.value, 10);

    try {
      const res = await fetch('/api/admin/permissions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ section_id: sectionId, required_level: reqLvl })
      });
      if (res.ok) {
        alert('分权配置已更新并即时生效');
      } else {
        alert('更新失败');
      }
    } catch (e) {
 if (e.name === 'AbortError') return;
      alert(`异常: ${e.message}`);
    }
  },

  async loadUsers() {
    const tbody = document.getElementById('user-table-body');
    if (!tbody) return;

    try {
      const res = await fetch('/api/admin/users');
      if (!res.ok) return;
      const users = await res.json();

      tbody.innerHTML = users.map(u => `
        <tr>
          <td>${u.id}</td>
          <td>${u.username}</td>
          <td>${u.role}</td>
          <td>
            <input type="number" id="user-lvl-${u.id}" value="${u.level}" min="0" max="99" style="width:60px;">
          </td>
          <td>${u.status}</td>
          <td>
            <button class="btn-primary" onclick="AdminHub.saveUserLevel(${u.id})">更新等级</button>
          </td>
        </tr>
      `).join('');
    } catch (_) {}
  },

  async saveUserLevel(userId) {
    const input = document.getElementById(`user-lvl-${userId}`);
    if (!input) return;
    const lvl = parseInt(input.value, 10);

    try {
      const res = await fetch('/api/admin/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user_id: userId, level: lvl })
      });
      if (res.ok) {
        alert('用户等级已更新，关联会话已强制下线即时生效');
      } else {
        alert('更新失败');
      }
    } catch (e) {
 if (e.name === 'AbortError') return;
      alert(`异常: ${e.message}`);
    }
  }
};

globalThis.AdminHub = AdminHub;
export default AdminHub;

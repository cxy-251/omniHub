// omniHub 核心前端逻辑
const Omni = {
  state: {
    user: null,
    activeTab: 'portal',
    libraries: [],
    primaryLib: null,
  },

  async init() {
    this.bindEvents();
    await this.fetchStatus();
    await this.fetchLibraries();
  },

  bindEvents() {
    document.querySelectorAll('nav.main-nav button').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const tab = e.target.getAttribute('data-tab');
        if (tab) this.switchTab(tab);
      });
    });
  },

  switchTab(tabId) {
    this.state.activeTab = tabId;
    document.querySelectorAll('nav.main-nav button').forEach(b => {
      b.classList.toggle('active', b.getAttribute('data-tab') === tabId);
    });
    document.querySelectorAll('.tab-pane').forEach(p => {
      p.classList.toggle('active', p.id === `pane-${tabId}`);
    });
    if (tabId !== 'media' && window.MediaHub && window.MediaHub.pauseMatrixVideos) {
      window.MediaHub.pauseMatrixVideos();
    }
    if (tabId !== 'tools' && window.ToolsHub && window.ToolsHub.deactivate) {
      window.ToolsHub.deactivate();
    }
    if (tabId === 'media' && window.MediaHub && window.MediaHub.activate) window.MediaHub.activate();
    if (tabId === 'games' && window.GamesHub && window.GamesHub.activate) window.GamesHub.activate();
    if (tabId === 'tools' && window.ToolsHub && window.ToolsHub.activate) window.ToolsHub.activate();
    if (tabId === 'admin' && window.AdminHub && window.AdminHub.activate) window.AdminHub.activate();
  },

  async fetchStatus() {
    try {
      const res = await fetch('/api/auth/status');
      const data = await res.json();
      this.state.user = data.user;
      if (!data.has_admin) {
        this.showAdminSetupModal();
      }
      const badge = document.getElementById('user-badge');
      if (badge && data.user) {
        badge.textContent = `${data.user.username} (Lv.${data.user.level})`;
        if (data.user.role === 'admin') {
          badge.className = 'badge badge-admin';
        } else {
          badge.className = 'badge badge-success';
        }
      }
    } catch (e) {
      console.error('Failed to fetch auth status', e);
    }
  },

  showAdminSetupModal() {
    if (document.getElementById('admin-setup-modal')) return;
    const modal = document.createElement('div');
    modal.id = 'admin-setup-modal';
    modal.style.cssText = 'position:fixed;top:0;left:0;width:100vw;height:100vh;background:rgba(0,0,0,0.85);z-index:99999;display:flex;align-items:center;justify-content:center;';
    modal.innerHTML = `
      <div style="background:#161b22;border:1px solid #30363d;border-radius:8px;padding:24px;width:380px;box-shadow:0 8px 24px rgba(0,0,0,0.5);color:#e6edf3;">
        <h3 style="margin-top:0;margin-bottom:12px;color:#58a6ff;">⚙️ 首次启动：设置管理员账户</h3>
        <p style="font-size:13px;color:#8b949e;margin-bottom:16px;">检测到系统尚未配置管理员。请在本地设置初始管理员账号与密码（设置完成前系统将阻断外部网络访问）：</p>
        <div style="display:flex;flex-direction:column;gap:10px;">
          <input type="text" id="init-admin-user" placeholder="管理员用户名 (如 admin)" style="background:#0d1117;border:1px solid #30363d;color:#e6edf3;padding:8px 12px;border-radius:6px;">
          <input type="password" id="init-admin-pwd" placeholder="管理员密码" style="background:#0d1117;border:1px solid #30363d;color:#e6edf3;padding:8px 12px;border-radius:6px;">
          <button id="init-admin-btn" style="background:#238636;color:#fff;border:none;padding:8px 16px;border-radius:6px;cursor:pointer;font-weight:bold;margin-top:4px;">完成初始化并进入系统</button>
        </div>
      </div>
    `;
    document.body.appendChild(modal);

    document.getElementById('init-admin-btn').addEventListener('click', async () => {
      const u = document.getElementById('init-admin-user').value.trim();
      const p = document.getElementById('init-admin-pwd').value.trim();
      if (!u || !p) {
        alert('用户名和密码均不能为空');
        return;
      }
      try {
        const res = await fetch('/api/auth/setup', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: u, password: p })
        });
        if (res.ok) {
          modal.remove();
          this.fetchStatus();
        } else {
          const err = await res.json();
          alert('初始化失败: ' + (err.error || res.statusText));
        }
      } catch (err) {
        alert('请求异常: ' + err.message);
      }
    });
  },

  async fetchLibraries() {
    try {
      const res = await fetch('/api/library/list');
      const data = await res.json();
      this.state.libraries = data.libraries || [];
      this.state.primaryLib = data.primary;
      const libStatus = document.getElementById('lib-status');
      if (libStatus) {
        if (data.primary) {
          libStatus.textContent = `主库: ${data.primary.name}`;
          libStatus.className = 'badge badge-success';
        } else {
          libStatus.textContent = '0 资源库模式';
          libStatus.className = 'badge badge-muted';
        }
      }
    } catch (e) {
      console.error('Failed to fetch libraries', e);
    }
  }
};

window.addEventListener('DOMContentLoaded', () => Omni.init());

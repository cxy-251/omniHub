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

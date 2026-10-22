// omniHub 网页游戏运行时：服务端在游戏 HTML 的 <head> 最前面注入。
// 游戏跑在沙箱（不透明来源）里，下面补上沙箱缺的东西；正常环境里每一项都会先检测、能用就不动。
(function () {
  var m = location.pathname.match(/^\/game-content\/[^/]+\//);
  var root = m ? m[0] : '/';

  // 1. 图片默认按 CORS 加载：资源响应带 Access-Control-Allow-Origin: *，否则画进 canvas 后 getImageData 报 SecurityError。
  var src = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'src');
  Object.defineProperty(HTMLImageElement.prototype, 'src', {
    configurable: true, enumerable: src.enumerable,
    get: function () { return src.get.call(this); },
    set: function (v) { if (this.getAttribute('crossorigin') === null && !/^(data|blob):/i.test(String(v))) this.crossOrigin = 'anonymous'; src.set.call(this, v); }
  });

  // 2. Worker：沙箱里不能用服务器地址创建，改为同步取回脚本、用本页的 blob 地址创建（importScripts 的相对地址按原地址解析）。
  var NativeWorker = window.Worker;
  if (NativeWorker) {
    var Worker = function (url, options) {
      try { return new NativeWorker(url, options); } catch (e) {
        var abs = new URL(url, location.href).href;
        var x = new XMLHttpRequest();
        x.open('GET', abs, false); x.send();
        var code = 'self.__omniBase=' + JSON.stringify(abs) + ';(function(){var i=self.importScripts;self.importScripts=function(){return i.apply(self,[].map.call(arguments,function(u){return new URL(u,self.__omniBase).href}))}})();\n' + x.responseText;
        return new NativeWorker(URL.createObjectURL(new Blob([code], { type: 'application/javascript' })), options);
      }
    };
    Worker.prototype = NativeWorker.prototype;
    window.Worker = Worker;
  }

  // 3. 存储：沙箱里 localStorage / IndexedDB 不可用（RPG Maker 存不了档、MZ 卡在加载）。
  //    换成按账号存在服务端的 localStorage（root + "__save"）；访客只存在本页内存里。
  var usable = true;
  try { window.localStorage.setItem('__omni', '1'); window.localStorage.removeItem('__omni'); } catch (e) { usable = false; }
  if (!usable) {
    var makeStorage = function (data, persist) {
      var s = {
        getItem: function (k) { k = String(k); return Object.prototype.hasOwnProperty.call(data, k) ? data[k] : null; },
        setItem: function (k, v) { k = String(k); v = String(v); data[k] = v; persist(k, v); },
        removeItem: function (k) { k = String(k); if (k in data) { delete data[k]; persist(k, null); } },
        clear: function () { Object.keys(data).forEach(function (k) { persist(k, null); }); data = {}; },
        key: function (i) { return Object.keys(data)[i] || null; }
      };
      Object.defineProperty(s, 'length', { get: function () { return Object.keys(data).length; } });
      return s;
    };
    var saved = {};
    try {
      var x = new XMLHttpRequest();
      x.open('GET', root + '__save', false); x.send();
      if (x.status === 200) saved = JSON.parse(x.responseText) || {};
    } catch (e) {}
    var local = makeStorage(saved, function (key, value) {
      var r = new XMLHttpRequest();
      r.open('POST', root + '__save', true);
      r.setRequestHeader('Content-Type', 'text/plain');
      r.onload = function () { if (r.status >= 400) console.warn('[omniHub] 存档未保存到服务端：' + r.status + ' ' + r.responseText); };
      r.send(JSON.stringify({ key: key, value: value }));
    });
    var session = makeStorage({}, function () {});
    Object.defineProperty(window, 'localStorage', { configurable: true, get: function () { return local; } });
    Object.defineProperty(window, 'sessionStorage', { configurable: true, get: function () { return session; } });
  }

  // 4. Service Worker：沙箱里读取 navigator.serviceWorker 会直接抛异常（Ren'Py 网页版等会去注册），换成报告「不可用」的替身。
  try { void navigator.serviceWorker; } catch (e) {
    var unavailable = function () { return Promise.reject(new Error('Service Worker 在游戏沙箱里不可用')); };
    Object.defineProperty(Navigator.prototype, 'serviceWorker', { configurable: true, get: function () {
      return { controller: null, ready: new Promise(function () {}), register: unavailable, getRegistration: function () { return Promise.resolve(undefined); },
        getRegistrations: function () { return Promise.resolve([]); }, addEventListener: function () {}, removeEventListener: function () {} };
    } });
  }

  // 5. 游戏目录里的 adapter.js（单个游戏的兼容补丁，如汉化加载），在游戏脚本之前执行。RPG 游戏由 rpg_runtime 加载。
  if (!window.__omniRpg) try {
    var a = new XMLHttpRequest();
    a.open('GET', root + 'adapter.js', false); a.send();
    if (a.status === 200 && a.responseText) (0, eval)(a.responseText + '\n//# sourceURL=adapter.js');
  } catch (e) { console.error('[omniHub] adapter.js 执行失败', e); }
})();

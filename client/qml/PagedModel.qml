import QtQuick

// PagedModel：按页懒加载的列表数据源，供 GridView 以 model: paged.total 使用。
// 委托通过 item(index) 取数据（先读 revision 以便页面到达后刷新）；缺页时异步请求。
// 只保留最近访问的 maxPages 页，滚远的页被丢弃，内存不随浏览量增长。
// 销毁时中止未完成的请求。
QtObject {
    id: paged
    // 不含 limit/offset 的接口路径，如 "/api/media/gallery/list?color=9"；变化时自动重置。
    property string endpoint: ""
    property int pageSize: 60
    property int maxPages: 8
    property int total: 0
    property int revision: 0
    property string error: ""
    readonly property bool loading: pending > 0

    property int pending: 0
    property var pages: ({})      // 页号 → 数组（普通 JS 对象，不参与绑定）
    property var recent: []       // 最近访问的页号，末尾最新
    property var requests: ({})   // 页号 → 进行中的 XMLHttpRequest

    onEndpointChanged: reset()
    Component.onDestruction: abortAll()

    // 先清空登记表再中止：abort() 会同步触发回调，回调据此识别并忽略已作废的请求。
    function abortAll() {
        var old = requests
        requests = {}
        pending = 0
        for (var p in old) old[p].abort()
    }
    function reset() {
        abortAll()
        pages = {}
        recent = []
        total = 0
        error = ""
        revision++
        if (endpoint) fetch(0)
    }
    function item(index) {
        if (index < 0) return null
        var p = Math.floor(index / pageSize)
        var rows = pages[p]
        // 绑定求值中不能有副作用（会触发绑定循环）：缺页时推迟到本轮求值之后再请求。
        if (!rows) { Qt.callLater(fetch, p); return null }
        touch(p)
        return rows[index - p * pageSize] || null
    }
    function touch(p) {
        var i = recent.indexOf(p)
        if (i === recent.length - 1) return
        if (i >= 0) recent.splice(i, 1)
        recent.push(p)
    }
    function fetch(p) {
        if (!endpoint || pages[p] || requests[p]) return
        var url = backendApi.apiUrl(endpoint + (endpoint.indexOf("?") >= 0 ? "&" : "?") + "limit=" + pageSize + "&offset=" + p * pageSize)
        if (!url) { error = "无法连接 omniHub 服务"; return }
        var xhr = new XMLHttpRequest()
        var forEndpoint = endpoint
        xhr.onreadystatechange = function () {
            if (xhr.readyState !== XMLHttpRequest.DONE) return
            if (paged.requests[p] !== xhr) return   // 已被 reset/abort
            delete paged.requests[p]
            paged.pending--
            if (forEndpoint !== paged.endpoint) return
            if (xhr.status !== 200) {
                paged.error = "请求失败（" + xhr.status + "）"
                console.warn("PagedModel", forEndpoint, "page", p, "HTTP", xhr.status, xhr.responseText)
                return
            }
            // 先写入页面数据再更新总数：总数变化会让 GridView 同步创建格子并立即读取数据。
            paged.pages[p] = JSON.parse(xhr.responseText)
            paged.touch(p)
            while (paged.recent.length > paged.maxPages) delete paged.pages[paged.recent.shift()]
            paged.error = ""
            paged.total = parseInt(xhr.getResponseHeader("X-Total-Count") || "0")
            paged.revision++
        }
        requests[p] = xhr
        pending++
        xhr.open("GET", url)
        xhr.send()
    }
}

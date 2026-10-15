import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 游戏大厅（本地原生游戏）：分类标签 + 卡片网格 + 右侧详情。
// 启动游戏后主窗口保持原样（不最小化、不关闭）；游戏在独立 cgroup，内存不算在 omniHub 名下。
Item {
    id: view

    property var gamesList: []
    property var active: null          // 正在运行的游戏会话
    property var selected: null
    property string error: ""
    property bool busy: false
    property alias category: prefs.tab

    Settings { id: prefs; category: "games"; property string tab: "__recent" }

    readonly property var categoryNames: ({steam: "Steam", unity: "Unity", wine: "Wine", godot: "Godot", unreal: "Unreal", renpy: "Ren'Py", "3ds": "3DS", app: "应用"})
    readonly property var categoryOrder: ["steam", "unity", "wine", "godot", "unreal", "renpy", "3ds", "app"]
    readonly property var counts: {
        var c = {}
        gamesList.forEach(function (g) { c[g.category] = (c[g.category] || 0) + 1 })
        return c
    }
    readonly property var shown: {
        if (category === "__recent")
            return gamesList.filter(function (g) { return !!g.last_played_at }).sort(function (a, b) { return a.last_played_at < b.last_played_at ? 1 : -1 })
        if (category === "__all") return gamesList
        return gamesList.filter(function (g) { return g.category === category })
    }

    function request(method, path, body, done) {
        var url = backendApi.apiUrl(path)
        if (!url) { view.error = "无法连接 omniHub 服务"; return }
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function () {
            if (xhr.readyState !== XMLHttpRequest.DONE || !view) return
            if (xhr.status < 200 || xhr.status >= 300) { view.error = xhr.responseText.trim() || ("请求失败（" + xhr.status + "）"); view.busy = false; return }
            view.error = ""
            done(xhr.responseText ? JSON.parse(xhr.responseText) : null)
        }
        xhr.open(method, url)
        if (body) xhr.setRequestHeader("Content-Type", "application/json")
        xhr.send(body ? JSON.stringify(body) : null)
    }
    function reload() {
        request("GET", "/api/games/list", null, function (list) { view.gamesList = list })
        request("GET", "/api/games/session", null, function (s) { view.active = s && s.state !== "exited" ? s : null; if (view.active) sessionPoll.start() })
    }
    function displayName(g) { return g.name.replace(/^\d+\s*-\s*/, "") }
    function playTime(sec) {
        if (!sec) return "未玩过"
        var h = Math.floor(sec / 3600), m = Math.floor(sec % 3600 / 60)
        return h > 0 ? h + " 小时 " + m + " 分" : Math.max(1, m) + " 分钟"
    }
    function tileColor(c) {
        var h = 0
        for (var i = 0; i < c.length; i++) h = (h * 31 + c.charCodeAt(i)) % 360
        return Qt.hsla(h / 360, 0.4, 0.3, 1)
    }
    function launch(g) {
        busy = true
        request("POST", "/api/games/launch", {library_id: g.library_id, id: g.id}, function (s) {
            view.busy = false
            view.active = s
            sessionPoll.start()
        })
    }
    function stop() {
        if (!active) return
        request("POST", "/api/games/stop", {id: active.id}, function () { stopPoll.start() })
    }
    // 游戏运行期间低频检查会话，结束后刷新时长。
    Timer { id: sessionPoll; interval: 5000; repeat: true; onTriggered: request("GET", "/api/games/session", null, function (s) { if (!s || s.state === "exited") { view.active = null; sessionPoll.stop(); view.reload() } }) }
    Timer { id: stopPoll; interval: 1500; repeat: true; onTriggered: request("GET", "/api/games/session", null, function (s) { if (!s || s.state === "exited") { view.active = null; stopPoll.stop(); view.reload() } }) }

    Component.onCompleted: reload()

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 6

        Rectangle {
            Layout.fillWidth: true
            visible: !!view.active
            height: 36; radius: 4
            color: "#1f2a3a"; border.color: "#58a6ff"
            RowLayout {
                anchors.fill: parent; anchors.leftMargin: 10; anchors.rightMargin: 6
                Label { Layout.fillWidth: true; color: "#c9d1d9"; text: view.active ? "游戏运行中：" + view.active.key.split(":").slice(1).join(":").split("/").pop() : "" }
                Chip { text: "停止游戏"; onClicked: view.stop() }
            }
        }
        RowLayout {
            Layout.fillWidth: true
            spacing: 4
            Chip { text: "最近游玩"; selected: view.category === "__recent"; onClicked: view.category = "__recent" }
            Chip { text: "全部 · " + view.gamesList.length; selected: view.category === "__all"; onClicked: view.category = "__all" }
            Rectangle { width: 1; height: 20; color: "#30363d" }
            Repeater {
                model: view.categoryOrder.filter(function (c) { return view.counts[c] })
                delegate: Chip {
                    required property string modelData
                    text: view.categoryNames[modelData] + " · " + view.counts[modelData]
                    selected: view.category === modelData
                    onClicked: view.category = modelData
                }
            }
            Item { Layout.fillWidth: true }
            IconButton { glyph: "list"; size: 30; onClicked: view.reload(); ToolTip.text: "刷新" }
        }
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12
            visible: text.length > 0
            color: view.error ? "#f85149" : "#8b949e"
            text: view.error || (view.category === "__recent" && view.shown.length === 0 ? "还没有游玩记录，从分类里选一个游戏开始" : "")
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 8
            GridView {
                id: grid
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                readonly property int columns: Math.max(3, Math.round(width / 170))
                cellWidth: Math.floor(width / columns)
                cellHeight: cellWidth * 0.62 + 40
                model: view.shown
                ScrollBar.vertical: ScrollBar {}
                delegate: Item {
                    id: cell
                    required property var modelData
                    width: grid.cellWidth
                    height: grid.cellHeight
                    Rectangle {
                        anchors.fill: parent
                        anchors.margins: 4
                        radius: 6
                        color: "#161b22"
                        border.color: view.selected && view.selected.id === cell.modelData.id ? "#58a6ff" : (tap.pressed ? "#58a6ff" : "#30363d")
                        clip: true
                        Rectangle {
                            id: art
                            width: parent.width; height: parent.height - 40
                            color: view.tileColor(cell.modelData.category)
                            Label {
                                anchors.fill: parent; anchors.margins: 8
                                visible: icon.status !== Image.Ready
                                text: view.displayName(cell.modelData)
                                wrapMode: Text.Wrap; elide: Text.ElideRight
                                horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
                                color: "#e6edf3"; font.pixelSize: 15; font.bold: true
                            }
                            Image {
                                id: icon
                                anchors.fill: parent
                                fillMode: Image.PreserveAspectFit
                                asynchronous: true
                                sourceSize.width: width
                                source: cell.modelData.icon ? backendApi.apiUrl("/api/games/icon?library_id=" + encodeURIComponent(cell.modelData.library_id) + "&id=" + encodeURIComponent(cell.modelData.id) + "&icon=" + encodeURIComponent(cell.modelData.icon)) : ""
                            }
                        }
                        Column {
                            anchors.top: art.bottom; anchors.left: parent.left; anchors.right: parent.right; anchors.margins: 5
                            Label { width: parent.width; text: view.displayName(cell.modelData); elide: Text.ElideRight; color: "#c9d1d9"; font.pixelSize: 12 }
                            Label { width: parent.width; text: view.categoryNames[cell.modelData.category] + " · " + view.playTime(cell.modelData.play_time_sec); elide: Text.ElideRight; color: "#8b949e"; font.pixelSize: 11 }
                        }
                        TapHandler { id: tap; onTapped: view.selected = cell.modelData; onDoubleTapped: if (!view.active) view.launch(cell.modelData) }
                    }
                }
            }

            // 详情
            Rectangle {
                Layout.preferredWidth: 280
                Layout.fillHeight: true
                visible: !!view.selected
                radius: 6
                color: "#161b22"; border.color: "#30363d"
                ColumnLayout {
                    anchors.fill: parent; anchors.margins: 12
                    spacing: 8
                    Label { Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#e6edf3"; font.pixelSize: 17; font.bold: true; text: view.selected ? view.displayName(view.selected) : "" }
                    Label { color: "#8b949e"; text: view.selected ? view.categoryNames[view.selected.category] : "" }
                    Label { color: "#c9d1d9"; text: view.selected ? "游玩时长：" + view.playTime(view.selected.play_time_sec) : "" }
                    Label { color: "#c9d1d9"; visible: !!(view.selected && view.selected.last_played_at); text: view.selected && view.selected.last_played_at ? "上次游玩：" + new Date(view.selected.last_played_at).toLocaleString(Qt.locale(), "yyyy-MM-dd hh:mm") : "" }
                    Label { Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: "#6e7681"; font.pixelSize: 11; text: view.selected ? view.selected.folder : "" }
                    Item { Layout.fillHeight: true }
                    Button {
                        Layout.fillWidth: true
                        text: view.busy ? "启动中…" : (view.active ? "已有游戏在运行" : "开始游戏")
                        enabled: !view.busy && !view.active
                        highlighted: true
                        onClicked: view.launch(view.selected)
                    }
                    Label { Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 11; text: "游戏在独立进程组中运行，内存不计入 omniHub。" }
                }
            }
        }
    }
}

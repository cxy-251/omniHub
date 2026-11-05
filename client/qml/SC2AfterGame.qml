import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// Global watcher survives page changes; the game process stays owned by the server.
DarkWindow {
    id: window
    width: 650; height: 460
    minimumWidth: 600; minimumHeight: 360
    visible: false
    title: "omniHub · 星际对局与回放"
    property bool enabled: false
    property bool polling: false
    property bool busy: false
    property string seenSession: ""
    property string watchedSession: ""
    property string replayID: ""
    property bool replayWasPlaying: false
    property var match: null
    property var records: []
    property string message: ""
    property bool windowOpen: false
    signal settingsRequested()
    ApiClient { id: api }

    function reveal() {
        refreshRecords()
        showNormal(); raise(); requestActivate()
    }
    function refreshRecords() {
        api.getJSON("/api/sc2/replays", function (rows) { window.records = rows })
    }
    function showRecords() { message = ""; reveal(); poll() }
    function poll() {
        if (!enabled || polling || busy) return
        polling = true
        api.send("GET", "/api/sc2/status", null, function (s) {
            window.polling = false
            window.windowOpen = !!s.window_open
            window.replayID = s.replay_id || ""
            if (window.replayID) { window.replayWasPlaying = true; return }
            if (window.replayWasPlaying) {
                window.replayWasPlaying = false
                window.message = s.replay_error ? "回放失败：" + s.replay_error : "回放已结束"
                window.reveal()
            }
            var id = s.session ? s.session.id : ""
            if (id && !s.ended) {
                if (window.watchedSession !== id) { window.watchedSession = id; window.visible = false }
                return
            }
            // Wait for the round controller to release its session before enabling rematch.
            if (!id || !s.ended || s.session.state !== "exited" || window.seenSession === id) return
            if (!s.window_open && window.watchedSession !== id) return
            window.seenSession = id
            window.match = s.result || null
            window.message = s.result ? (s.result.error || s.result.replay_error || "") : ""
            window.reveal()
        }, function () { window.polling = false })
    }
    function action(path, data, hide) {
        if (busy) return
        busy = true; message = ""
        api.send("POST", path, data, function () {
            window.busy = false
            if (path === "/api/sc2/replays") window.replayWasPlaying = true
            if (data.action === "close") window.replayWasPlaying = false
            if (hide) window.visible = false
            window.poll()
        }, function (xhr) { window.busy = false; window.message = xhr.responseText || "操作失败" })
    }
    function rematch() {
        if (!match) return
        action("/api/sc2/play", {map: match.map, race: match.race, opponents: match.opponents, mods: match.mods || []}, true)
    }
    function watch(id) { action("/api/sc2/replays", {id: id}, true) }
    Timer { interval: 2000; repeat: true; running: window.enabled; triggeredOnStart: true; onTriggered: window.poll() }

    ColumnLayout {
        anchors.fill: parent; anchors.margins: 18; spacing: 12
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 22; color: "#e6edf3"
            text: window.replayID ? "正在观看回放" : window.match ? ({Victory: "本局胜利", Defeat: "本局失败", Tie: "本局平局", Quit: "对局已退出", Error: "对局异常结束"}[window.match.result] || "对局已结束") : "最近回放"
        }
        Label { visible: !!window.match; text: window.match ? window.match.map : ""; color: "#8b949e" }
        RowLayout {
            PrimaryButton { text: "再来一局"; enabled: !!window.match && !window.busy && !window.replayID; onClicked: window.rematch() }
            Chip { text: "换地图／队伍"; enabled: !window.busy && !window.replayID; onClicked: { window.visible = false; window.settingsRequested() } }
            Chip { text: "观看本局回放"; enabled: !!(window.match && window.match.replay_id) && !window.busy && !window.replayID; onClicked: window.watch(window.match.replay_id) }
        }
        RowLayout {
            Chip { text: "结束回放"; visible: !!window.replayID; enabled: !window.busy; onClicked: window.action("/api/sc2/control", {action: "stop_replay"}, false) }
            Chip { text: "关闭游戏"; enabled: window.windowOpen && !window.busy; onClicked: window.action("/api/sc2/control", {action: "close"}, true) }
            Label { text: window.busy ? "正在处理…" : ""; color: "#8b949e" }
        }
        Label { visible: !!window.message; text: window.message; color: "#e3b341"; wrapMode: Text.Wrap; Layout.fillWidth: true }
        Label { text: "最近 10 场 · 新回放入队时，最旧记录移入系统回收站"; color: "#8b949e"; Layout.fillWidth: true; wrapMode: Text.Wrap }
        ListView {
            Layout.fillWidth: true; Layout.fillHeight: true; clip: true
            model: window.records
            ScrollBar.vertical: ScrollBar { }
            delegate: ItemDelegate {
                required property var modelData
                width: ListView.view.width; height: 54
                enabled: !window.busy && !window.replayID
                contentItem: RowLayout {
                    ColumnLayout {
                        Layout.fillWidth: true
                        Label { text: modelData.map + " · " + ({Victory: "胜利", Defeat: "失败", Tie: "平局", Quit: "退出"}[modelData.result] || modelData.result); color: "#e6edf3"; elide: Text.ElideRight; Layout.fillWidth: true }
                        Label { text: Qt.formatDateTime(new Date(modelData.created_at / 1000000), "yyyy-MM-dd hh:mm"); color: "#8b949e"; font.pixelSize: 11 }
                    }
                    Label { text: "观看回放 ▶"; color: "#58a6ff" }
                }
                onClicked: window.watch(modelData.id)
            }
            Label { anchors.centerIn: parent; visible: window.records.length === 0; text: "完成下一场对局后，回放会出现在这里"; color: "#8b949e" }
        }
    }
}

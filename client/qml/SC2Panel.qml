import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 星际争霸 2 对战面板（游戏大厅 Wine 分类里的「星际争霸 II」）：选地图、种族、电脑对手与 mod 后开局。
// 只有你一个真人，其余全是电脑；对局在独立进程组中运行，主窗口保持不动。
Rectangle {
    id: panel
    required property var request         // GamesView.request(method, path, body, done)
    required property var game            // 大厅里的「星际争霸 II」条目（含 favorite）
    signal favoriteToggled()
    property var maps: []
    property var mods: []
    property var status: ({})
    property string teamFilter: ""
    property string error: ""
    radius: 6
    color: "#161b22"; border.color: "#30363d"

    Settings {
        id: prefs
        category: "sc2"
        property string map: ""
        property string race: "P"
        property var opponents: [{race: "T", difficulty: "hard", ai_build: "random"}]
        property var mods: []
    }

    readonly property var raceNames: ({T: "人族", P: "神族", Z: "虫族", R: "随机"})
    readonly property var difficulties: [["veryeasy", "非常简单"], ["easy", "简单"], ["medium", "中等"], ["mediumhard", "较难"], ["hard", "困难"],
        ["harder", "更难"], ["veryhard", "非常难"], ["cheatvision", "作弊·视野"], ["cheatmoney", "作弊·资源"], ["cheatinsane", "作弊·疯狂"]]
    readonly property var builds: [["random", "随机打法"], ["rush", "速攻"], ["timing", "时间点"], ["power", "强势"], ["macro", "运营"], ["air", "空军"]]
    readonly property var selectedMap: maps.find(function (m) { return m.stem === prefs.map }) || null
    readonly property int maxOpponents: selectedMap ? selectedMap.max_opponents : 1
    readonly property bool running: !!(status.session && status.session.state !== "exited")

    function refreshStatus() { request("GET", "/api/sc2/status", null, function (s) { panel.status = s }) }
    function setOpponent(i, change) {
        var list = prefs.opponents.slice()
        list[i] = Object.assign({}, list[i], change)
        prefs.opponents = list
    }
    function start() {
        error = ""
        var opponents = prefs.opponents.slice(0, maxOpponents)
        request("POST", "/api/sc2/play", {map: prefs.map, race: prefs.race, opponents: opponents, mods: prefs.mods}, function () {
            refreshStatus()
            poll.start()
        })
    }
    Timer { id: poll; interval: 3000; repeat: true; onTriggered: { panel.refreshStatus(); if (!panel.running) stop() } }

    Component.onCompleted: {
        request("GET", "/api/sc2/maps", null, function (list) { panel.maps = list; if (!panel.selectedMap && list.length) prefs.map = list[0].stem })
        request("GET", "/api/sc2/mods", null, function (list) { panel.mods = list })
        refreshStatus()
    }

    ColumnLayout {
        anchors.fill: parent; anchors.margins: 10
        spacing: 6
        RowLayout {
            Label { text: "星际争霸 II · 对战"; color: "#e6edf3"; font.pixelSize: 17; font.bold: true }
            Chip { text: panel.game.favorite ? "♥ 已收藏" : "♡ 收藏"; selected: !!panel.game.favorite; onClicked: panel.favoriteToggled() }
            Item { Layout.fillWidth: true }
            Label { text: panel.maps.length ? panel.maps.length + " 张地图" : "正在读取地图（首次约 10 秒）…"; color: "#8b949e"; font.pixelSize: 12 }
        }
        // 地图
        RowLayout {
            spacing: 4
            Repeater {
                model: [["", "全部"], ["1v1", "1v1"], ["2v2", "2v2"], ["3v3", "3v3"], ["4v4+", "4v4+"]]
                delegate: Chip {
                    required property var modelData
                    text: modelData[1]; selected: panel.teamFilter === modelData[0]
                    onClicked: panel.teamFilter = modelData[0]
                }
            }
        }
        GridView {
            id: mapGrid
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            cellWidth: Math.floor(width / Math.max(3, Math.round(width / 120)))
            cellHeight: cellWidth + 18
            model: panel.maps.filter(function (m) { return !panel.teamFilter || m.team === panel.teamFilter })
            ScrollBar.vertical: ScrollBar {}
            delegate: Item {
                required property var modelData
                width: mapGrid.cellWidth; height: mapGrid.cellHeight
                Rectangle {
                    anchors.fill: parent; anchors.margins: 3
                    radius: 4; color: "#0d1117"
                    border.color: prefs.map === modelData.stem ? "#58a6ff" : "#30363d"
                    border.width: prefs.map === modelData.stem ? 2 : 1
                    Image {
                        id: thumb
                        width: parent.width - 4; height: width; x: 2; y: 2
                        asynchronous: true; fillMode: Image.PreserveAspectCrop
                        sourceSize.width: width
                        source: backendApi.apiUrl("/api/sc2/thumb?stem=" + encodeURIComponent(modelData.stem))
                    }
                    Label {
                        anchors.top: thumb.bottom; anchors.left: parent.left; anchors.right: parent.right; anchors.margins: 2
                        text: modelData.team + " " + modelData.stem; elide: Text.ElideRight; font.pixelSize: 11
                        color: prefs.map === modelData.stem ? "#58a6ff" : "#adbac7"
                    }
                    TapHandler { onTapped: prefs.map = modelData.stem }
                }
            }
        }
        // 你的种族
        RowLayout {
            spacing: 4
            Label { text: "你："; color: "#c9d1d9"; Layout.preferredWidth: 46 }
            Repeater {
                model: ["T", "P", "Z", "R"]
                delegate: Chip { required property string modelData; text: panel.raceNames[modelData]; selected: prefs.race === modelData; onClicked: prefs.race = modelData }
            }
        }
        // 电脑对手
        Repeater {
            model: Math.min(prefs.opponents.length, panel.maxOpponents)
            delegate: RowLayout {
                required property int index
                readonly property var opp: prefs.opponents[index]
                spacing: 4
                Label { text: "电脑 " + (index + 1); color: "#c9d1d9"; Layout.preferredWidth: 46; font.pixelSize: 12 }
                Repeater {
                    model: ["T", "P", "Z", "R"]
                    delegate: Chip { required property string modelData; text: panel.raceNames[modelData]; selected: opp.race === modelData; onClicked: panel.setOpponent(index, {race: modelData}) }
                }
                ComboBox {
                    implicitHeight: 28; font.pixelSize: 12; Layout.preferredWidth: 110
                    model: panel.difficulties.map(function (d) { return d[1] })
                    currentIndex: Math.max(0, panel.difficulties.findIndex(function (d) { return d[0] === opp.difficulty }))
                    onActivated: function (i) { panel.setOpponent(index, {difficulty: panel.difficulties[i][0]}) }
                }
                ComboBox {
                    implicitHeight: 28; font.pixelSize: 12; Layout.preferredWidth: 100
                    model: panel.builds.map(function (b) { return b[1] })
                    currentIndex: Math.max(0, panel.builds.findIndex(function (b) { return b[0] === (opp.ai_build || "random") }))
                    onActivated: function (i) { panel.setOpponent(index, {ai_build: panel.builds[i][0]}) }
                }
                IconButton { glyph: "close"; size: 26; visible: prefs.opponents.length > 1; onClicked: { var l = prefs.opponents.slice(); l.splice(index, 1); prefs.opponents = l } }
            }
        }
        RowLayout {
            spacing: 4
            Chip {
                text: "+ 添加电脑"
                enabled: Math.min(prefs.opponents.length, panel.maxOpponents) < panel.maxOpponents
                onClicked: prefs.opponents = prefs.opponents.concat([{race: "R", difficulty: "hard", ai_build: "random"}])
            }
            Label {
                color: "#8b949e"; font.pixelSize: 11; Layout.fillWidth: true; elide: Text.ElideRight
                text: panel.selectedMap ? "这张图最多 " + panel.maxOpponents + " 个电脑" + (panel.maxOpponents === 1 && panel.selectedMap.max_players > 2 ? "（没有队伍数据，多了会互相敌对）" : "") : ""
            }
        }
        // mod
        Flow {
            Layout.fillWidth: true
            spacing: 4
            visible: panel.mods.length > 0
            Repeater {
                model: panel.mods
                delegate: Chip {
                    required property var modelData
                    text: modelData.name
                    selected: prefs.mods.indexOf(modelData.key) >= 0
                    ToolTip.visible: hovered; ToolTip.text: modelData.desc || ""
                    onClicked: {
                        var l = prefs.mods.filter(function (k) { return k !== modelData.key })
                        if (!selected) l.push(modelData.key)
                        prefs.mods = l
                    }
                }
            }
        }
        RowLayout {
            Button {
                text: panel.running ? "对局进行中…" : "开始对局"
                highlighted: true
                enabled: !panel.running && !!panel.selectedMap
                onClicked: panel.start()
            }
            Label {
                Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12
                color: panel.error ? "#f85149" : "#c9d1d9"
                text: {
                    if (panel.error) return panel.error
                    var r = panel.status.result
                    if (panel.running) return "SC2 启动中，首次可能要等十几秒"
                    if (!r) return ""
                    return "上一局：" + r.map + " · " + ({Victory: "胜利", Defeat: "失败", Tie: "平局"}[r.result] || r.result) + (r.error ? "（" + r.error + "）" : "")
                }
            }
        }
    }
}

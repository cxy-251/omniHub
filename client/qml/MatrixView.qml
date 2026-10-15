import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore
import omniHub.Media 1.0

// 多联放映：2/3 屏并排，每屏独立频道（平台或博主）、顺序/随机、静音。
// 顶栏只有一套控件，操作焦点屏；点屏或按 1/2/3 切换焦点。
// 「焦点出声」开启时只有焦点屏出声；手动静音的屏始终无声。
// 键盘：空格 暂停/继续，←/→ 上/下一条，M 静音，Tab 显示/收起顶栏，Esc 返回浏览。
FocusScope {
    id: matrix
    required property var creators     // [{id, platform, name, posts}]
    required property var shortVideo    // ShortVideoView，提供 getJSON / localFile
    signal exitRequested()

    property int focused: 0
    property bool barVisible: true

    Settings {
        id: prefs
        category: "matrix"
        property int screens: 3
        property bool focusAudio: true
        // 每屏：{channel: "抖音" | "抖音/博主" | "", shuffle, muted, seed, index}
        property var slots: []
    }

    // 频道选项：全部、各平台、各博主（博主 ID 带 "/"）。
    readonly property var channels: {
        var list = [{value: "", label: "全部"}]
        var seen = {}
        creators.forEach(function (c) {
            if (!seen[c.platform]) { seen[c.platform] = true; list.push({value: c.platform, label: c.platform}) }
        })
        creators.forEach(function (c) { list.push({value: c.id, label: c.platform + " · " + c.name + "（" + c.posts + "）"}) })
        return list
    }

    function newSlot() { return {channel: "", shuffle: true, muted: false, seed: Math.floor(Math.random() * 1e9), index: 0} }
    // 调用前 prefs.slots 已补齐 3 屏（见 Component.onCompleted），这里不能生成随机值，否则绑定每次重算都会换片。
    function slotConfig(i) { return prefs.slots[i] || {channel: "", shuffle: true, muted: false, seed: 0, index: 0} }
    Component.onCompleted: {
        if (prefs.slots.length >= 3) return
        var all = prefs.slots.slice()
        while (all.length < 3) all.push(newSlot())
        prefs.slots = all
    }
    function setSlot(i, changes) {
        var all = prefs.slots.slice()
        while (all.length <= i) all.push(newSlot())
        var s = Object.assign({}, slotConfig(i), changes)
        all[i] = s
        prefs.slots = all
    }

    focus: true
    Keys.onPressed: function (event) {
        var slot = repeater.itemAt(focused)
        if (event.key === Qt.Key_Escape) matrix.exitRequested()
        else if (event.key === Qt.Key_Tab) barVisible = !barVisible
        else if (event.key >= Qt.Key_1 && event.key < Qt.Key_1 + prefs.screens) focused = event.key - Qt.Key_1
        else if (!slot) return
        else if (event.key === Qt.Key_Space) slot.togglePause()
        else if (event.key === Qt.Key_Right) slot.step(1)
        else if (event.key === Qt.Key_Left) slot.step(-1)
        else if (event.key === Qt.Key_M) setSlot(focused, {muted: !slotConfig(focused).muted})
        else return
        event.accepted = true
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // 顶栏：全局设置 + 焦点屏控件（标题显示在各屏底部，顶栏只放控件）
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: matrix.barVisible ? 40 : 0
            visible: matrix.barVisible
            color: "#161b22"
            RowLayout {
                id: bar
                anchors.fill: parent; anchors.leftMargin: 8; anchors.rightMargin: 8
                spacing: 4
                property var cfg: matrix.slotConfig(matrix.focused)
                property var slotItem: repeater.itemAt(matrix.focused)
                Chip { text: "浏览"; onClicked: matrix.exitRequested() }
                Chip { text: "多联放映"; selected: true }
                Rectangle { width: 1; height: 20; color: "#30363d" }
                Chip { text: "2 屏"; selected: prefs.screens === 2; onClicked: { prefs.screens = 2; matrix.focused = Math.min(matrix.focused, 1) } }
                Chip { text: "3 屏"; selected: prefs.screens === 3; onClicked: prefs.screens = 3 }
                Chip { text: "焦点出声"; selected: prefs.focusAudio; onClicked: prefs.focusAudio = !prefs.focusAudio }
                Rectangle { width: 1; height: 20; color: "#30363d" }
                Label { text: "屏 " + (matrix.focused + 1); color: "#58a6ff"; font.pixelSize: 13 }
                ComboBox {
                    Layout.fillWidth: true
                    Layout.maximumWidth: 320
                    Layout.minimumWidth: 140
                    implicitHeight: 28
                    font.pixelSize: 13
                    textRole: "label"; valueRole: "value"
                    model: matrix.channels
                    currentIndex: Math.max(0, indexOfValue(bar.cfg.channel))
                    onActivated: matrix.setSlot(matrix.focused, {channel: currentValue, index: 0, seed: Math.floor(Math.random() * 1e9)})
                }
                Chip { text: bar.cfg.shuffle ? "随机" : "顺序"; onClicked: matrix.setSlot(matrix.focused, {shuffle: !bar.cfg.shuffle, index: 0}) }
                Chip { text: "◀ 上一条"; onClicked: if (bar.slotItem) bar.slotItem.step(-1) }
                Chip { text: bar.slotItem && bar.slotItem.paused ? "▶ 播放" : "⏸ 暂停"; onClicked: if (bar.slotItem) bar.slotItem.togglePause() }
                Chip { text: "下一条 ▶"; onClicked: if (bar.slotItem) bar.slotItem.step(1) }
                Chip { text: bar.cfg.muted ? "🔇 已静音" : "🔊 声音"; selected: bar.cfg.muted; onClicked: matrix.setSlot(matrix.focused, {muted: !bar.cfg.muted}) }
                Item { Layout.fillWidth: true }
            }
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 2
            Repeater {
                id: repeater
                model: prefs.screens
                delegate: Rectangle {
                    id: slot
                    required property int index
                    readonly property var cfg: matrix.slotConfig(index)
                    readonly property bool audible: !cfg.muted && (!prefs.focusAudio || matrix.focused === index)
                    property var post: null
                    property int total: 0
                    property bool paused: false
                    property string caption: post ? post.creator_id.split("/")[1] + " · " + post.published + " · " + post.title + "（" + (cfg.index + 1) + "/" + total + "）" : (error || "加载中…")
                    property string error: ""
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    color: "black"
                    border.color: matrix.focused === index ? "#58a6ff" : "transparent"
                    border.width: 2

                    function load() {
                        var q = "/api/media/shortvideo/sequence?portrait=1&index=" + cfg.index + "&seed=" + cfg.seed + "&order=" + (cfg.shuffle ? "shuffle" : "sequential")
                        if (cfg.channel.indexOf("/") >= 0) q += "&creator=" + encodeURIComponent(cfg.channel)
                        else if (cfg.channel) q += "&platform=" + encodeURIComponent(cfg.channel)
                        error = ""
                        matrix.shortVideo.getJSON(q, function (result) {
                            total = result.total
                            post = result.post
                            video.source = matrix.shortVideo.localFile(post, post.id)
                            paused = false
                        })
                    }
                    function step(delta) {
                        var n = Math.max(1, total)
                        matrix.setSlot(index, {index: (cfg.index + delta + n) % n})
                    }
                    function togglePause() {
                        if (paused) video.play(); else video.pause()
                        paused = !paused
                    }
                    // 频道、顺序、序号或种子变化时重新取片；静音变化不重新加载。
                    readonly property string sequenceKey: cfg.channel + "|" + cfg.shuffle + "|" + cfg.seed + "|" + cfg.index
                    onSequenceKeyChanged: load()
                    Component.onCompleted: load()

                    MpvItem {
                        id: video
                        anchors.fill: parent
                        anchors.margins: 2
                        volume: slot.audible ? 100 : 0
                        onPlaybackEnded: slot.step(1)
                    }
                    TapHandler { onTapped: { matrix.focused = slot.index; matrix.forceActiveFocus() } }
                    // 底部标题条：静音标记 + 博主 · 日期 · 标题（序号/总数）
                    Rectangle {
                        anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
                        anchors.margins: 2
                        height: 22
                        color: "#99000000"
                        Label {
                            anchors.fill: parent; anchors.leftMargin: 6; anchors.rightMargin: 6
                            verticalAlignment: Text.AlignVCenter
                            elide: Text.ElideRight
                            font.pixelSize: 12
                            color: slot.error ? "#f85149" : "#e6edf3"
                            text: (slot.audible ? "" : "🔇 ") + slot.caption
                        }
                    }
                }
            }
        }
    }
}

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

        // 顶栏：全局设置 + 焦点屏控件
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: matrix.barVisible ? bar.implicitHeight + 12 : 0
            visible: matrix.barVisible
            color: "#161b22"
            RowLayout {
                id: bar
                anchors.fill: parent; anchors.margins: 6
                property var cfg: matrix.slotConfig(matrix.focused)
                property var slotItem: repeater.itemAt(matrix.focused)
                Button { text: "返回浏览"; onClicked: matrix.exitRequested() }
                ComboBox {
                    model: ["2 屏", "3 屏"]
                    currentIndex: prefs.screens - 2
                    onActivated: function (i) { prefs.screens = i + 2; matrix.focused = Math.min(matrix.focused, prefs.screens - 1) }
                }
                CheckBox { text: "焦点出声"; checked: prefs.focusAudio; onToggled: prefs.focusAudio = checked }
                Label { text: "屏 " + (matrix.focused + 1) + "："; color: "#58a6ff" }
                ComboBox {
                    Layout.preferredWidth: 280
                    textRole: "label"; valueRole: "value"
                    model: matrix.channels
                    currentIndex: Math.max(0, indexOfValue(bar.cfg.channel))
                    onActivated: matrix.setSlot(matrix.focused, {channel: currentValue, index: 0, seed: Math.floor(Math.random() * 1e9)})
                }
                Button { text: bar.cfg.shuffle ? "随机" : "顺序"; onClicked: matrix.setSlot(matrix.focused, {shuffle: !bar.cfg.shuffle, index: 0}) }
                Button { text: "上一条"; onClicked: if (bar.slotItem) bar.slotItem.step(-1) }
                Button { text: bar.slotItem && bar.slotItem.paused ? "播放" : "暂停"; onClicked: if (bar.slotItem) bar.slotItem.togglePause() }
                Button { text: "下一条"; onClicked: if (bar.slotItem) bar.slotItem.step(1) }
                Button { text: bar.cfg.muted ? "取消静音" : "静音"; onClicked: matrix.setSlot(matrix.focused, {muted: !bar.cfg.muted}) }
                Label {
                    Layout.fillWidth: true
                    elide: Text.ElideRight; color: "#8b949e"
                    text: bar.slotItem ? bar.slotItem.caption : ""
                }
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
                        var q = "/api/media/shortvideo/sequence?index=" + cfg.index + "&seed=" + cfg.seed + "&order=" + (cfg.shuffle ? "shuffle" : "sequential")
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
                    Label {
                        anchors.bottom: parent.bottom; anchors.left: parent.left; anchors.margins: 8
                        visible: !slot.audible
                        text: "🔇"; font.pixelSize: 20
                    }
                }
            }
        }
    }
}

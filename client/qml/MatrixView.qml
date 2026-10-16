import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore
import omniHub.Media 1.0

// 多联放映（短视频与相册共用）：2/3 屏并排，只放竖屏内容。domain 决定每屏放什么：
//   shortvideo  视频（mpv），播完换下一条；可焦点出声、静音
//   gallery     图片，按设定的秒数自动轮播
// 每屏独立频道（推荐、我的点赞、平台/分组），顶栏只有一套控件，操作焦点屏；点屏或按 1/2/3 切换焦点。
// 点赞、移到回收站、伴听（全局音声引擎，伴听时视频静音）、记住每屏位置、弹出独立窗口——两种内容同一套代码。
// 键盘：空格 暂停/继续，←/→ 上/下一条，L 点赞，M 静音，Tab 固定/收起顶栏，F 全屏（独立窗口），Esc 返回/关闭。
FocusScope {
    id: matrix
    property string domain: "shortvideo"
    readonly property bool isVideo: domain === "shortvideo"
    property bool detached: false
    // 全局音声播放引擎（伴听用）；伴听进行时所有视频屏静音。
    property var audioEngine: null
    property var audioAlbums: []
    readonly property bool accompanying: !!(audioEngine && audioEngine.player.playing)
    property bool fullscreen: false
    signal exitRequested()
    signal detachRequested()
    signal fullscreenToggled()

    property int focused: 0
    // 打开时默认暂停：视频停在第一帧、图片不轮播，按「全部播放」开始。
    property bool playing: false
    // 设置读出并补齐各屏配置后才开始加载；否则各屏会先按默认值加载同一条，再换成记住的那条。
    property bool ready: false
    // 顶栏：进入后显示 3 秒；之后鼠标移到顶边出现，离开 1.5 秒收起；📌 固定后常显。
    property bool barPinned: false
    property bool barShown: true
    readonly property bool barVisible: barPinned || barShown || barHover.hovered || topEdge.hovered
    Timer { id: barTimer; interval: 3000; running: true; onTriggered: matrix.barShown = false }
    function revealBar() { barShown = true; barTimer.interval = 1500; barTimer.restart() }
    function setAllPlaying(on) {
        playing = on
        for (var i = 0; i < repeater.count; i++) { var item = repeater.itemAt(i); if (item) item.setPaused(!on) }
    }

    FeedApi { id: feed; domain: matrix.domain }
    ApiClient { id: api }

    // 开发验证：OMNIHUB_DEV_MATRIX=<秒> 自动全部播放，并按这个间隔打印各屏丢帧统计。
    Timer {
        interval: typeof devMatrix !== "undefined" && devMatrix > 0 ? devMatrix * 1000 : 5000; repeat: true
        running: typeof devMatrix !== "undefined" && devMatrix > 0
        onTriggered: {
            if (!matrix.playing) matrix.setAllPlaying(true)
            for (var i = 0; i < repeater.count; i++) { var item = repeater.itemAt(i); if (item) console.log("matrix-stats", i, item.post ? item.post.id : "-", JSON.stringify(item.stats())) }
        }
    }

    Settings {
        id: prefs
        category: matrix.isVideo ? "matrix" : "matrix_gallery"
        property int screens: 3
        property bool focusAudio: true
        property int slideSeconds: 5
        // 每屏：{channel, shuffle, muted, seed, index, postId, pos}
        property var slots: []
    }

    // 频道：推荐、我的点赞、平台（短视频）、📌 固定的分组、其余分组。
    readonly property var channels: {
        var list = [{value: "recommend", label: "推荐"}, {value: "liked", label: "♥ 我的点赞"}]
        if (isVideo) {
            var seen = {}
            feed.groups.forEach(function (g) { if (!seen[g.platform]) { seen[g.platform] = true; list.push({value: "tag:platform:" + g.platform, label: g.platform}) } })
        }
        var label = function (g, pinned) { return (pinned ? "📌 " : "") + (g.platform ? g.platform + " · " : "") + g.label + "（" + g.count + "）" }
        feed.pins.forEach(function (id) { var g = feed.groups.find(function (x) { return x.id === id }); if (g) list.push({value: "group:" + g.id, label: label(g, true)}) })
        feed.groups.forEach(function (g) { if (feed.pins.indexOf(g.id) < 0) list.push({value: "group:" + g.id, label: label(g, false)}) })
        return list
    }
    // 旧版保存的频道（""、"抖音"、"抖音/博主"）换成新写法。
    function normChannel(c) {
        if (!c) return "recommend"
        if (c === "liked" || c === "recommend" || c.indexOf("group:") === 0 || c.indexOf("tag:") === 0) return c
        return c.indexOf("/") >= 0 ? "group:" + c : "tag:platform:" + c
    }
    function newSlot() { return {channel: "recommend", shuffle: true, muted: false, seed: Math.floor(Math.random() * 1e9), index: 0, postId: "", pos: 0} }
    function slotConfig(i) {
        var c = prefs.slots[i] || {channel: "recommend", shuffle: true, muted: false, seed: 0, index: 0, postId: "", pos: 0}
        return c.channel === normChannel(c.channel) ? c : Object.assign({}, c, {channel: normChannel(c.channel)})
    }
    Component.onCompleted: {
        if (prefs.slots.length < 3) {
            var all = prefs.slots.slice()
            while (all.length < 3) all.push(newSlot())
            prefs.slots = all
        }
        ready = true
        feed.loadGroups()
        if (audioEngine) api.getJSON("/api/media/audio/albums?limit=100", function (list) { matrix.audioAlbums = list })
    }
    function setSlot(i, changes) {
        var all = prefs.slots.slice()
        while (all.length <= i) all.push(newSlot())
        all[i] = Object.assign({}, slotConfig(i), changes)
        prefs.slots = all
    }
    // 关闭前记下各屏位置。
    function savePositions() {
        for (var i = 0; i < repeater.count; i++) { var item = repeater.itemAt(i); if (item) item.savePosition() }
    }
    Component.onDestruction: savePositions()
    Timer { interval: 10000; repeat: true; running: matrix.playing; onTriggered: matrix.savePositions() }

    focus: true
    Keys.onPressed: function (event) {
        var slot = repeater.itemAt(focused)
        if (event.key === Qt.Key_Escape) { if (matrix.fullscreen) matrix.fullscreenToggled(); else matrix.exitRequested() }
        else if (event.key === Qt.Key_Tab) barPinned = !barPinned
        else if (event.key === Qt.Key_F && matrix.detached) matrix.fullscreenToggled()
        else if (event.key >= Qt.Key_1 && event.key < Qt.Key_1 + prefs.screens) focused = event.key - Qt.Key_1
        else if (!slot) return
        else if (event.key === Qt.Key_Space) slot.togglePause()
        else if (event.key === Qt.Key_Right) slot.step(1)
        else if (event.key === Qt.Key_Left) slot.step(-1)
        else if (event.key === Qt.Key_L) slot.toggleLike()
        else if (event.key === Qt.Key_M && matrix.isVideo) setSlot(focused, {muted: !slotConfig(focused).muted})
        else return
        event.accepted = true
    }

    Item {
        anchors.fill: parent
        // 顶栏：全局设置 + 焦点屏控件（标题显示在各屏底部，顶栏只放控件）
        Rectangle {
            id: barBox
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            height: 40
            z: 2
            visible: matrix.barVisible
            color: "#e6161b22"
            HoverHandler { id: barHover; onHoveredChanged: if (!hovered) matrix.revealBar() }
            RowLayout {
                id: bar
                anchors.fill: parent; anchors.leftMargin: 8; anchors.rightMargin: 8
                spacing: 4
                property var cfg: matrix.slotConfig(matrix.focused)
                // itemAt() 不发通知；读 count 让格子创建后重新求值。
                property var slotItem: { repeater.count; return repeater.itemAt(matrix.focused) }
                Chip { text: matrix.detached ? "关闭窗口" : "浏览"; onClicked: matrix.exitRequested() }
                Chip { text: "弹出窗口"; visible: !matrix.detached; onClicked: matrix.detachRequested() }
                Chip { text: matrix.fullscreen ? "退出全屏" : "全屏"; visible: matrix.detached; onClicked: matrix.fullscreenToggled() }
                Rectangle { width: 1; height: 20; color: "#30363d" }
                Chip { text: "2 屏"; selected: prefs.screens === 2; onClicked: { prefs.screens = 2; matrix.focused = Math.min(matrix.focused, 1) } }
                Chip { text: "3 屏"; selected: prefs.screens === 3; onClicked: prefs.screens = 3 }
                Chip { visible: matrix.isVideo; text: "焦点出声"; selected: prefs.focusAudio; onClicked: prefs.focusAudio = !prefs.focusAudio }
                Chip {
                    // 图片轮播间隔
                    visible: !matrix.isVideo
                    text: prefs.slideSeconds + " 秒/张"
                    onClicked: { var c = [3, 5, 8, 15]; prefs.slideSeconds = c[(c.indexOf(prefs.slideSeconds) + 1) % c.length] }
                }
                Chip { text: matrix.playing ? "⏸ 全部暂停" : "▶ 全部播放"; selected: matrix.playing; onClicked: matrix.setAllPlaying(!matrix.playing) }
                Rectangle { width: 1; height: 20; color: "#30363d" }
                Label { text: "屏 " + (matrix.focused + 1); color: "#58a6ff"; font.pixelSize: 13 }
                ComboBox {
                    Layout.fillWidth: true
                    Layout.maximumWidth: 280
                    Layout.minimumWidth: 120
                    implicitHeight: 28
                    font.pixelSize: 13
                    textRole: "label"; valueRole: "value"
                    model: matrix.channels
                    currentIndex: Math.max(0, indexOfValue(bar.cfg.channel))
                    onActivated: matrix.setSlot(matrix.focused, {channel: currentValue, index: 0, postId: "", pos: 0, seed: Math.floor(Math.random() * 1e9)})
                }
                // 推荐频道本身就是随机的；其它频道可选顺序/随机。
                Chip { visible: bar.cfg.channel !== "recommend"; text: bar.cfg.shuffle ? "随机" : "顺序"; onClicked: matrix.setSlot(matrix.focused, {shuffle: !bar.cfg.shuffle, index: 0, postId: "", pos: 0}) }
                Chip { text: "◀"; onClicked: if (bar.slotItem) bar.slotItem.step(-1) }
                Chip { text: bar.slotItem && bar.slotItem.paused ? "▶" : "⏸"; onClicked: if (bar.slotItem) bar.slotItem.togglePause() }
                Chip { text: "▶▶"; onClicked: if (bar.slotItem) bar.slotItem.step(1) }
                Chip { visible: matrix.isVideo; text: bar.cfg.muted ? "🔇" : "🔊"; selected: bar.cfg.muted; onClicked: matrix.setSlot(matrix.focused, {muted: !bar.cfg.muted}) }
                Chip {
                    readonly property bool liked: !!(bar.slotItem && bar.slotItem.post && bar.slotItem.post.liked)
                    text: liked ? "♥ 已赞" : "♡ 点赞"; selected: liked
                    enabled: !!(bar.slotItem && bar.slotItem.post)
                    onClicked: bar.slotItem.toggleLike()
                }
                Chip {
                    // 连点两次才移到回收站（3 秒内）。短视频进系统回收站；相册只在 omniHub 里隐藏（原图库只读），都能恢复。
                    id: trashChip
                    property bool armed: false
                    text: armed ? "再点一次确认" : "🗑 回收站"; selected: armed
                    enabled: !!(bar.slotItem && bar.slotItem.post)
                    onClicked: { if (armed) { armed = false; bar.slotItem.trash() } else { armed = true; disarm.restart() } }
                    Timer { id: disarm; interval: 3000; onTriggered: trashChip.armed = false }
                }
                // 伴听：选一张音声专辑，从上次听到的地方接着放；选「关」暂停。底部播放条可切集、调倍速。
                ComboBox {
                    visible: !!matrix.audioEngine && matrix.audioAlbums.length > 0
                    Layout.preferredWidth: 160
                    implicitHeight: 28
                    font.pixelSize: 13
                    textRole: "label"; valueRole: "value"
                    model: [{value: "", label: "🎧 伴听：关"}].concat(matrix.audioAlbums.map(function (a) { return {value: a.id, label: "🎧 " + a.title} }))
                    currentIndex: matrix.accompanying ? Math.max(0, indexOfValue(matrix.audioEngine.albumId)) : 0
                    onActivated: {
                        if (!currentValue) { matrix.audioEngine.player.pause(); return }
                        var album = matrix.audioAlbums.find(function (a) { return a.id === currentValue })
                        if (matrix.audioEngine.albumId === currentValue && matrix.audioEngine.active) matrix.audioEngine.player.play()
                        else matrix.audioEngine.continueAlbum(album.id, album.title)
                    }
                    ToolTip.visible: hovered && matrix.accompanying
                    ToolTip.text: matrix.accompanying && matrix.audioEngine.track ? "正在伴听：" + matrix.audioEngine.track.title : ""
                }
                Item { Layout.fillWidth: true }
                Chip { text: "📌"; selected: matrix.barPinned; onClicked: matrix.barPinned = !matrix.barPinned }
            }
        }
        // 顶边感应条：鼠标移到最上方时显示顶栏；触屏点顶边同样显示。
        Item {
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            height: 10
            z: 3
            HoverHandler { id: topEdge; onHoveredChanged: if (hovered) matrix.revealBar() }
            TapHandler { onTapped: matrix.revealBar() }
        }

        RowLayout {
            anchors.fill: parent
            spacing: 2
            Repeater {
                id: repeater
                model: prefs.screens
                delegate: Rectangle {
                    id: slot
                    required property int index
                    readonly property var cfg: matrix.slotConfig(index)
                    readonly property bool audible: matrix.isVideo && !matrix.accompanying && !cfg.muted && (!prefs.focusAudio || matrix.focused === index)
                    property var post: null
                    property int total: 0
                    property bool paused: !matrix.playing
                    property double shownAt: 0
                    readonly property string caption: !post ? (error || "加载中…")
                        : matrix.isVideo ? post.creator_id.split("/")[1] + " · " + post.published + " · " + post.title + "（" + (cfg.index + 1) + "/" + total + "）"
                        : (cfg.index + 1) + " / " + total
                    property string error: ""
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    color: "black"
                    border.color: matrix.focused === index ? "#58a6ff" : "transparent"
                    border.width: 2
                    readonly property var video: matrix.isVideo ? content.item : null

                    function load() {
                        if (!matrix.ready) return
                        var q = feed.path("sequence") + "?portrait=1&channel=" + encodeURIComponent(cfg.channel) + "&seed=" + cfg.seed + "&index=" + cfg.index
                        if (matrix.isVideo) q += "&kind=video"
                        if (cfg.shuffle && cfg.channel !== "recommend") q += "&order=shuffle"
                        error = ""
                        post = null   // 新内容到达前不保存位置，免得把上一条的 ID 记到新序号下
                        if (typeof devMatrix !== "undefined" && devMatrix > 0) console.log("matrix-load", index, q, cfg.postId, cfg.pos)
                        var resume = cfg.postId && cfg.pos > 1 ? {id: cfg.postId, pos: cfg.pos} : null
                        slotApi.send("GET", q, null, function (result) {
                            total = result.total
                            post = result.item
                            shownAt = Date.now()
                            feed.event(post, "view")
                            if (slot.video) {
                                slot.video.source = slotApi.localFile(post, post.id)
                                // 同一条视频才接着上次的位置放；序列变了（比如有视频移进回收站）就从头放。
                                if (resume && resume.id === post.id) slot.video.position = resume.pos
                                if (slot.paused) slot.video.pause()   // 默认暂停：停在第一帧
                            }
                        })
                    }
                    // auto 为 true 表示自动换下一条（播完、轮播到时），不算「划走」。
                    function step(delta, auto) {
                        if (post && !auto && delta > 0 && Date.now() - shownAt < 2000) feed.event(post, "skip")
                        var n = Math.max(1, total)
                        matrix.setSlot(index, {index: (cfg.index + delta + n) % n, postId: "", pos: 0})
                    }
                    function setPaused(p) {
                        paused = p
                        if (slot.video) { if (p) slot.video.pause(); else slot.video.play() }
                    }
                    function togglePause() { setPaused(!paused) }
                    function stats() { return slot.video ? slot.video.stats() : ({}) }
                    function savePosition() {
                        if (!post) return
                        var pos = slot.video && slot.video.mediaLoaded ? Math.floor(slot.video.position) : 0
                        matrix.setSlot(index, {postId: post.id, pos: pos})
                    }
                    function toggleLike() {
                        if (!post) return
                        var target = post, liked = !post.liked
                        feed.like(target, liked, function () {
                            if (slot.post === target) slot.post = Object.assign({}, target, {liked: liked})
                        })
                    }
                    function trash() {
                        if (!post) return
                        var target = post
                        if (slot.video) slot.video.source = ""   // 先放开文件
                        feed.trash(target, function () {
                            // 序列少了这一条，同一序号就是下一条。
                            matrix.setSlot(index, {postId: "", pos: 0})
                            slot.load()
                        })
                    }
                    ApiClient { id: slotApi; onErrorChanged: slot.error = error }
                    Component.onDestruction: savePosition()
                    // 频道、顺序、序号或种子变化时重新取；静音和位置变化不重新加载。
                    readonly property string sequenceKey: cfg.channel + "|" + cfg.shuffle + "|" + cfg.seed + "|" + cfg.index
                    onSequenceKeyChanged: load()
                    Connections { target: matrix; function onReadyChanged() { slot.load() } }
                    Component.onCompleted: load()

                    // 内容：视频屏用 mpv，图片屏用 Image（按屏幕尺寸解码，切走即释放）。
                    Loader {
                        id: content
                        anchors.fill: parent
                        anchors.margins: 2
                        sourceComponent: matrix.isVideo ? videoScreen : imageScreen
                        onLoaded: if (slot.post) slot.load()
                    }
                    Component {
                        id: videoScreen
                        MpvItem {
                            volume: slot.audible ? 100 : 0
                            onPlaybackEnded: { feed.event(slot.post, "complete"); slot.step(1, true) }
                        }
                    }
                    Component {
                        id: imageScreen
                        Image {
                            fillMode: Image.PreserveAspectFit
                            asynchronous: true
                            sourceSize: Qt.size(width, height)
                            source: slot.post ? backendApi.apiUrl("/api/media/gallery/original?library_id=" + encodeURIComponent(slot.post.library_id) + "&id=" + encodeURIComponent(slot.post.id)) : ""
                            BusyIndicator { anchors.centerIn: parent; running: parent.status === Image.Loading }
                        }
                    }
                    // 图片轮播：播放中且图片已显示时，按设定秒数换下一张。
                    Timer {
                        interval: prefs.slideSeconds * 1000
                        running: !matrix.isVideo && !slot.paused && !!slot.post && content.item && content.item.status === Image.Ready
                        onTriggered: slot.step(1, true)
                    }
                    TapHandler { onTapped: { matrix.focused = slot.index; matrix.forceActiveFocus() } }
                    // 底部：进度细条（视频）+ 标题条（静音/伴听、点赞标记 + 说明）
                    Rectangle {
                        anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
                        anchors.margins: 2
                        height: 22
                        color: "#99000000"
                        Rectangle {
                            anchors.left: parent.left; anchors.top: parent.top
                            height: 2; color: "#58a6ff"
                            visible: !!slot.video
                            width: slot.video && slot.video.duration > 0 ? parent.width * Math.min(1, slot.video.position / slot.video.duration) : 0
                        }
                        Label {
                            anchors.fill: parent; anchors.leftMargin: 6; anchors.rightMargin: 6
                            verticalAlignment: Text.AlignVCenter
                            elide: Text.ElideRight
                            font.pixelSize: 12
                            color: slot.error ? "#f85149" : "#e6edf3"
                            text: (matrix.isVideo && !slot.audible ? (matrix.accompanying ? "🎧 " : "🔇 ") : "") + (slot.post && slot.post.liked ? "♥ " : "") + slot.caption
                        }
                    }
                }
            }
        }
    }
}

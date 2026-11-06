import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore
import omniHub.Media 1.0

// 短视频浏览：先选平台（全部 | 抖音 | … | YouTube），再选频道（推荐、点赞、博主）→ 作品网格；视频用内嵌 mpv 直接播放磁盘文件，图集经本机服务读取（原目录或 albums.db）。
// 页面隐藏时由 Main.qml 的 Loader 销毁，解码器与图片缓存随之释放。
Item {
    id: view
    readonly property int pageSize: 60
    property string error: ""
    property int openIndex: -1
    property alias mode: viewPrefs.mode   // browse | matrix，记住上次的模式
    // 多联放映已在独立窗口中：本页不再内嵌一份，点「多联放映」只把窗口提到前面。
    property bool matrixWindowOpen: false
    property var audioEngine: null        // 全局音声引擎，交给多联放映做伴听
    signal matrixWindowRequested()
    // muted / volume：查看作品时的静音与音量，换下一个作品也沿用
    Settings { id: viewPrefs; category: "shortvideo"; property string mode: "browse"; property string channel: "recommend"; property string platform: ""; property bool muted: false; property real volume: 100 }
    property alias channel: viewPrefs.channel
    property alias platform: viewPrefs.platform

    // 推荐、点赞、回收站、行为上报与频道（博主分组）——与相册共用 FeedApi。
    FeedApi { id: feed; domain: "shortvideo"; onErrorChanged: view.error = error }
    readonly property var platforms: {
        var seen = {}, out = []
        feed.groups.forEach(function (g) { if (g.platform && !seen[g.platform]) { seen[g.platform] = true; out.push(g.platform) } })
        return out
    }
    function pickPlatform(p) {
        platform = p
        // 当前博主不属于所选平台时回到推荐。
        if (channel.indexOf("group:") === 0 && !feed.groups.some(function (g) { return "group:" + g.id === channel && (!p || g.platform === p) })) channel = "recommend"
    }
    // 旧版把平台当频道（tag:platform:抖音）存着：改成选中该平台；平台没有了（库离线等）回到「全部」。
    Connections {
        target: feed
        function onGroupsChanged() {
            if (view.channel.indexOf("tag:platform:") === 0) { view.platform = view.channel.slice(13); view.channel = "recommend" }
            if (view.platform && feed.groups.length && view.platforms.indexOf(view.platform) < 0) view.platform = ""
        }
    }
    // 点赞状态在本地先改，列表数据（按页缓存）不必重取。
    property var likedOverride: ({})
    function isLiked(post) { return post && (post.id in likedOverride ? likedOverride[post.id] : !!post.liked) }
    function toggleLike(post) {
        var on = !isLiked(post)
        feed.like(post, on, function () { var m = Object.assign({}, view.likedOverride); m[post.id] = on; view.likedOverride = m })
    }

    ApiClient { id: api; onErrorChanged: view.error = error }
    function getJSON(path, done) { api.getJSON(path, done) }
    function apiQuery(path, post, extra) {
        var q = "library_id=" + encodeURIComponent(post.library_id) + "&id=" + encodeURIComponent(post.id)
        return backendApi.apiUrl(path + "?" + q + (extra || ""))
    }
    function localFile(post, rel) { return api.localFile(post, rel) }
    function postAt(i) { paged.revision; return paged.item(i) }

    // 多联放映时清空浏览数据源，网格格子与缩略图随之释放。
    // 开发验证：devScroll < 0 时打开作品，每 -devScroll 毫秒切到下一个。
    Timer {
        interval: typeof devScroll !== "undefined" && devScroll < 0 ? -devScroll : 1000; repeat: true; triggeredOnStart: true
        running: typeof devScroll !== "undefined" && devScroll < 0 && paged.total > 0
        onTriggered: view.openIndex = (view.openIndex + 1) % paged.total
    }
    PagedModel { id: paged; endpoint: view.mode === "browse" ? feed.itemsPath(view.channel, "", view.platform ? "tag:platform:" + view.platform : "") : "" }

    Component.onCompleted: {
        backendApi.refreshLibraries()
        feed.loadGroups()
    }
    // 列表变短（移到回收站后）时停在原位置看下一个；原来就是最后一个则退到前一个。
    Connections {
        target: paged
        function onTotalChanged() { if (view.openIndex >= paged.total && paged.total > 0) view.openIndex = paged.total - 1 }
    }


    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 6
        visible: view.openIndex < 0 && view.mode === "browse"

        // 一行：浏览 | 多联放映 ‖ 平台 ▾ ‖ 推荐 | 我的点赞 | 📌 固定的博主 | 其余博主（只列所选平台的）‖ 换一批 · 固定 · 作品数
        RowLayout {
            Layout.fillWidth: true
            spacing: 4
            readonly property bool canMatrix: view.platforms.some(function (p) { return p !== "MV" }) && view.platform !== "MV" && view.channel.indexOf("group:MV/") !== 0
            Chip { text: "浏览"; selected: true; visible: parent.canMatrix }
            Chip { text: view.matrixWindowOpen ? "多联放映（窗口）" : "多联放映"; visible: parent.canMatrix; onClicked: if (view.matrixWindowOpen) view.matrixWindowRequested(); else view.mode = "matrix" }
            Divider { Layout.leftMargin: 2; Layout.rightMargin: 2; visible: parent.canMatrix }
            ComboBox {
                visible: view.platforms.length > 1
                implicitHeight: 28; Layout.preferredWidth: 110; font.pixelSize: 12
                model: ["全部平台"].concat(view.platforms)
                currentIndex: Math.max(0, view.platforms.indexOf(view.platform) + 1)
                onActivated: function (i) { view.pickPlatform(i === 0 ? "" : view.platforms[i - 1]) }
                ToolTip.visible: hovered; ToolTip.text: "平台"
            }
            ChannelBar {
                Layout.fillWidth: true
                Layout.preferredHeight: 28
                feed: feed
                channel: view.channel
                platform: view.platform
                onPicked: function (ch) { view.channel = ch }
            }
            Divider { Layout.leftMargin: 2; Layout.rightMargin: 2 }
            Chip { text: "换一批"; visible: view.channel === "recommend"; onClicked: feed.reshuffle() }
            Chip {
                readonly property string group: view.channel.indexOf("group:") === 0 ? view.channel.slice(6) : ""
                readonly property bool isPinned: feed.pins.indexOf(group) >= 0
                visible: group !== ""
                text: isPinned ? "取消固定" : "📌 固定"
                selected: isPinned
                onClicked: feed.setPinned(group, !isPinned)
                ToolTip.visible: hovered; ToolTip.text: isPinned ? "取消固定这个博主" : "把这个博主固定在频道条前面"
            }
            Label {
                text: view.error || paged.error || (paged.total > 0 ? paged.total + " 个" : (paged.loading ? "" : "没有作品"))
                color: view.error || paged.error ? "#f85149" : "#8b949e"
                font.pixelSize: 12
            }
        }

        GridView {
            WheelScroll { }
            id: grid
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            readonly property bool isLandscape: view.platform === "MV" || view.channel.indexOf("group:MV/") === 0
            readonly property int columns: Math.max(2, Math.round(width / (isLandscape ? 260 : 210)))
            cellWidth: Math.floor(width / columns)
            cellHeight: cellWidth * (isLandscape ? 9 / 16 : 16 / 9) + 42
            model: paged.total
            cacheBuffer: cellHeight * 2
            ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }
            delegate: Item {
                required property int index
                readonly property var modelData: { paged.revision; return paged.item(index) || {id: "", kind: "", title: "", published: "", creator_id: "/", has_thumbnail: false, width: 0, height: 0} }
                width: grid.cellWidth
                height: grid.cellHeight
                Rectangle {
                    anchors.fill: parent
                    anchors.margins: 3
                    color: "#161b22"
                    border.color: tap.pressed ? "#58a6ff" : "#30363d"
                    radius: 6
                    clip: true
                    Image {
                        id: thumb
                        width: parent.width
                        height: width * (grid.isLandscape ? 9 / 16 : 16 / 9)
                        fillMode: (!grid.isLandscape && modelData.width > modelData.height && modelData.height > 0) ? Image.PreserveAspectFit : Image.PreserveAspectCrop
                        asynchronous: true
                        sourceSize.width: 360
                        source: modelData.has_thumbnail ? view.apiQuery("/api/media/shortvideo/thumbnail", modelData) : ""
                    }
                    Label {
                        anchors.centerIn: thumb
                        visible: !modelData.has_thumbnail
                        text: modelData.kind === "album" ? "🖼" : "▶"
                        font.pixelSize: 40; color: "#30363d"
                    }
                    Label {
                        anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 4
                        padding: 3
                        text: modelData.kind === "album" ? "图集 " + modelData.image_count : "视频"
                        color: "white"; font.pixelSize: 12
                        background: Rectangle { color: "#99000000"; radius: 3 }
                    }
                    Label {
                        anchors.left: parent.left; anchors.top: parent.top; anchors.margins: 4
                        visible: view.isLiked(modelData)
                        text: "♥"; color: "#f778ba"; font.pixelSize: 16
                    }
                    Column {
                        anchors.top: thumb.bottom; anchors.left: parent.left; anchors.right: parent.right; anchors.margins: 4; anchors.topMargin: 3
                        Label { width: parent.width; text: modelData.title; elide: Text.ElideRight; color: "#c9d1d9"; font.pixelSize: 12 }
                        Label { width: parent.width; text: modelData.creator_id.split("/").slice(1).join(" / ") + " · " + modelData.published; elide: Text.ElideRight; color: "#8b949e"; font.pixelSize: 11 }
                    }
                    TapHandler { id: tap; onTapped: if (modelData.id) view.openIndex = index }
                }
            }
        }
        BusyIndicator { running: paged.loading; visible: running; Layout.alignment: Qt.AlignHCenter }
    }

    Loader {
        anchors.fill: parent
        active: view.mode === "matrix" && !view.matrixWindowOpen
        sourceComponent: Component {
            MatrixView {
                audioEngine: view.audioEngine
                onExitRequested: view.mode = "browse"
                // 先销毁内嵌的这份（记下位置），再由主窗口打开独立窗口。
                onDetachRequested: { view.mode = "browse"; Qt.callLater(view.matrixWindowRequested) }
            }
        }
        onLoaded: item.forceActiveFocus()
    }

    Loader {
        anchors.fill: parent
        active: view.openIndex >= 0   // 列表刷新时总数会短暂为 0，不能据此销毁查看界面
        sourceComponent: Component { Viewer { post: view.postAt(view.openIndex) } }
    }

    // Viewer：全屏查看一个作品。←/→ 切作品，↑/↓ 翻图，空格暂停，M 静音，Esc 返回。
    // 点画面：左边三分之一上一个、右边三分之一下一个（图集先翻图片，翻到头再换作品），中间暂停/继续（图集翻下一张）；
    // 拖动/滑动：左滑下一个、右滑上一个、下滑返回。底部控件条不在点按区域里。
    component Viewer: Rectangle {
        id: viewer
        required property var post   // 所在页未到达时为 null
        property int page: 1
        readonly property bool isVideo: post?.kind === "video"
        color: "black"
        focus: true
        // 行为上报：展示即 view；离开时按停留与播放进度判断 skip（2 秒内划走）/ complete（看完或 80%）/ dwell（停留 8 秒以上）。
        property var shown: null
        property double shownAt: 0
        property int pagesSeen: 1
        function finish() {
            if (!shown) return
            var ms = Date.now() - shownAt
            var type = ""
            if (shown.kind === "video" && video.duration > 0 && (video.position >= video.duration * 0.8)) type = "complete"
            else if (ms < 2000) type = "skip"
            else if (ms >= 8000 || pagesSeen >= 3) type = "dwell"
            if (type) feed.event(shown, type)
            shown = null
        }
        onPostChanged: {
            finish()
            page = 1
            pagesSeen = 1
            if (post) { shown = post; shownAt = Date.now(); feed.event(post, "view") }
        }
        onPageChanged: pagesSeen = Math.max(pagesSeen, page)
        Component.onDestruction: finish()
        property bool trashArmed: false
        Timer { id: trashDisarm; interval: 3000; onTriggered: viewer.trashArmed = false }
        // 3 秒内点两次（或按两次 Delete）。完成后留在查看界面，同一位置换成下一个作品。
        property string notice: ""
        Timer { id: noticeTimer; interval: 2500; onTriggered: viewer.notice = "" }
        function say(text) { notice = text; noticeTimer.restart() }
        function trash() {
            if (!trashArmed) { trashArmed = true; trashDisarm.restart(); say("再点一次移到回收站"); return }
            trashArmed = false
            var target = post
            shown = null
            video.source = ""
            feed.trash(target, function () { paged.reset(); viewer.say("已移到系统回收站") })
        }
        function next() { if (view.openIndex < paged.total - 1) view.openIndex++ }
        function prev() { if (view.openIndex > 0) view.openIndex-- }
        Keys.onPressed: function (event) {
            if (event.key === Qt.Key_Escape || event.key === Qt.Key_Back) view.openIndex = -1
            else if (event.key === Qt.Key_Right) next()
            else if (event.key === Qt.Key_Left) prev()
            else if (event.key === Qt.Key_Down) page = Math.min(post?.image_count ?? 1, page + 1)
            else if (event.key === Qt.Key_Up) page = Math.max(1, page - 1)
            else if (event.key === Qt.Key_Space && isVideo) video.playing ? video.pause() : video.play()
            else if (event.key === Qt.Key_M) viewPrefs.muted = !viewPrefs.muted
            else if (event.key === Qt.Key_Z && post) view.toggleLike(post)
            else if (event.key === Qt.Key_Delete && post) trash()
            else return
            event.accepted = true
        }

        MpvItem {
            id: video
            anchors.fill: parent
            anchors.bottomMargin: 0
            visible: viewer.isVideo
            source: viewer.isVideo ? view.localFile(viewer.post, viewer.post.id) : ""
            volume: viewPrefs.muted ? 0 : viewPrefs.volume
            onPlaybackEnded: { feed.event(viewer.post, "complete"); viewer.shown = null; viewer.next() }
        }
        Image {
            anchors.fill: parent
            anchors.bottomMargin: 0
            visible: !viewer.isVideo && !!viewer.post
            fillMode: Image.PreserveAspectFit
            asynchronous: true
            source: viewer.post && !viewer.isVideo ? view.apiQuery("/api/media/shortvideo/album/image", viewer.post, "&index=" + viewer.page) : ""
            BusyIndicator { anchors.centerIn: parent; running: parent.status === Image.Loading }
        }
        // 图集背景音乐：音频不需要画面，用 1 像素透明播放器播放磁盘上的 mp3。
        MpvItem {
            width: 1; height: 1; opacity: 0
            source: viewer.post && !viewer.isVideo && viewer.post.music ? view.localFile(viewer.post, viewer.post.music) : ""
            volume: viewPrefs.muted ? 0 : viewPrefs.volume
            onPlaybackEnded: restart()
        }
        Item {
            id: gestures
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            anchors.bottom: parent.bottom; anchors.bottomMargin: 92
            property point drag: Qt.point(0, 0)
            function prevPage() { if (viewer.page > 1) viewer.page--; else viewer.prev() }
            function nextPage() { if (viewer.page < (viewer.post?.image_count ?? 1)) viewer.page++; else viewer.next() }
            TapHandler {
                onTapped: function (point) {
                    var x = point.position.x / gestures.width
                    if (x < 1 / 3) { if (viewer.isVideo) viewer.prev(); else gestures.prevPage() }
                    else if (x > 2 / 3) { if (viewer.isVideo) viewer.next(); else gestures.nextPage() }
                    else if (viewer.isVideo) { if (video.playing) video.pause(); else video.play() }
                    else gestures.nextPage()
                }
            }
            DragHandler {
                target: null
                onActiveTranslationChanged: gestures.drag = activeTranslation
                onActiveChanged: if (!active) {
                    var dx = gestures.drag.x, dy = gestures.drag.y
                    if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.2) { if (dx < 0) viewer.next(); else viewer.prev() }
                    else if (dy > 90 && dy > Math.abs(dx) * 1.2) view.openIndex = -1
                    gestures.drag = Qt.point(0, 0)
                }
            }
        }

        // 底部渐变控件条（与外部播放器同一套控件）
        Item {
            anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
            height: 92
            Rectangle {
                anchors.fill: parent
                gradient: Gradient {
                    GradientStop { position: 0; color: "#00000000" }
                    GradientStop { position: 1; color: "#dd000000" }
                }
            }
            ColumnLayout {
                anchors.fill: parent
                anchors.leftMargin: 12; anchors.rightMargin: 12; anchors.bottomMargin: 6; anchors.topMargin: 10
                spacing: 0
                Label {
                    Layout.fillWidth: true
                    text: viewer.post ? viewer.post.creator_id.split("/").slice(1).join(" / ") + " · " + viewer.post.published + " · " + viewer.post.title : "加载中…"
                    elide: Text.ElideRight; color: "white"; font.pixelSize: 13
                }
                SeekBar {
                    Layout.fillWidth: true
                    visible: viewer.isVideo
                    from: 0; to: Math.max(1, video.duration); value: video.position
                    onMoved: video.position = value
                }
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 2
                    IconButton { glyph: "back"; onClicked: view.openIndex = -1; ToolTip.text: "返回（Esc）" }
                    IconButton { glyph: "prev"; enabled: view.openIndex > 0; onClicked: viewer.prev(); ToolTip.text: "上一个（←）" }
                    IconButton { visible: viewer.isVideo; glyph: video.playing ? "pause" : "play"; size: 42; onClicked: video.playing ? video.pause() : video.play() }
                    IconButton { glyph: "next"; enabled: view.openIndex < paged.total - 1; onClicked: viewer.next(); ToolTip.text: "下一个（→）" }
                    Label {
                        visible: viewer.isVideo; color: "#e6edf3"; font.pixelSize: 13; Layout.leftMargin: 8
                        text: Math.floor(video.position / 60) + ":" + ("0" + Math.floor(video.position % 60)).slice(-2) + " / " + Math.floor(video.duration / 60) + ":" + ("0" + Math.floor(video.duration % 60)).slice(-2)
                    }
                    Item { Layout.fillWidth: true }
                    IconButton {
                        visible: viewer.isVideo || !!viewer.post?.music
                        glyph: viewPrefs.muted || viewPrefs.volume === 0 ? "mute" : "volume"
                        onClicked: viewPrefs.muted = !viewPrefs.muted
                        ToolTip.text: viewPrefs.muted ? "取消静音（M）" : "静音（M），之后的作品也静音"
                    }
                    SeekBar {
                        visible: viewer.isVideo || !!viewer.post?.music
                        Layout.preferredWidth: 90
                        from: 0; to: 100; value: viewPrefs.muted ? 0 : viewPrefs.volume
                        onMoved: { viewPrefs.volume = value; viewPrefs.muted = value === 0 }
                    }
                    Label { text: video.error; visible: viewer.isVideo && text.length > 0; color: "#f85149" }
                    IconButton {
                        glyph: view.isLiked(viewer.post) ? "favorite-fill" : "favorite"
                        enabled: !!viewer.post
                        onClicked: view.toggleLike(viewer.post)
                        ToolTip.text: view.isLiked(viewer.post) ? "取消点赞（Z）" : "点赞（Z）"
                    }
                    Label {
                        visible: viewer.notice !== ""
                        text: viewer.notice
                        color: viewer.trashArmed ? "#ff7b72" : "#3fb950"; font.pixelSize: 13; font.bold: viewer.trashArmed
                    }
                    IconButton {
                        glyph: "delete"; checked: viewer.trashArmed
                        enabled: !!viewer.post
                        onClicked: viewer.trash()
                        ToolTip.text: "移到系统回收站（Delete 按两次）"
                    }
                    IconButton { visible: !viewer.isVideo; glyph: "chevron_left"; enabled: viewer.page > 1; onClicked: viewer.page--; ToolTip.text: "上一张（↑）" }
                    Label { visible: !viewer.isVideo; color: "white"; font.pixelSize: 13; text: viewer.page + " / " + (viewer.post?.image_count ?? 0) }
                    IconButton { visible: !viewer.isVideo; glyph: "chevron_right"; enabled: viewer.page < (viewer.post?.image_count ?? 1); onClicked: viewer.page++; ToolTip.text: "下一张（↓）" }
                    IconButton { visible: !viewer.isVideo && !!viewer.post?.music; glyph: "music"; ToolTip.text: "背景音乐播放中" }
                }
            }
        }
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore
import omniHub.Media 1.0

// 短视频浏览：平台 → 博主 → 作品网格；视频用内嵌 mpv 直接播放磁盘文件，图集经本机服务读取（原目录或 albums.db）。
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
    Settings { id: viewPrefs; category: "shortvideo"; property string mode: "browse"; property string channel: "recommend" }
    property alias channel: viewPrefs.channel

    // 推荐、点赞、回收站、行为上报与频道（博主分组）——与相册共用 FeedApi。
    FeedApi { id: feed; domain: "shortvideo"; onErrorChanged: view.error = error }
    readonly property var platformFilters: {
        var seen = {}, out = []
        feed.groups.forEach(function (g) { if (!seen[g.platform]) { seen[g.platform] = true; out.push({channel: "tag:platform:" + g.platform, label: g.platform}) } })
        return out
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
    PagedModel { id: paged; endpoint: view.mode === "browse" ? feed.itemsPath(view.channel) : "" }

    Component.onCompleted: {
        backendApi.refreshLibraries()
        feed.loadGroups()
    }


    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 6
        visible: view.openIndex < 0 && view.mode === "browse"

        // 第一行：模式 | 平台 | 作品数
        RowLayout {
            Layout.fillWidth: true
            spacing: 4
            Chip { text: "浏览"; selected: true }
            Chip { text: view.matrixWindowOpen ? "多联放映（窗口）" : "多联放映"; onClicked: if (view.matrixWindowOpen) view.matrixWindowRequested(); else view.mode = "matrix" }
            Item { Layout.fillWidth: true }
            Chip { text: "换一批"; visible: view.channel === "recommend"; onClicked: feed.reshuffle() }
            Chip {
                readonly property string group: view.channel.indexOf("group:") === 0 ? view.channel.slice(6) : ""
                readonly property bool isPinned: feed.pins.indexOf(group) >= 0
                visible: group !== ""
                text: isPinned ? "取消固定博主" : "📌 固定这个博主"
                selected: isPinned
                onClicked: feed.setPinned(group, !isPinned)
            }
            Label {
                text: view.error || paged.error || (paged.total > 0 ? paged.total + " 个作品" : (paged.loading ? "" : "没有作品，需先由管理员扫描建立索引"))
                color: view.error || paged.error ? "#f85149" : "#8b949e"
                font.pixelSize: 12
            }
        }
        // 第二行：推荐 | 我的点赞 | 平台 | 📌 固定的博主 | 其余博主
        ChannelBar {
            Layout.fillWidth: true
            Layout.preferredHeight: 28
            feed: feed
            channel: view.channel
            filters: view.platformFilters
            onPicked: function (ch) { view.channel = ch }
        }

        GridView {
            WheelScroll { }
            id: grid
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            readonly property int columns: Math.max(2, Math.round(width / 210))
            cellWidth: Math.floor(width / columns)
            cellHeight: cellWidth * 16 / 9 + 42
            model: paged.total
            cacheBuffer: cellHeight * 2
            ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }
            delegate: Item {
                required property int index
                readonly property var modelData: { paged.revision; return paged.item(index) || {id: "", kind: "", title: "", published: "", creator_id: "/", has_thumbnail: false} }
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
                        height: width * 16 / 9
                        fillMode: Image.PreserveAspectCrop
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
                        Label { width: parent.width; text: modelData.creator_id.split("/")[1] + " · " + modelData.published; elide: Text.ElideRight; color: "#8b949e"; font.pixelSize: 11 }
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
        active: view.openIndex >= 0 && view.openIndex < paged.total
        sourceComponent: Component { Viewer { post: view.postAt(view.openIndex) } }
    }

    // Viewer：全屏查看一个作品。←/→ 切作品，↑/↓ 翻图，空格暂停，Esc 返回。
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
        function trash() {
            if (!trashArmed) { trashArmed = true; trashDisarm.restart(); return }
            trashArmed = false
            var target = post
            shown = null
            video.source = ""
            feed.trash(target, function () { paged.reset(); if (view.openIndex >= paged.total) view.openIndex = -1 })
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
            else if (event.key === Qt.Key_L && post) view.toggleLike(post)
            else return
            event.accepted = true
        }

        MpvItem {
            id: video
            anchors.fill: parent
            anchors.bottomMargin: 0
            visible: viewer.isVideo
            source: viewer.isVideo ? view.localFile(viewer.post, viewer.post.id) : ""
            onPlaybackEnded: { feed.event(viewer.post, "complete"); viewer.shown = null; viewer.next() }
            TapHandler { onTapped: video.playing ? video.pause() : video.play() }
        }
        Image {
            anchors.fill: parent
            anchors.bottomMargin: 0
            visible: !viewer.isVideo && !!viewer.post
            fillMode: Image.PreserveAspectFit
            asynchronous: true
            source: viewer.post && !viewer.isVideo ? view.apiQuery("/api/media/shortvideo/album/image", viewer.post, "&index=" + viewer.page) : ""
            TapHandler { onTapped: viewer.page = viewer.page < (viewer.post?.image_count ?? 1) ? viewer.page + 1 : 1 }
            BusyIndicator { anchors.centerIn: parent; running: parent.status === Image.Loading }
        }
        // 图集背景音乐：音频不需要画面，用 1 像素透明播放器播放磁盘上的 mp3。
        MpvItem {
            width: 1; height: 1; opacity: 0
            source: viewer.post && !viewer.isVideo && viewer.post.music ? view.localFile(viewer.post, viewer.post.music) : ""
            onPlaybackEnded: restart()
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
                    text: viewer.post ? viewer.post.creator_id.split("/")[1] + " · " + viewer.post.published + " · " + viewer.post.title : "加载中…"
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
                    Label { text: video.error; visible: viewer.isVideo && text.length > 0; color: "#f85149" }
                    IconButton {
                        glyph: view.isLiked(viewer.post) ? "favorite-fill" : "favorite"
                        enabled: !!viewer.post
                        onClicked: view.toggleLike(viewer.post)
                        ToolTip.text: view.isLiked(viewer.post) ? "取消点赞（L）" : "点赞（L）"
                    }
                    IconButton {
                        glyph: "delete"; checked: viewer.trashArmed
                        enabled: !!viewer.post
                        onClicked: viewer.trash()
                        ToolTip.text: viewer.trashArmed ? "再点一次：移到系统回收站" : "移到回收站"
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

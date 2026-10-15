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
    property var creators: []
    property string platform: ""
    property string creator: ""
    property string error: ""
    property int openIndex: -1
    property alias mode: viewPrefs.mode   // browse | matrix，记住上次的模式
    Settings { id: viewPrefs; category: "shortvideo"; property string mode: "browse" }

    readonly property var platforms: creators.map(function (c) { return c.platform }).filter(function (p, i, a) { return a.indexOf(p) === i })
    readonly property var shownCreators: creators.filter(function (c) { return !platform || c.platform === platform })

    function getJSON(path, done) {
        var url = backendApi.apiUrl(path)
        if (!url) { view.error = "无法连接 omniHub 服务"; return }
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function () {
            if (xhr.readyState !== XMLHttpRequest.DONE || !view) return   // 页面已销毁
            if (xhr.status !== 200) { view.error = "请求失败（" + xhr.status + "）：" + xhr.responseText; return }
            view.error = ""
            done(JSON.parse(xhr.responseText), xhr)
        }
        xhr.open("GET", url)
        xhr.send()
    }
    function apiQuery(path, post, extra) {
        var q = "library_id=" + encodeURIComponent(post.library_id) + "&id=" + encodeURIComponent(post.id)
        return backendApi.apiUrl(path + "?" + q + (extra || ""))
    }
    function libraryRoot(id) {
        var libs = backendApi.libraries
        for (var i = 0; i < libs.length; i++) if (libs[i].id === id) return libs[i].root_path
        return ""
    }
    // 磁盘文件的绝对路径：资源库根目录 + 分类目录 + 作品相对路径。
    function localFile(post, rel) {
        var root = libraryRoot(post.library_id)
        return root ? root + "/media_library/shortvideo/" + rel : ""
    }
    function postsEndpoint() {
        if (creator) return "/api/media/shortvideo/posts?creator=" + encodeURIComponent(creator)
        if (platform) return "/api/media/shortvideo/posts?platform=" + encodeURIComponent(platform)
        return "/api/media/shortvideo/posts"
    }
    function postAt(i) { paged.revision; return paged.item(i) }

    PagedModel { id: paged; endpoint: view.postsEndpoint() }

    Component.onCompleted: {
        backendApi.refreshLibraries()
        getJSON("/api/media/shortvideo/creators", function (list) { creators = list })
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
            Chip { text: "多联放映"; onClicked: view.mode = "matrix" }
            Rectangle { width: 1; height: 20; color: "#30363d"; Layout.leftMargin: 4; Layout.rightMargin: 4 }
            Repeater {
                model: [""].concat(view.platforms)
                delegate: Chip {
                    required property string modelData
                    text: modelData || "全部平台"
                    selected: view.platform === modelData
                    onClicked: { view.platform = modelData; view.creator = "" }
                }
            }
            Item { Layout.fillWidth: true }
            Label {
                text: view.error || paged.error || (paged.total > 0 ? paged.total + " 个作品" : (paged.loading ? "" : "没有作品，需先由管理员扫描建立索引"))
                color: view.error || paged.error ? "#f85149" : "#8b949e"
                font.pixelSize: 12
            }
        }
        // 第二行：博主（横向滚动）
        ListView {
            Layout.fillWidth: true
            Layout.preferredHeight: 28
            orientation: ListView.Horizontal
            spacing: 4
            clip: true
            model: [{id: "", name: "全部博主", posts: -1}].concat(view.shownCreators)
            delegate: Chip {
                required property var modelData
                text: modelData.posts < 0 ? modelData.name : modelData.name + " · " + modelData.posts
                selected: view.creator === modelData.id
                onClicked: view.creator = modelData.id
            }
        }

        GridView {
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
        active: view.mode === "matrix" && view.creators.length > 0
        sourceComponent: Component { MatrixView { creators: view.creators; shortVideo: view; onExitRequested: view.mode = "browse" } }
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
        onPostChanged: page = 1
        function next() { if (view.openIndex < paged.total - 1) view.openIndex++ }
        function prev() { if (view.openIndex > 0) view.openIndex-- }
        Keys.onPressed: function (event) {
            if (event.key === Qt.Key_Escape || event.key === Qt.Key_Back) view.openIndex = -1
            else if (event.key === Qt.Key_Right) next()
            else if (event.key === Qt.Key_Left) prev()
            else if (event.key === Qt.Key_Down) page = Math.min(post?.image_count ?? 1, page + 1)
            else if (event.key === Qt.Key_Up) page = Math.max(1, page - 1)
            else if (event.key === Qt.Key_Space && isVideo) video.playing ? video.pause() : video.play()
            else return
            event.accepted = true
        }

        MpvItem {
            id: video
            anchors.fill: parent
            anchors.bottomMargin: 96
            visible: viewer.isVideo
            source: viewer.isVideo ? view.localFile(viewer.post, viewer.post.id) : ""
            onPlaybackEnded: viewer.next()
            TapHandler { onTapped: video.playing ? video.pause() : video.play() }
        }
        Image {
            anchors.fill: parent
            anchors.bottomMargin: 96
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

        ColumnLayout {
            anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
            anchors.margins: 12
            Label {
                Layout.fillWidth: true
                text: viewer.post ? viewer.post.creator_id.split("/")[1] + " · " + viewer.post.published + " · " + viewer.post.title : "加载中…"
                elide: Text.ElideRight; color: "white"
            }
            RowLayout {
                Layout.fillWidth: true
                Button { text: "返回"; onClicked: view.openIndex = -1 }
                Button { text: "上一个"; enabled: view.openIndex > 0; onClicked: viewer.prev() }
                Button { text: "下一个"; enabled: view.openIndex < paged.total - 1; onClicked: viewer.next() }
                Button { visible: viewer.isVideo; text: video.playing ? "暂停" : "播放"; onClicked: video.playing ? video.pause() : video.play() }
                Slider {
                    visible: viewer.isVideo
                    Layout.fillWidth: true
                    from: 0; to: Math.max(1, video.duration)
                    value: video.position
                    onMoved: video.position = value
                }
                Label { visible: viewer.isVideo; color: "white"; text: Math.floor(video.position) + " / " + Math.floor(video.duration) + " 秒" }
                Item { visible: !viewer.isVideo; Layout.fillWidth: true }
                Button { visible: !viewer.isVideo; text: "上一张"; enabled: viewer.page > 1; onClicked: viewer.page-- }
                Label { visible: !viewer.isVideo; color: "white"; text: viewer.page + " / " + (viewer.post?.image_count ?? 0) }
                Button { visible: !viewer.isVideo; text: "下一张"; enabled: viewer.page < (viewer.post?.image_count ?? 1); onClicked: viewer.page++ }
            }
            Label { text: video.error; visible: viewer.isVideo && text.length > 0; color: "#f85149" }
        }
    }
}

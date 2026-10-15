import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import omniHub.Media 1.0

// 短视频浏览：平台 → 博主 → 作品网格；视频用内嵌 mpv 直接播放磁盘文件，图集经本机服务读取（原目录或 albums.db）。
// 页面隐藏时由 Main.qml 的 Loader 销毁，解码器与图片缓存随之释放。
Item {
    id: view
    readonly property int pageSize: 60
    property var creators: []
    property string platform: ""
    property string creator: ""
    property var posts: []
    property bool hasMore: false
    property bool loading: false
    property string error: ""
    property int openIndex: -1

    readonly property var platforms: creators.map(function (c) { return c.platform }).filter(function (p, i, a) { return a.indexOf(p) === i })
    readonly property var shownCreators: creators.filter(function (c) { return !platform || c.platform === platform })

    function getJSON(path, done) {
        var url = backendApi.apiUrl(path)
        if (!url) { error = "无法连接 omniHub 服务"; return }
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function () {
            if (xhr.readyState !== XMLHttpRequest.DONE) return
            if (xhr.status !== 200) { error = "请求失败（" + xhr.status + "）：" + xhr.responseText; loading = false; return }
            error = ""
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
    function load(offset) {
        loading = true
        var q = "/api/media/shortvideo/posts?limit=" + pageSize + "&offset=" + offset
        if (creator) q += "&creator=" + encodeURIComponent(creator)
        else if (platform) q += "&platform=" + encodeURIComponent(platform)
        getJSON(q, function (page, xhr) {
            posts = offset === 0 ? page : posts.concat(page)
            hasMore = xhr.getResponseHeader("X-Has-More") === "true"
            loading = false
        })
    }
    function reload() { posts = []; openIndex = -1; load(0) }

    Component.onCompleted: {
        backendApi.refreshLibraries()
        getJSON("/api/media/shortvideo/creators", function (list) { creators = list })
        load(0)
    }

    component Chip: Button {
        id: chip
        property bool selected: false
        font.pixelSize: 14
        contentItem: Label { text: chip.text; font: chip.font; color: chip.selected ? "#58a6ff" : "#c9d1d9"; horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter }
        background: Rectangle { radius: 16; color: chip.selected ? "#1f2a3a" : "#161b22"; border.color: chip.selected ? "#58a6ff" : "#30363d" }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 12
        spacing: 8
        visible: view.openIndex < 0

        ListView {
            Layout.fillWidth: true
            Layout.preferredHeight: 40
            orientation: ListView.Horizontal
            spacing: 6
            model: [""].concat(view.platforms)
            delegate: Chip {
                required property string modelData
                text: modelData || "全部平台"
                selected: view.platform === modelData
                onClicked: { view.platform = modelData; view.creator = ""; view.reload() }
            }
        }
        ListView {
            Layout.fillWidth: true
            Layout.preferredHeight: 40
            orientation: ListView.Horizontal
            spacing: 6
            clip: true
            model: [{id: "", name: "全部博主", posts: -1}].concat(view.shownCreators)
            delegate: Chip {
                required property var modelData
                text: modelData.posts < 0 ? modelData.name : modelData.name + " · " + modelData.posts
                selected: view.creator === modelData.id
                onClicked: { view.creator = modelData.id; view.reload() }
            }
        }
        Label { text: view.error; visible: text.length > 0; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true }
        Label {
            text: "没有作品。资源库需要先由管理员扫描建立索引。"
            visible: !view.loading && view.posts.length === 0 && !view.error
            color: "#8b949e"
        }

        GridView {
            id: grid
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            readonly property int columns: Math.max(2, Math.round(width / 210))
            cellWidth: Math.floor(width / columns)
            cellHeight: cellWidth * 16 / 9 + 56
            model: view.posts
            cacheBuffer: cellHeight * 2
            onAtYEndChanged: if (atYEnd && view.hasMore && !view.loading) view.load(view.posts.length)
            ScrollBar.vertical: ScrollBar {}
            delegate: Item {
                required property var modelData
                required property int index
                width: grid.cellWidth
                height: grid.cellHeight
                Rectangle {
                    anchors.fill: parent
                    anchors.margins: 4
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
                        anchors.top: thumb.bottom; anchors.left: parent.left; anchors.right: parent.right; anchors.margins: 6
                        Label { width: parent.width; text: modelData.title; elide: Text.ElideRight; color: "#c9d1d9"; font.pixelSize: 13 }
                        Label { width: parent.width; text: modelData.creator_id.split("/")[1] + " · " + modelData.published; elide: Text.ElideRight; color: "#8b949e"; font.pixelSize: 12 }
                    }
                    TapHandler { id: tap; onTapped: view.openIndex = index }
                }
            }
        }
        BusyIndicator { running: view.loading; visible: running; Layout.alignment: Qt.AlignHCenter }
    }

    Loader {
        anchors.fill: parent
        active: view.openIndex >= 0 && view.openIndex < view.posts.length
        sourceComponent: Component { Viewer { post: view.posts[view.openIndex] } }
    }

    // Viewer：全屏查看一个作品。←/→ 切作品，↑/↓ 翻图，空格暂停，Esc 返回。
    component Viewer: Rectangle {
        id: viewer
        required property var post
        property int page: 1
        readonly property bool isVideo: post.kind === "video"
        color: "black"
        focus: true
        onPostChanged: page = 1
        function next() {
            if (view.openIndex < view.posts.length - 1) view.openIndex++
            else if (view.hasMore && !view.loading) view.load(view.posts.length)
        }
        function prev() { if (view.openIndex > 0) view.openIndex-- }
        Keys.onPressed: function (event) {
            if (event.key === Qt.Key_Escape || event.key === Qt.Key_Back) view.openIndex = -1
            else if (event.key === Qt.Key_Right) next()
            else if (event.key === Qt.Key_Left) prev()
            else if (event.key === Qt.Key_Down) page = Math.min(post.image_count, page + 1)
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
            visible: !viewer.isVideo
            fillMode: Image.PreserveAspectFit
            asynchronous: true
            source: viewer.isVideo ? "" : view.apiQuery("/api/media/shortvideo/album/image", viewer.post, "&index=" + viewer.page)
            TapHandler { onTapped: viewer.page = viewer.page < viewer.post.image_count ? viewer.page + 1 : 1 }
            BusyIndicator { anchors.centerIn: parent; running: parent.status === Image.Loading }
        }
        // 图集背景音乐：音频不需要画面，用 1 像素透明播放器播放磁盘上的 mp3。
        MpvItem {
            width: 1; height: 1; opacity: 0
            source: !viewer.isVideo && viewer.post.music ? view.localFile(viewer.post, viewer.post.music) : ""
            onPlaybackEnded: restart()
        }

        ColumnLayout {
            anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
            anchors.margins: 12
            Label {
                Layout.fillWidth: true
                text: viewer.post.creator_id.split("/")[1] + " · " + viewer.post.published + " · " + viewer.post.title
                elide: Text.ElideRight; color: "white"
            }
            RowLayout {
                Layout.fillWidth: true
                Button { text: "返回"; onClicked: view.openIndex = -1 }
                Button { text: "上一个"; enabled: view.openIndex > 0; onClicked: viewer.prev() }
                Button { text: "下一个"; enabled: view.openIndex < view.posts.length - 1 || view.hasMore; onClicked: viewer.next() }
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
                Label { visible: !viewer.isVideo; color: "white"; text: viewer.page + " / " + viewer.post.image_count }
                Button { visible: !viewer.isVideo; text: "下一张"; enabled: viewer.page < viewer.post.image_count; onClicked: viewer.page++ }
            }
            Label { text: video.error; visible: viewer.isVideo && text.length > 0; color: "#f85149" }
        }
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 音声页：左栏「继续收听」与专辑列表，右栏所选专辑的分集（可搜索、按页懒加载，显示各集断点）。
// 播放交给全局 AudioEngine，离开本页不中断。
Item {
    id: view
    required property var engine
    property var albums: []
    property var recent: []
    property string error: ""
    // 其它页面（如下载完成后的「去收听」）要求打开并播放的专辑。
    property string request: ""
    property bool pendingPlay: false
    readonly property var selected: albums.find(function (a) { return a.id === prefs.album }) || null

    Settings { id: prefs; category: "audioView"; property string album: ""; property string query: "" }
    ApiClient { id: api; onErrorChanged: view.error = error }

    function refresh() {
        api.getJSON("/api/media/audio/albums?limit=100", function (list) {
            view.albums = list
            if (!view.selected && list.length) prefs.album = list[0].id
            if (view.pendingPlay && view.selected) { view.pendingPlay = false; view.continueAlbum(view.selected) }
        })
        api.getJSON("/api/media/audio/recent?limit=6", function (list) { view.recent = list })
    }
    Component.onCompleted: {
        if (request) { prefs.album = request; pendingPlay = true }
        refresh()
    }
    property bool devStarted: false
    // 开发验证：devScroll < 0 时自动「继续收听」所选专辑，每 -devScroll 毫秒打印播放状态。
    Timer {
        interval: typeof devScroll !== "undefined" && devScroll < 0 ? -devScroll : 1000; repeat: true
        running: typeof devScroll !== "undefined" && devScroll < 0 && !!view.selected
        onTriggered: {
            if (!view.engine.active) view.continueAlbum(view.selected)
            else if (!devStarted) { devStarted = true; view.engine.player.play() }
            var p = view.engine.player
            console.log("audio-state", view.engine.position, view.engine.track ? view.engine.track.title : "-", p.playing, Math.floor(p.position), Math.floor(p.duration), view.engine.error)
        }
    }
    // 切集或暂停时断点变了：刷新「继续收听」与当前列表里的进度。
    Connections {
        target: view.engine.player
        function onPlayingChanged() { if (!view.engine.player.playing) refreshTimer.restart() }
    }
    Connections { target: view.engine; function onTrackChanged() { refreshTimer.restart() } }
    Timer {
        id: refreshTimer; interval: 1200
        onTriggered: {
            api.getJSON("/api/media/audio/recent?limit=6", function (list) { view.recent = list })
            paged.reset()
        }
    }

    // 「继续」：本专辑最近听过的那集；没有就从第一集开始。
    function continueAlbum(album) {
        var last = recent.find(function (r) { return r.album_id === album.id })
        if (last) playId(album, last.audio_id)
        else engine.open(album.id, album.title, 0, true)
    }
    // 按分集 ID 找序号：分集文件名的自然序号由服务端给出（index 从 1 起）。
    function playId(album, audioId) {
        var name = audioId.slice(audioId.lastIndexOf("/") + 1)
        var stem = name.slice(0, name.lastIndexOf("."))
        api.getJSON("/api/media/audio/tracks?path=" + encodeURIComponent(album.id) + "&limit=100&q=" + encodeURIComponent(stem), function (list) {
            var hit = list.find(function (t) { return t.id === audioId })
            view.engine.open(album.id, album.title, hit ? hit.index - 1 : 0, true)
        })
    }
    function coverUrl(logical) { return logical ? backendApi.apiUrl("/api/media/audio/cover?path=" + encodeURIComponent(logical)) : "" }

    PagedModel {
        id: paged
        pageSize: 100
        endpoint: view.selected ? "/api/media/audio/tracks?path=" + encodeURIComponent(view.selected.id) + (prefs.query ? "&q=" + encodeURIComponent(prefs.query) : "") : ""
    }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // 左栏
        Rectangle {
            Layout.preferredWidth: 280
            Layout.fillHeight: true
            color: "#0d1117"
            Rectangle { anchors.right: parent.right; width: 1; height: parent.height; color: "#30363d" }
            ListView {
                id: side
                anchors.fill: parent; anchors.margins: 6; anchors.rightMargin: 7
                clip: true
                spacing: 2
                WheelScroll { }
                ScrollBar.vertical: ScrollBar {}
                header: Column {
                    width: side.width
                    visible: view.recent.length > 0
                    height: visible ? implicitHeight : 0
                    Label { text: "继续收听"; color: "#8b949e"; font.pixelSize: 12; topPadding: 4; bottomPadding: 4; leftPadding: 4 }
                    Repeater {
                        model: view.recent
                        delegate: ItemDelegate {
                            required property var modelData
                            width: side.width
                            height: 44
                            padding: 4
                            contentItem: Column {
                                Label { width: parent.width; elide: Text.ElideRight; text: modelData.title; color: "#e6edf3"; font.pixelSize: 12 }
                                Label {
                                    width: parent.width; elide: Text.ElideRight; color: "#8b949e"; font.pixelSize: 11
                                    text: modelData.album_title + " · " + (modelData.duration > 0 && modelData.offset >= modelData.duration - 5 ? "已听完" : "听到 " + view.engine.formatTime(modelData.offset))
                                }
                            }
                            onClicked: {
                                var album = view.albums.find(function (a) { return a.id === modelData.album_id })
                                if (album) { prefs.album = album.id; view.playId(album, modelData.audio_id) }
                            }
                        }
                    }
                    Label { text: "专辑"; color: "#8b949e"; font.pixelSize: 12; topPadding: 8; bottomPadding: 4; leftPadding: 4 }
                }
                model: view.albums
                delegate: ItemDelegate {
                    required property var modelData
                    width: side.width
                    height: 52
                    padding: 4
                    highlighted: prefs.album === modelData.id
                    contentItem: RowLayout {
                        spacing: 8
                        Rectangle {
                            Layout.preferredWidth: 44; Layout.preferredHeight: 44
                            radius: 4; color: "#21262d"; clip: true
                            Image {
                                anchors.fill: parent; asynchronous: true; fillMode: Image.PreserveAspectCrop
                                sourceSize: Qt.size(88, 88)
                                source: view.coverUrl(modelData.cover_path)
                                visible: status === Image.Ready
                            }
                            Image { anchors.centerIn: parent; source: "qrc:/icons/music.svg"; sourceSize: Qt.size(22, 22); visible: !modelData.cover_path }
                        }
                        Column {
                            Layout.fillWidth: true
                            Label { width: parent.width; elide: Text.ElideRight; text: modelData.title; color: prefs.album === modelData.id ? "#58a6ff" : "#e6edf3"; font.pixelSize: 13 }
                            Label { text: modelData.track_count + " 集 · " + (modelData.library_name || ""); color: "#8b949e"; font.pixelSize: 11 }
                        }
                    }
                    onClicked: prefs.album = modelData.id
                }
            }
        }

        // 右栏：专辑分集
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            Layout.margins: 10
            spacing: 6
            RowLayout {
                spacing: 8
                Label {
                    text: view.selected ? view.selected.title : (view.error || "没有音声专辑")
                    color: view.error && !view.selected ? "#f85149" : "#e6edf3"
                    font.pixelSize: 17; font.bold: true
                    Layout.maximumWidth: 360; elide: Text.ElideRight
                }
                Label { text: view.selected ? paged.total + " 集" : ""; color: "#8b949e"; font.pixelSize: 12 }
                Chip {
                    text: view.engine.albumId === (view.selected ? view.selected.id : "") && view.engine.player.playing ? "⏸ 暂停" : "▶ 继续收听"
                    visible: !!view.selected
                    onClicked: {
                        if (view.engine.albumId === view.selected.id && view.engine.active) view.engine.togglePlay()
                        else view.continueAlbum(view.selected)
                    }
                }
                Item { Layout.fillWidth: true }
                TextField {
                    Layout.preferredWidth: 220
                    implicitHeight: 30
                    placeholderText: "搜索分集"
                    text: prefs.query
                    font.pixelSize: 13
                    onTextEdited: searchDelay.restart()
                    Timer { id: searchDelay; interval: 300; onTriggered: prefs.query = parent.text.trim() }
                }
            }
            ListView {
                id: list
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                model: paged.total
                WheelScroll { }
                ScrollBar.vertical: ScrollBar {}
                delegate: ItemDelegate {
                    id: row
                    required property int index
                    readonly property var t: { paged.revision; return paged.item(index) }
                    readonly property bool current: !!(t && view.engine.track && view.engine.track.id === t.id)
                    readonly property bool done: !!(t && t.duration > 0 && t.offset >= t.duration - 5)
                    width: list.width
                    height: 40
                    padding: 6
                    highlighted: current
                    contentItem: RowLayout {
                        spacing: 8
                        Label { text: row.t ? row.t.index : ""; color: "#6e7681"; font.pixelSize: 12; Layout.preferredWidth: 36; horizontalAlignment: Text.AlignRight }
                        Image { source: "qrc:/icons/" + (view.engine.player.playing ? "volume" : "pause") + ".svg"; sourceSize: Qt.size(16, 16); visible: row.current }
                        Label { Layout.fillWidth: true; elide: Text.ElideRight; text: row.t ? row.t.title : "…"; color: row.current ? "#58a6ff" : (row.done ? "#8b949e" : "#e6edf3"); font.pixelSize: 13 }
                        // 断点：细进度条 + 文字
                        Rectangle {
                            Layout.preferredWidth: 80; Layout.preferredHeight: 3
                            radius: 1.5; color: "#30363d"
                            visible: !!(row.t && row.t.offset > 0 && row.t.duration > 0)
                            Rectangle { height: parent.height; radius: parent.radius; color: row.done ? "#3fb950" : "#58a6ff"; width: row.t && row.t.duration > 0 ? parent.width * Math.min(1, row.t.offset / row.t.duration) : 0 }
                        }
                        Label {
                            Layout.preferredWidth: 110; horizontalAlignment: Text.AlignRight
                            color: "#8b949e"; font.pixelSize: 11
                            text: !row.t ? "" : row.done ? "已听完" : row.t.offset > 0 ? "听到 " + view.engine.formatTime(row.t.offset) : ""
                        }
                    }
                    onClicked: if (row.t) view.engine.open(view.selected.id, view.selected.title, row.t.index - 1, true)
                }
            }
            Label { visible: paged.error !== ""; text: paged.error; color: "#f85149" }
        }
    }
}

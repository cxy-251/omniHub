import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 音声页：左栏顶部切换「音乐 / 有声书」。音乐：左栏歌手，右栏 MusicPane（单曲、专辑、循环范围）；
// 有声书：左栏「继续收听」与专辑列表，右栏所选专辑的分集（可搜索、按页懒加载，显示各集断点）。
// 播放交给全局 AudioEngine，离开本页不中断。
Item {
    id: view
    required property var engine
    property var albums: []
    property var artists: []
    property string musicCollection: "all"
    property bool otherArtists: false
    readonly property var visibleArtists: artists.filter(function (a) { return !!a.main_artist !== view.otherArtists })
    readonly property bool music: prefs.section === "music"
    readonly property var selectedArtist: artists.find(function (a) { return a.id === prefs.artist }) || null
    property var recent: []
    property string error: ""
    // 其它页面（如下载完成后的「去收听」）要求打开并播放的专辑。
    property string request: ""
    property bool pendingPlay: false
    readonly property var selected: albums.find(function (a) { return a.id === prefs.album }) || null

    Settings { id: prefs; category: "audioView"; property string album: ""; property string query: ""; property string section: "music"; property string artist: "" }
    property bool unsyncedOnly: false   // 只看没有带时间轴歌词的（以后单独补）
    readonly property var lyricStatusText: ({plain: "只有纯文本歌词", search: "歌词是搜索来的，待核对", none: "查不到歌词", error: "歌词查询失败", unknown: "没查过歌词"})
    ApiClient { id: api; onErrorChanged: view.error = error }

    function refresh() {
        engine.refreshMusic()
        api.getJSON("/api/media/audio/albums?limit=100", function (list) {
            view.albums = list
            if (!view.selected && list.length) prefs.album = list[0].id
            if (view.pendingPlay && view.selected) { view.pendingPlay = false; view.continueAlbum(view.selected) }
        })
        api.getJSON("/api/media/audio/recent?limit=6", function (list) { view.recent = list })
        api.getJSON("/api/media/audio/artists", function (list) {
            view.artists = list
            if (!view.selectedArtist && list.length) prefs.artist = list[0].id
        })
    }
    Component.onCompleted: {
        if (request) { prefs.album = request; pendingPlay = true }
        refresh()
    }
    Connections { target: view.engine; function onMusicChanged(catalog) { if (catalog) view.refresh() } }
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

    // 「继续」：本专辑最近听过的那集的断点；没有就从第一集开始（逻辑在播放引擎里，多联放映伴听也用）。
    function continueAlbum(album) { engine.continueAlbum(album.id, album.title) }
    // 播放「继续收听」里的某一集：按分集 ID 找序号（index 从 1 起）。
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
        endpoint: view.selected ? "/api/media/audio/tracks?path=" + encodeURIComponent(view.selected.id) + (prefs.query ? "&q=" + encodeURIComponent(prefs.query) : "") + (view.unsyncedOnly ? "&lyrics=unsynced" : "") : ""
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
                    Row {
                        spacing: 6; bottomPadding: 6
                        Chip { text: "音乐"; selected: view.music; onClicked: prefs.section = "music" }
                        Chip { text: "有声书"; selected: !view.music; onClicked: prefs.section = "book" }
                    }
                    Column {
                        visible: view.music
                        width: side.width; spacing: 4
                        Flow {
                            width: parent.width; spacing: 4
                            Repeater {
                                model: [["all", "全部音乐"], ["liked", "我的点赞"], ["disliked", "不喜欢"]]
                                delegate: Chip { required property var modelData; text: modelData[1]; selected: view.musicCollection === modelData[0]
                                    onClicked: view.musicCollection = modelData[0] }
                            }
                            Chip { text: "回收记录"; visible: view.engine.musicCanTrash; selected: view.musicCollection === "trash"; onClicked: view.musicCollection = "trash" }
                        }
                        Row {
                            spacing: 4
                            Chip { text: "常听歌手"; selected: !view.otherArtists; onClicked: { view.otherArtists = false; view.musicCollection = "all" } }
                            Chip { text: "其他歌手"; selected: view.otherArtists; onClicked: { view.otherArtists = true; view.musicCollection = "other" } }
                        }
                    }
                    Label { visible: view.recent.length > 0; text: "继续收听"; color: "#8b949e"; font.pixelSize: 12; topPadding: 4; bottomPadding: 4; leftPadding: 4 }
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
                                // 音乐的专辑不在有声书列表里：直接按目录放
                                var album = view.albums.find(function (a) { return a.id === modelData.album_id }) || {id: modelData.album_id, title: modelData.album_title}
                                if (view.albums.indexOf(album) >= 0) prefs.album = album.id
                                view.playId(album, modelData.audio_id)
                            }
                        }
                    }
                    Label { text: view.music ? (view.otherArtists ? "其他歌手" : "常听歌手") : "专辑"; color: "#8b949e"; font.pixelSize: 12; topPadding: 8; bottomPadding: 4; leftPadding: 4 }
                }
                model: view.music ? view.visibleArtists : view.albums
                delegate: ItemDelegate {
                    required property var modelData
                    width: side.width
                    height: 52
                    padding: 4
                    readonly property bool chosen: view.music ? prefs.artist === modelData.id : prefs.album === modelData.id
                    highlighted: chosen
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
                            Label { width: parent.width; elide: Text.ElideRight; text: modelData.title; color: chosen ? "#58a6ff" : "#e6edf3"; font.pixelSize: 13 }
                            Label {
                                color: "#8b949e"; font.pixelSize: 11
                                text: view.music ? modelData.single_count + " 首单曲 · " + modelData.album_count + " 张专辑"
                                                 : modelData.track_count + " 集 · " + (modelData.library_name || "")
                            }
                        }
                    }
                    onClicked: if (view.music) { prefs.artist = modelData.id; view.musicCollection = "" } else prefs.album = modelData.id
                }
            }
        }

        // 右栏（音乐）：歌手的单曲与专辑
        MusicPane {
            visible: view.music && view.musicCollection === ""
            Layout.fillWidth: true
            Layout.fillHeight: true
            Layout.margins: 10
            engine: view.engine
            artist: view.selectedArtist
        }
        MusicCollections {
            visible: view.music && view.musicCollection !== ""
            Layout.fillWidth: true; Layout.fillHeight: true; Layout.margins: 10
            engine: view.engine
            collection: view.musicCollection || "all"
        }

        // 右栏（有声书）：专辑分集
        ColumnLayout {
            visible: !view.music
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
                Chip { text: "歌词待处理"; selected: view.unsyncedOnly; visible: !!view.selected; onClicked: view.unsyncedOnly = !view.unsyncedOnly }
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
                            visible: !!(row.t && row.t.lyrics_status)
                            text: row.t && row.t.lyrics_status ? view.lyricStatusText[row.t.lyrics_status] || row.t.lyrics_status : ""
                            color: "#e3b341"; font.pixelSize: 11
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

        // 歌词：正在播放的那首；带时间轴的跟着进度高亮、滚动，纯文本的整段显示
        Rectangle {
            Layout.preferredWidth: 320
            Layout.fillHeight: true
            visible: view.engine.lyrics.lines.length > 0
            color: "#0d1117"
            Rectangle { anchors.left: parent.left; width: 1; height: parent.height; color: "#30363d" }
            Label {
                id: lyricHead
                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 10
                elide: Text.ElideRight; color: "#8b949e"; font.pixelSize: 12
                text: (view.engine.track ? view.engine.track.title : "") + (view.engine.lyrics.synced ? "" : "（纯文本歌词）")
            }
            ListView {
                id: lyricList
                anchors.fill: parent; anchors.topMargin: lyricHead.height + 18; anchors.leftMargin: 12; anchors.rightMargin: 8; anchors.bottomMargin: 8
                clip: true
                spacing: 6
                model: view.engine.lyrics.lines
                WheelScroll { }
                ScrollBar.vertical: ScrollBar {}
                // 手动滚动后 4 秒内不自动跟随
                property double userScrolled: 0
                onMovementStarted: userScrolled = Date.now()
                readonly property int current: view.engine.lyricIndex
                onCurrentChanged: if (current >= 0 && Date.now() - userScrolled > 4000) positionViewAtIndex(current, ListView.Center)
                delegate: Label {
                    required property var modelData
                    required property int index
                    width: lyricList.width - 10
                    wrapMode: Text.Wrap
                    text: modelData.text || " "
                    font.pixelSize: index === view.engine.lyricIndex ? 15 : 13
                    color: !view.engine.lyrics.synced ? "#c9d1d9" : index === view.engine.lyricIndex ? "#58a6ff" : "#8b949e"
                    TapHandler { enabled: view.engine.lyrics.synced; onTapped: view.engine.player.position = modelData.time }
                }
            }
        }
    }
}

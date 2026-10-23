import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 音乐页右栏：一个歌手的单曲与专辑；点专辑进去是专辑里的歌。
// 播放范围：点任何一首都按「歌手播放列表」（单曲在前，再接各张专辑）放，播完接着下一首单曲或下一张专辑；
// 「循环这个歌手」「循环这张专辑」把范围定在歌手或专辑并切到列表循环。单曲循环、随机用播放条上的模式按钮。
ColumnLayout {
    id: pane
    required property var engine
    property var artist: null            // {id, title}
    property var info: ({sections: []})  // /api/media/audio/artist
    property var album: null             // 打开的专辑段 {album, id, offset, count, cover_path}
    property bool reviewOnly: false      // 只看歌词待处理的
    readonly property var singles: info.sections.length ? info.sections[0] : {count: 0, offset: 0}
    readonly property var albums: info.sections.slice(1)
    readonly property var lyricStatusText: ({plain: "只有纯文本歌词", search: "歌词是搜索来的，待核对", none: "查不到歌词", error: "歌词查询失败", unknown: "没查过歌词"})
    spacing: 6

    ApiClient { id: api }
    function load() {
        album = null
        info = {sections: []}
        if (artist) api.getJSON("/api/media/audio/artist?path=" + encodeURIComponent(artist.id), function (r) { pane.info = r })
    }
    onArtistChanged: load()
    function coverUrl(logical) { return logical ? backendApi.apiUrl("/api/media/audio/cover?path=" + encodeURIComponent(logical)) : "" }
    // 播放：t.index 是在歌手播放列表（或专辑）里的位置（1 起）
    function playInArtist(t) { engine.open(artist.id, artist.title, t.index - 1, true) }
    function loopScope(id, title) { engine.setMode("list"); engine.open(id, title, 0, true) }
    readonly property string scope: album ? album.id : (artist ? artist.id : "")

    PagedModel {
        id: paged
        pageSize: 100
        // 歌手页：歌手播放列表（前 singles.count 首是单曲）；专辑页：专辑内的歌；只看待处理时按当前范围筛
        endpoint: pane.scope ? "/api/media/audio/tracks?path=" + encodeURIComponent(pane.scope) + (pane.reviewOnly ? "&lyrics=unsynced" : "") : ""
    }

    RowLayout {
        spacing: 8
        Chip { visible: !!pane.album; text: "← " + (pane.artist ? pane.artist.title : ""); onClicked: pane.album = null }
        Label {
            text: pane.album ? pane.album.album : pane.artist ? pane.artist.title : "还没有音乐（在「下载 → 音乐」下载，移入资源库后在这里）"
            color: "#e6edf3"; font.pixelSize: 17; font.bold: true
            Layout.maximumWidth: 360; elide: Text.ElideRight
        }
        Label {
            color: "#8b949e"; font.pixelSize: 12
            text: !pane.artist ? "" : pane.album ? pane.album.count + " 首" : pane.singles.count + " 首单曲 · " + pane.albums.length + " 张专辑"
        }
        Chip {
            visible: !!pane.artist
            text: pane.album ? "🔁 循环这张专辑" : "🔁 循环这个歌手"
            selected: pane.engine.albumId === pane.scope && pane.engine.mode === "list"
            onClicked: pane.album ? pane.loopScope(pane.album.id, pane.album.album) : pane.loopScope(pane.artist.id, pane.artist.title)
        }
        Item { Layout.fillWidth: true }
        Chip { visible: !!pane.artist; text: "歌词待处理"; selected: pane.reviewOnly; onClicked: pane.reviewOnly = !pane.reviewOnly }
    }
    Label {
        visible: !!pane.artist
        color: "#6e7681"; font.pixelSize: 11
        text: pane.engine.albumId === (pane.artist ? pane.artist.id : "") ? "正在按歌手播放：播完一首接下一首单曲，单曲完了接各张专辑"
            : pane.album && pane.engine.albumId === pane.album.id ? "正在循环这张专辑"
            : "点一首歌：按歌手播放列表接着往下放（单曲 → 各张专辑）"
    }

    ListView {
        id: list
        Layout.fillWidth: true
        Layout.fillHeight: true
        clip: true
        // 歌手页只列单曲（歌手播放列表的前几首），专辑放在下面；专辑页、待处理列全部
        model: pane.album || pane.reviewOnly ? paged.total : Math.min(paged.total, pane.singles.count)
        WheelScroll { }
        ScrollBar.vertical: ScrollBar {}
        header: Label {
            visible: !pane.album && !pane.reviewOnly && pane.singles.count > 0
            height: visible ? implicitHeight : 0
            text: "单曲"; color: "#8b949e"; font.pixelSize: 12; bottomPadding: 4
        }
        delegate: ItemDelegate {
            id: row
            required property int index
            readonly property var t: { paged.revision; return paged.item(index) }
            readonly property bool current: !!(t && pane.engine.track && pane.engine.track.id === t.id)
            width: list.width
            height: 38
            padding: 6
            highlighted: current
            contentItem: RowLayout {
                spacing: 8
                Label { text: row.t ? (pane.album ? row.t.index : row.index + 1) : ""; color: "#6e7681"; font.pixelSize: 12; Layout.preferredWidth: 30; horizontalAlignment: Text.AlignRight }
                Image { source: "qrc:/icons/" + (pane.engine.player.playing ? "volume" : "pause") + ".svg"; sourceSize: Qt.size(16, 16); visible: row.current }
                Label {
                    Layout.fillWidth: true; elide: Text.ElideRight; font.pixelSize: 13
                    color: row.current ? "#58a6ff" : "#e6edf3"
                    // 歌手播放列表里专辑的歌带「专辑/」前缀，只显示歌名
                    text: row.t ? row.t.title : "…"
                }
                Label {
                    visible: !!(row.t && row.t.lyrics_status)
                    text: row.t && row.t.lyrics_status ? pane.lyricStatusText[row.t.lyrics_status] || row.t.lyrics_status : ""
                    color: "#e3b341"; font.pixelSize: 11
                }
            }
            onClicked: {
                if (!row.t) return
                // 专辑页里点歌：位置换算到歌手播放列表（专辑起点 + 曲序），播完接着歌手的下一张专辑
                if (pane.album) pane.engine.open(pane.artist.id, pane.artist.title, pane.album.offset + row.t.index - 1, true)
                else if (pane.reviewOnly) pane.engine.open(pane.scope, pane.album ? pane.album.album : pane.artist.title, row.t.index - 1, true)
                else pane.playInArtist(row.t)
            }
        }
        // 歌手页底部：专辑（点进去看歌）
        footer: Column {
            width: list.width
            visible: !pane.album && !pane.reviewOnly && pane.albums.length > 0
            height: visible ? implicitHeight : 0
            spacing: 6
            topPadding: 12
            Label { text: "专辑"; color: "#8b949e"; font.pixelSize: 12 }
            Flow {
                width: parent.width
                spacing: 10
                Repeater {
                    model: pane.albums
                    delegate: ItemDelegate {
                        required property var modelData
                        width: 150; height: 196
                        padding: 4
                        contentItem: Column {
                            spacing: 4
                            Rectangle {
                                width: 142; height: 142; radius: 6; color: "#21262d"; clip: true
                                Image {
                                    anchors.fill: parent; asynchronous: true; fillMode: Image.PreserveAspectCrop
                                    sourceSize: Qt.size(284, 284); source: pane.coverUrl(modelData.cover_path)
                                    visible: status === Image.Ready
                                }
                                Image { anchors.centerIn: parent; source: "qrc:/icons/music.svg"; sourceSize: Qt.size(36, 36); visible: !modelData.cover_path }
                            }
                            Label { width: 142; elide: Text.ElideRight; text: modelData.album; color: "#e6edf3"; font.pixelSize: 13 }
                            Label { text: modelData.count + " 首"; color: "#8b949e"; font.pixelSize: 11 }
                        }
                        onClicked: pane.album = modelData
                    }
                }
            }
        }
    }
    Label { visible: paged.error !== ""; text: paged.error; color: "#f85149" }
}

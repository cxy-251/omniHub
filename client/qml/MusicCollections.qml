import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

ColumnLayout {
    id: pane
    required property var engine
    property string collection: "all"
    property string query: ""
    property int page: 0
    property var selected: ({})
    property var records: []
    property bool busy: false
    property string message: ""
    readonly property string title: ({all: "全部音乐", liked: "我的点赞", disliked: "不喜欢", other: "其他歌手", trash: "回收记录"})[collection] || "音乐"
    readonly property bool trash: collection === "trash"
    readonly property string scope: "@music/" + collection
    readonly property var selectedIds: Object.keys(selected).filter(function (id) { return selected[id] })
    spacing: 8
    ApiClient { id: api; onErrorChanged: pane.message = error }
    function refresh() {
        if (!visible) return
        selected = ({})
        if (trash) api.getJSON("/api/media/audio/music/trash", function (rows) { pane.records = rows })
        else paged.reset()
    }
    function setSelected(id, on) { var next = Object.assign({}, selected); if (on) next[id] = true; else delete next[id]; selected = next }
    function selectPage() {
        var next = {}
        for (var i = page * 100; i < Math.min(paged.total, (page + 1) * 100); i++) { var t = paged.item(i); if (t) next[t.id] = true }
        selected = next
    }
    function recycle() {
        busy = true
        api.send("POST", "/api/media/audio/music/trash", {ids: selectedIds}, function (result) {
            pane.busy = false
            var moved = result.moved || []
            pane.message = "已移入系统回收站 " + moved.length + " 首" + (result.failed.length ? "；失败 " + result.failed.length + " 首：" + result.failed[0].error : "")
            if (pane.engine.track && moved.indexOf(pane.engine.track.id) >= 0) pane.engine.stopRemoved()
            pane.engine.refreshMusicQueue()
            pane.engine.musicChanged(true)
            pane.refresh()
        }, function (xhr) { pane.busy = false; pane.message = xhr.responseText || "回收失败" })
    }
    function restore(id) {
        busy = true
        api.send("POST", "/api/media/audio/music/restore", {id: id}, function () {
            pane.busy = false; pane.message = "已恢复原歌曲，原有文件不会被覆盖"
            pane.engine.musicChanged(true); pane.refresh()
        }, function (xhr) { pane.busy = false; pane.message = xhr.responseText || "恢复失败" })
    }
    // Wait until the derived trash/scope bindings reflect the selected collection.
    onCollectionChanged: { page = 0; message = ""; Qt.callLater(pane.refresh) }
    onPageChanged: selected = ({})
    Component.onCompleted: if (visible) refresh()
    onVisibleChanged: if (visible) refresh()
    Connections { target: pane.engine; function onMusicChanged(catalog) { pane.refresh() } }
    PagedModel {
        id: paged
        pageSize: 100
        endpoint: !pane.visible || pane.trash ? "" : "/api/media/audio/tracks?path=" + encodeURIComponent(pane.scope) + (pane.query ? "&q=" + encodeURIComponent(pane.query) : "")
    }
    RowLayout {
        Label { text: pane.title; color: "#e6edf3"; font.pixelSize: 18; font.bold: true }
        Label { text: (pane.trash ? pane.records.length : paged.total) + (pane.trash ? " 条" : " 首"); color: "#8b949e" }
        Item { Layout.fillWidth: true }
        TextField { visible: !pane.trash; placeholderText: "搜索歌名、歌手、专辑"; Layout.preferredWidth: 230
            onTextChanged: searchTimer.restart()
            Timer { id: searchTimer; interval: 250; onTriggered: { pane.query = parent.text.trim(); pane.page = 0 } } }
    }
    RowLayout {
        visible: pane.collection === "disliked"
        Chip { text: "全选本页"; enabled: !pane.busy && pane.engine.musicCanTrash; onClicked: pane.selectPage() }
        Chip { text: "取消选择"; enabled: pane.selectedIds.length > 0; onClicked: pane.selected = ({}) }
        Chip { text: "移入系统回收站（" + pane.selectedIds.length + "）"; enabled: !pane.busy && pane.engine.musicCanTrash && pane.selectedIds.length > 0; onClicked: confirm.open() }
    }
    Label { visible: pane.collection === "disliked"; text: "不喜欢只作标记；选择后再回收，回收的歌曲可在“回收记录”恢复。"; color: "#8b949e"; wrapMode: Text.Wrap; Layout.fillWidth: true }
    Label { visible: pane.trash; text: "恢复到原来的歌手／专辑目录，不覆盖已有文件。系统回收站被清空后无法恢复。"; color: "#8b949e"; wrapMode: Text.Wrap; Layout.fillWidth: true }
    Label { visible: pane.message !== "" || paged.error !== ""; text: pane.message || paged.error; color: "#e3b341"; wrapMode: Text.Wrap; Layout.fillWidth: true }
    ListView {
        id: list
        Layout.fillWidth: true; Layout.fillHeight: true; clip: true
        model: pane.trash ? pane.records.length : Math.min(100, Math.max(0, paged.total - pane.page * 100))
        ScrollBar.vertical: ScrollBar { }
        WheelScroll { }
        delegate: ItemDelegate {
            id: row
            required property int index
            readonly property var item: { paged.revision; return pane.trash ? pane.records[index] : paged.item(pane.page * 100 + index) }
            width: list.width; height: 58; padding: 6
            highlighted: !!(item && pane.engine.track && pane.engine.track.id === item.id)
            contentItem: RowLayout {
                CheckBox { visible: pane.collection === "disliked"; enabled: !!row.item && !pane.busy && pane.engine.musicCanTrash
                    checked: !!(row.item && pane.selected[row.item.id]); onToggled: if (row.item) pane.setSelected(row.item.id, checked) }
                ColumnLayout {
                    Layout.fillWidth: true; spacing: 2
                    Label { text: row.item ? row.item.title : "…"; color: "#e6edf3"; elide: Text.ElideRight; Layout.fillWidth: true }
                    Label { text: row.item ? row.item.artist + (row.item.album ? " · " + row.item.album : "") + (pane.trash ? " · " + row.item.created_at.slice(0, 10) : "") : ""; color: "#8b949e"; font.pixelSize: 11; elide: Text.ElideRight; Layout.fillWidth: true }
                }
                MusicRatingButtons { visible: !pane.trash; engine: pane.engine; trackId: !pane.trash && row.item ? row.item.id : "" }
                Chip { visible: pane.trash; text: "恢复"; enabled: !pane.busy; onClicked: if (row.item) pane.restore(row.item.id) }
            }
            onClicked: if (!pane.trash && item) pane.engine.open(pane.scope, pane.title, item.index - 1, true)
        }
    }
    RowLayout {
        visible: !pane.trash && paged.total > 100
        Chip { text: "上一页"; enabled: pane.page > 0; onClicked: { pane.page--; list.positionViewAtBeginning() } }
        Label { text: (pane.page + 1) + " / " + Math.ceil(paged.total / 100); color: "#8b949e" }
        Chip { text: "下一页"; enabled: (pane.page + 1) * 100 < paged.total; onClicked: { pane.page++; list.positionViewAtBeginning() } }
    }
    Dialog {
        id: confirm
        anchors.centerIn: Overlay.overlay
        width: Math.min(480, Overlay.overlay.width - 32)
        title: "将 " + pane.selectedIds.length + " 首歌曲移入系统回收站？"
        modal: true; standardButtons: Dialog.Yes | Dialog.No
        contentItem: Label { text: "只回收所选音乐文件，专辑封面保留。\n可在“回收记录”恢复，原有文件不会被覆盖。"; color: "#e6edf3"; wrapMode: Text.Wrap }
        onAccepted: pane.recycle()
    }
}

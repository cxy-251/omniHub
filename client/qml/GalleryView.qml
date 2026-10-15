import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 相册（只读）：按色系筛选的缩略图网格 + 原图查看。
// 网格数据由 PagedModel 按页懒加载，只保留附近若干页；缩略图按格子尺寸解码。
// 原图按窗口尺寸解码，切走即释放；页面隐藏时整个视图由 Main.qml 的 Loader 销毁。
Item {
    id: view
    property int color: -1
    property var colorCounts: ({})
    property int openIndex: -1

    // gallery.db 的 color_family 0~9（定义见 gallery 项目 color_service.ClassifyColor）。
    readonly property var colorNames: ["红", "橙", "黄", "绿", "蓝", "紫", "粉", "白", "黑", "灰"]

    function apiQuery(path, photo) {
        return backendApi.apiUrl(path + "?library_id=" + encodeURIComponent(photo.library_id) + "&id=" + encodeURIComponent(photo.id))
    }

    PagedModel {
        id: paged
        pageSize: 100
        endpoint: "/api/media/gallery/list" + (view.color >= 0 ? "?color=" + view.color : "")
    }

    Component.onCompleted: {
        var url = backendApi.apiUrl("/api/media/gallery/colors")
        if (!url) return
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function () {
            if (view && xhr.readyState === XMLHttpRequest.DONE && xhr.status === 200) view.colorCounts = JSON.parse(xhr.responseText)
        }
        xhr.open("GET", url)
        xhr.send()
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
            model: 11
            delegate: Chip {
                required property int index
                readonly property int family: index - 1
                text: family < 0 ? "全部" : view.colorNames[family] + " · " + (view.colorCounts[family] || 0)
                selected: view.color === family
                onClicked: view.color = family
            }
        }
        Label { text: paged.error; visible: text.length > 0; color: "#f85149" }
        Label { text: paged.total + " 张（只读）"; color: "#8b949e" }

        GridView {
            id: grid
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            readonly property int columns: Math.max(3, Math.round(width / 200))
            cellWidth: Math.floor(width / columns)
            cellHeight: cellWidth
            model: paged.total
            cacheBuffer: cellHeight * 2
            ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }
            delegate: Item {
                id: cell
                required property int index
                readonly property var photo: { paged.revision; return paged.item(index) }
                width: grid.cellWidth
                height: grid.cellHeight
                Rectangle {
                    anchors.fill: parent
                    anchors.margins: 2
                    color: cell.photo && cell.photo.color_hex ? cell.photo.color_hex : "#161b22"
                    Image {
                        anchors.fill: parent
                        fillMode: Image.PreserveAspectCrop
                        asynchronous: true
                        sourceSize.width: grid.cellWidth
                        sourceSize.height: grid.cellHeight
                        source: cell.photo ? view.apiQuery("/api/media/gallery/thumbnail", cell.photo) : ""
                    }
                    TapHandler { onTapped: if (cell.photo) view.openIndex = cell.index }
                }
            }
        }
    }

    Loader {
        anchors.fill: parent
        active: view.openIndex >= 0
        sourceComponent: Component {
            Rectangle {
                id: viewer
                readonly property var photo: { paged.revision; return paged.item(view.openIndex) }
                color: "black"
                focus: true
                Component.onCompleted: forceActiveFocus()
                function step(delta) { view.openIndex = Math.max(0, Math.min(paged.total - 1, view.openIndex + delta)) }
                Keys.onPressed: function (event) {
                    if (event.key === Qt.Key_Escape || event.key === Qt.Key_Back) view.openIndex = -1
                    else if (event.key === Qt.Key_Right || event.key === Qt.Key_Down) step(1)
                    else if (event.key === Qt.Key_Left || event.key === Qt.Key_Up) step(-1)
                    else return
                    event.accepted = true
                }
                // 先显示缩略图，原图到达后覆盖，翻页不会出现空白。
                Image {
                    anchors.fill: parent; anchors.bottomMargin: 56
                    fillMode: Image.PreserveAspectFit
                    source: viewer.photo ? view.apiQuery("/api/media/gallery/thumbnail", viewer.photo) : ""
                    visible: original.status !== Image.Ready
                }
                Image {
                    id: original
                    anchors.fill: parent; anchors.bottomMargin: 56
                    fillMode: Image.PreserveAspectFit
                    asynchronous: true
                    cache: false
                    sourceSize.width: viewer.width
                    sourceSize.height: viewer.height
                    source: viewer.photo ? view.apiQuery("/api/media/gallery/original", viewer.photo) : ""
                }
                TapHandler { onTapped: function (point) { viewer.step(point.position.x < viewer.width / 3 ? -1 : 1) } }
                RowLayout {
                    anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom; anchors.margins: 8
                    Button { text: "返回"; onClicked: view.openIndex = -1 }
                    Button { text: "上一张"; enabled: view.openIndex > 0; onClicked: viewer.step(-1) }
                    Button { text: "下一张"; enabled: view.openIndex < paged.total - 1; onClicked: viewer.step(1) }
                    Label {
                        Layout.fillWidth: true; color: "white"; elide: Text.ElideRight
                        text: (view.openIndex + 1) + " / " + paged.total + (viewer.photo ? "  ·  " + viewer.photo.filename + "  " + viewer.photo.width + "×" + viewer.photo.height : "")
                    }
                    BusyIndicator { running: original.status === Image.Loading; implicitHeight: 32; implicitWidth: 32 }
                }
            }
        }
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import omniHub.Media 1.0

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


    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 6
        visible: view.openIndex < 0

        RowLayout {
            Layout.fillWidth: true
            spacing: 4
            Repeater {
                model: 11
                delegate: Chip {
                    required property int index
                    readonly property int family: index - 1
                    text: family < 0 ? "全部" : view.colorNames[family] + " · " + (view.colorCounts[family] || 0)
                    selected: view.color === family
                    onClicked: view.color = family
                }
            }
            Item { Layout.fillWidth: true }
            Label {
                text: paged.error || (paged.total + " 张 · 只读")
                color: paged.error ? "#f85149" : "#8b949e"; font.pixelSize: 12
            }
        }

        // 瀑布流：每张照片放进当前最矮的一列，纵坐标随序号单调不减，可二分查找可视范围。
        // 只创建可视区上下各一屏内的格子；pool 个格子按 序号 mod pool 复用，滚动时只换进出边界的那几张。
        Flickable {
            WheelScroll { }
            id: masonry
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            contentWidth: width
            contentHeight: layout.contentHeight
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }

            readonly property int gap: 4
            readonly property int columns: Math.max(3, Math.round(width / 200))
            readonly property real colWidth: (width - gap * (columns - 1)) / columns
            readonly property int pool: 240
            // 可视区上下各多算一屏
            readonly property int first: (layout.revision, layout.find(contentY - Math.max(height, layout.maxItem)))
            readonly property int last: (layout.revision, Math.min(first + pool, layout.find(contentY + 2 * height + layout.maxItem)))

            // 排版在 C++ 里做（MasonryLayout）：它自己取全部高宽比，筛选变化时重新取并回到顶部。
            MasonryLayout {
                id: layout
                source: backendApi.apiUrl("/api/media/gallery/ratios" + (view.color >= 0 ? "?color=" + view.color : ""))
                columns: masonry.columns
                columnWidth: masonry.colWidth
                gap: masonry.gap
                onLoaded: masonry.contentY = 0
            }
            // 开发验证：devScroll < 0 时打开原图，每 -devScroll 毫秒翻一张（-100 模拟按住方向键）。
            Timer {
                interval: typeof devScroll !== "undefined" && devScroll < 0 ? -devScroll : 100; repeat: true
                running: typeof devScroll !== "undefined" && devScroll < 0 && paged.total > 0
                onTriggered: view.openIndex = (view.openIndex + 1) % paged.total
            }
            Timer {
                interval: 100; repeat: true
                running: typeof devScroll !== "undefined" && devScroll > 0
                onTriggered: masonry.contentY = Math.min(masonry.contentHeight - masonry.height, masonry.contentY + devScroll)
            }

            Repeater {
                model: masonry.pool
                delegate: Item {
                    id: cell
                    required property int index
                    // 本格子负责 [first, first+pool) 中 序号 ≡ index (mod pool) 的那一张
                    readonly property int photoIndex: masonry.first + ((index - masonry.first % masonry.pool) + masonry.pool) % masonry.pool
                    readonly property bool shown: photoIndex < masonry.last && photoIndex < layout.count
                    readonly property var photo: { paged.revision; return shown ? paged.item(photoIndex) : null }
                    visible: shown
                    // itemX/itemY 是函数，不发通知；读 revision 让排版变化后重新求值。
                    x: shown ? (layout.revision, layout.itemX(photoIndex)) : 0
                    y: shown ? (layout.revision, layout.itemY(photoIndex)) : 0
                    width: masonry.colWidth
                    height: shown ? (layout.revision, layout.itemHeight(photoIndex)) : 0
                    Rectangle {
                        anchors.fill: parent
                        radius: 3
                        color: cell.photo && cell.photo.color_hex ? cell.photo.color_hex : "#161b22"
                        Image {
                            anchors.fill: parent
                            fillMode: Image.PreserveAspectCrop
                            asynchronous: true
                            sourceSize.width: masonry.colWidth
                            source: cell.photo ? view.apiQuery("/api/media/gallery/thumbnail", cell.photo) : ""
                        }
                        TapHandler { onTapped: if (cell.photo) view.openIndex = cell.photoIndex }
                    }
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
                // 翻页时先只显示缩略图；停下 250ms 后才加载原图，按住方向键快速翻时不会逐张解码原图。
                property string originalSource: ""
                onPhotoChanged: { originalSource = ""; originalTimer.restart() }
                Timer { id: originalTimer; interval: 250; onTriggered: viewer.originalSource = viewer.photo ? view.apiQuery("/api/media/gallery/original", viewer.photo) : "" }
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
                    anchors.fill: parent
                    fillMode: Image.PreserveAspectFit
                    source: viewer.photo ? view.apiQuery("/api/media/gallery/thumbnail", viewer.photo) : ""
                    visible: original.status !== Image.Ready
                }
                Image {
                    id: original
                    anchors.fill: parent
                    fillMode: Image.PreserveAspectFit
                    asynchronous: true
                    cache: false
                    sourceSize.width: viewer.width
                    sourceSize.height: viewer.height
                    source: viewer.originalSource
                }
                TapHandler { onTapped: function (point) { viewer.step(point.position.x < viewer.width / 3 ? -1 : 1) } }
                // 底部渐变控件条（与短视频图集查看器一致）
                Item {
                    anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
                    height: 64
                    Rectangle {
                        anchors.fill: parent
                        gradient: Gradient {
                            GradientStop { position: 0; color: "#00000000" }
                            GradientStop { position: 1; color: "#dd000000" }
                        }
                    }
                    RowLayout {
                        anchors.fill: parent; anchors.leftMargin: 12; anchors.rightMargin: 12; anchors.topMargin: 16; anchors.bottomMargin: 6
                        spacing: 2
                        IconButton { glyph: "back"; onClicked: view.openIndex = -1; ToolTip.text: "返回（Esc）" }
                        IconButton { glyph: "chevron_left"; enabled: view.openIndex > 0; onClicked: viewer.step(-1); ToolTip.text: "上一张（←）" }
                        Label { color: "white"; font.pixelSize: 13; text: (view.openIndex + 1) + " / " + paged.total }
                        IconButton { glyph: "chevron_right"; enabled: view.openIndex < paged.total - 1; onClicked: viewer.step(1); ToolTip.text: "下一张（→）" }
                        Label {
                            Layout.fillWidth: true; Layout.leftMargin: 8; color: "#adbac7"; font.pixelSize: 12; elide: Text.ElideMiddle
                            text: viewer.photo ? viewer.photo.filename + "  ·  " + viewer.photo.width + "×" + viewer.photo.height : ""
                        }
                        BusyIndicator { running: original.status === Image.Loading || viewer.originalSource === ""; visible: running; implicitHeight: 28; implicitWidth: 28 }
                    }
                }
            }
        }
    }
}

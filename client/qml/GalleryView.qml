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

    // 筛选变化时重新取全部高宽比并排版；回到顶部。
    function loadRatios() {
        var url = backendApi.apiUrl("/api/media/gallery/ratios" + (view.color >= 0 ? "?color=" + view.color : ""))
        if (!url) return
        var xhr = new XMLHttpRequest()
        var forColor = view.color
        xhr.onreadystatechange = function () {
            if (!view || xhr.readyState !== XMLHttpRequest.DONE || xhr.status !== 200 || forColor !== view.color) return
            layout.ratios = JSON.parse(xhr.responseText).ratios
            layout.rebuild()
            masonry.contentY = 0
        }
        xhr.open("GET", url)
        xhr.send()
    }
    onColorChanged: loadRatios()

    PagedModel {
        id: paged
        pageSize: 100
        endpoint: "/api/media/gallery/list" + (view.color >= 0 ? "?color=" + view.color : "")
    }

    Component.onCompleted: {
        loadRatios()
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
            id: masonry
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            contentWidth: width
            contentHeight: layout.height
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }

            readonly property int gap: 4
            readonly property int columns: Math.max(3, Math.round(width / 200))
            readonly property real colWidth: (width - gap * (columns - 1)) / columns
            readonly property int pool: 240
            // 可视区上下各多算一屏
            readonly property int first: layout.find(contentY - Math.max(height, layout.maxItem))
            readonly property int last: Math.min(first + pool, layout.find(contentY + 2 * height + layout.maxItem))

            QtObject {
                id: layout
                property var ratios: []      // 高宽比 ×1000，与列表同序
                property var ys: []          // 每张的纵坐标
                property var cols: []        // 每张所在列
                property real height: 0
                property real maxItem: 0
                property int revision: 0
                function rebuild() {
                    var n = ratios.length, w = masonry.colWidth, g = masonry.gap
                    var heights = []
                    for (var c = 0; c < masonry.columns; c++) heights.push(0)
                    var y = new Array(n), col = new Array(n), tallest = 0
                    for (var i = 0; i < n; i++) {
                        var best = 0
                        for (var k = 1; k < heights.length; k++) if (heights[k] < heights[best]) best = k
                        var h = w * Math.min(2.5, Math.max(0.4, ratios[i] / 1000))
                        y[i] = heights[best]; col[i] = best
                        heights[best] += h + g
                        if (h > tallest) tallest = h
                    }
                    ys = y; cols = col; maxItem = tallest
                    height = Math.max.apply(null, heights.concat([0]))
                    revision++
                }
                // 第一个纵坐标 >= y 的序号（ys 单调不减）
                function find(y) {
                    revision
                    var lo = 0, hi = ys.length
                    while (lo < hi) { var mid = (lo + hi) >> 1; if (ys[mid] < y) lo = mid + 1; else hi = mid }
                    return lo
                }
                function itemHeight(i) { return masonry.colWidth * Math.min(2.5, Math.max(0.4, ratios[i] / 1000)) }
            }
            onColumnsChanged: layout.rebuild()
            onColWidthChanged: layout.rebuild()

            Repeater {
                model: masonry.pool
                delegate: Item {
                    id: cell
                    required property int index
                    // 本格子负责 [first, first+pool) 中 序号 ≡ index (mod pool) 的那一张
                    readonly property int photoIndex: masonry.first + ((index - masonry.first % masonry.pool) + masonry.pool) % masonry.pool
                    readonly property bool shown: photoIndex < masonry.last && photoIndex < layout.ys.length
                    readonly property var photo: { paged.revision; return shown ? paged.item(photoIndex) : null }
                    visible: shown
                    x: shown ? layout.cols[photoIndex] * (masonry.colWidth + masonry.gap) : 0
                    y: shown ? layout.ys[photoIndex] : 0
                    width: masonry.colWidth
                    height: shown ? layout.itemHeight(photoIndex) : 0
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

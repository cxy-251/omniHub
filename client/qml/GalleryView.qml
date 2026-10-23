import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtCore
import omniHub.Media 1.0

// 相册（只读）：频道（推荐、我的点赞、色系、回收站）的瀑布流 + 原图查看 + 多联放映（图片自动轮播）。
// 推荐、点赞、回收站、行为上报与短视频共用 FeedApi / ChannelBar / MatrixView。回收站只在 omniHub 里隐藏，不改原图库。
// 网格数据由 PagedModel 按页懒加载，只保留附近若干页；缩略图按格子尺寸解码。
// 原图按窗口尺寸解码，切走即释放；页面隐藏时整个视图由 Main.qml 的 Loader 销毁。
Item {
    id: view
    property int openIndex: -1
    property var audioEngine: null          // 全局音声引擎，交给多联放映做伴听
    property bool matrixWindowOpen: false
    signal matrixWindowRequested()
    Settings { id: viewPrefs; category: "gallery"; property string channel: "recommend"; property string mode: "browse" }
    property alias channel: viewPrefs.channel
    property alias mode: viewPrefs.mode       // browse | matrix

    FeedApi { id: feed; domain: "gallery" }
    property var likedOverride: ({})
    function isLiked(photo) { return photo && (photo.id in likedOverride ? likedOverride[photo.id] : !!photo.liked) }
    function toggleLike(photo) {
        var on = !isLiked(photo)
        feed.like(photo, on, function () { var m = Object.assign({}, view.likedOverride); m[photo.id] = on; view.likedOverride = m })
    }
    // 移到回收站 / 从回收站恢复后，列表与排版都要重取（序号变了）。
    function reload() { paged.reset(); layout.source = ""; layout.source = view.ratiosUrl() }
    function ratiosUrl() { return backendApi.apiUrl(feed.path("ratios") + "?channel=" + encodeURIComponent(view.channel) + "&seed=" + feed.seed) }

    function apiQuery(path, photo) {
        return backendApi.apiUrl(path + "?library_id=" + encodeURIComponent(photo.library_id) + "&id=" + encodeURIComponent(photo.id))
    }

    PagedModel {
        id: paged
        pageSize: 100
        endpoint: view.mode === "browse" ? feed.itemsPath(view.channel) : ""
    }

    Component.onCompleted: feed.loadGroups()
    // 列表变短（移到回收站后）时，大图停在原位置看下一张；原来就是最后一张则退到前一张。
    Connections {
        target: paged
        function onTotalChanged() { if (view.openIndex >= paged.total && paged.total > 0) view.openIndex = paged.total - 1 }
    }


    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 6
        visible: view.openIndex < 0 && view.mode === "browse"

        RowLayout {
            Layout.fillWidth: true
            spacing: 4
            Chip { text: "浏览"; selected: true }
            Chip { text: view.matrixWindowOpen ? "多联放映（窗口）" : "多联放映"; onClicked: if (view.matrixWindowOpen) view.matrixWindowRequested(); else view.mode = "matrix" }
            Item { Layout.fillWidth: true }
            Chip { text: "换一批"; visible: view.channel === "recommend"; onClicked: feed.reshuffle() }
            Label {
                text: paged.error || feed.error || (paged.total + " 张" + (view.channel === "hidden" ? "（回收站，可恢复）" : ""))
                color: paged.error || feed.error ? "#f85149" : "#8b949e"; font.pixelSize: 12
            }
        }
        // 推荐 | 我的点赞 | 色系 | 回收站
        ChannelBar {
            Layout.fillWidth: true
            Layout.preferredHeight: 28
            feed: feed
            channel: view.channel
            showHidden: true
            onPicked: function (ch) { view.channel = ch }
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
                source: view.mode === "browse" ? view.ratiosUrl() : ""
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
                        Label {
                            anchors.left: parent.left; anchors.top: parent.top; anchors.margins: 4
                            visible: view.isLiked(cell.photo)
                            text: "♥"; color: "#f778ba"; font.pixelSize: 15
                        }
                        TapHandler { onTapped: if (cell.photo) view.openIndex = cell.photoIndex }
                    }
                }
            }
        }
    }

    // 下载原图：默认存到「下载」目录、用原文件名。
    FileDialog {
        id: saveDialog
        property var photo: null
        title: "保存原图"
        fileMode: FileDialog.SaveFile
        currentFolder: StandardPaths.writableLocation(StandardPaths.DownloadLocation)
        onAccepted: if (photo) backendApi.download("/api/media/gallery/original?library_id=" + encodeURIComponent(photo.library_id)
                                                   + "&id=" + encodeURIComponent(photo.id), selectedFile)
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
                // 行为上报：展示即 view；离开时 1 秒内划走算 skip，停留 4 秒以上算 dwell。
                property var shown: null
                property double shownAt: 0
                function finish() {
                    if (!shown) return
                    var ms = Date.now() - shownAt
                    if (ms < 1000) feed.event(shown, "skip")
                    else if (ms >= 4000) feed.event(shown, "dwell")
                    shown = null
                }
                onPhotoChanged: {
                    stage.scale = 1; detail = 1
                    originalSource = ""; originalTimer.restart()
                    finish()
                    if (photo) { shown = photo; shownAt = Date.now(); viewTimer.restart() }
                }
                // 停留 0.6 秒以上才算看过（按住方向键快速翻过的不算）。
                Timer { id: viewTimer; interval: 600; onTriggered: if (viewer.shown) feed.event(viewer.shown, "view") }
                Component.onDestruction: finish()
                property bool trashArmed: false
                Timer { id: trashDisarm; interval: 3000; onTriggered: viewer.trashArmed = false }
                // 3 秒内点两次（或按两次 Delete）。完成后留在大图里，同一位置换成下一张。
                property string notice: ""
                Timer { id: noticeTimer; interval: 2500; onTriggered: viewer.notice = "" }
                function say(text) { notice = text; noticeTimer.restart() }
                Connections {
                    target: backendApi
                    function onDownloadFinished(file, error) {
                        viewer.say(error ? "保存失败：" + error : "已保存到 " + file)
                    }
                }
                function trash() {
                    var restoring = view.channel === "hidden"
                    if (!trashArmed) { trashArmed = true; trashDisarm.restart(); say(restoring ? "再点一次从回收站恢复" : "再点一次移到回收站"); return }
                    trashArmed = false
                    var target = photo
                    shown = null
                    var done = function () {
                        view.reload()
                        viewer.say(restoring ? "已恢复" : "已移到回收站，可在「🗑 回收站」里恢复")
                    }
                    if (restoring) feed.restore(target, done); else feed.trash(target, done)
                }
                Timer { id: originalTimer; interval: 250; onTriggered: viewer.originalSource = viewer.photo ? view.apiQuery("/api/media/gallery/original", viewer.photo) : "" }
                color: "black"
                clip: true
                focus: true
                Component.onCompleted: forceActiveFocus()
                function step(delta) { view.openIndex = Math.max(0, Math.min(paged.total - 1, view.openIndex + delta)) }
                Keys.onPressed: function (event) {
                    if ((event.key === Qt.Key_Escape || event.key === Qt.Key_Back) && stage.scale > 1) zoomTo(1, width / 2, height / 2)
                    else if (event.key === Qt.Key_Escape || event.key === Qt.Key_Back) view.openIndex = -1
                    else if (event.key === Qt.Key_Plus || event.key === Qt.Key_Equal) zoomTo(stage.scale * 1.5, width / 2, height / 2)
                    else if (event.key === Qt.Key_Minus) zoomTo(stage.scale / 1.5, width / 2, height / 2)
                    else if (event.key === Qt.Key_0) zoomTo(1, width / 2, height / 2)
                    else if (event.key === Qt.Key_Right || event.key === Qt.Key_Down) step(1)
                    else if (event.key === Qt.Key_Left || event.key === Qt.Key_Up) step(-1)
                    else if (event.key === Qt.Key_Z && photo) view.toggleLike(photo)
                    else if (event.key === Qt.Key_Delete && photo) trash()
                    else return
                    event.accepted = true
                }
                // 先显示缩略图，原图到达后覆盖，翻页不会出现空白。
                // 缩放：滚轮或双指 1～5 倍（以光标/手指为中心），双击在 1 倍与 2.5 倍间切换，放大后拖动平移；
                // 放大时单击不翻页。第一次放大后原图改按 2.5 倍窗口尺寸解码，免得放大后发糊。
                property real detail: 1
                function zoomTo(scale, px, py) {
                    var s = Math.max(1, Math.min(5, scale))
                    // stage 以中心为变换原点：保持 (px, py) 下的像素不动。
                    var cx = px - width / 2, cy = py - height / 2
                    var k = s / stage.scale
                    stage.x = s === 1 ? 0 : cx - (cx - stage.x) * k
                    stage.y = s === 1 ? 0 : cy - (cy - stage.y) * k
                    stage.scale = s
                }
                Item {
                    id: stage
                    width: viewer.width; height: viewer.height
                    transformOrigin: Item.Center
                    onScaleChanged: {
                        if (scale > 5) { scale = 5; return }
                        if (scale < 1) { scale = 1; return }
                        if (scale > 1.2) viewer.detail = 2.5
                        if (scale <= 1.001) { x = 0; y = 0 }
                    }
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
                        sourceSize.width: viewer.width * viewer.detail
                        sourceSize.height: viewer.height * viewer.detail
                        source: viewer.originalSource
                    }
                }
                WheelHandler {
                    target: stage
                    property: "scale"
                    targetScaleMultiplier: 1.2
                    targetTransformAroundCursor: true
                    acceptedDevices: PointerDevice.Mouse | PointerDevice.TouchPad
                }
                PinchHandler { target: stage; minimumScale: 1; maximumScale: 5; minimumRotation: 0; maximumRotation: 0 }
                DragHandler { target: stage; enabled: stage.scale > 1 }
                TapHandler {
                    onSingleTapped: function (point) { if (stage.scale <= 1) viewer.step(point.position.x < viewer.width / 3 ? -1 : 1) }
                    onDoubleTapped: function (point) { viewer.zoomTo(stage.scale > 1 ? 1 : 2.5, point.position.x, point.position.y) }
                }
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
                        IconButton {
                            glyph: view.isLiked(viewer.photo) ? "favorite-fill" : "favorite"
                            enabled: !!viewer.photo
                            onClicked: view.toggleLike(viewer.photo)
                            ToolTip.text: view.isLiked(viewer.photo) ? "取消点赞（Z）" : "点赞（Z）"
                        }
                        IconButton {
                            glyph: "download"
                            enabled: !!viewer.photo
                            onClicked: {
                                saveDialog.photo = viewer.photo
                                saveDialog.selectedFile = saveDialog.currentFolder + "/" + encodeURIComponent(viewer.photo.filename || viewer.photo.id)
                                saveDialog.open()
                            }
                            ToolTip.text: "下载原图"
                        }
                        Label {
                            visible: viewer.notice !== ""
                            text: viewer.notice
                            color: viewer.trashArmed ? "#ff7b72" : "#3fb950"; font.pixelSize: 13; font.bold: viewer.trashArmed
                        }
                        IconButton {
                            glyph: "delete"; checked: viewer.trashArmed
                            enabled: !!viewer.photo
                            onClicked: viewer.trash()
                            ToolTip.text: view.channel === "hidden" ? "从回收站恢复（Delete 按两次）" : "移到回收站（Delete 按两次；只在 omniHub 里隐藏，可恢复）"
                        }
                    }
                }
            }
        }
    }

    // 多联放映（图片自动轮播），与短视频同一个组件
    Loader {
        anchors.fill: parent
        active: view.mode === "matrix" && !view.matrixWindowOpen
        sourceComponent: Component {
            MatrixView {
                domain: "gallery"
                audioEngine: view.audioEngine
                onExitRequested: view.mode = "browse"
                onDetachRequested: { view.mode = "browse"; Qt.callLater(view.matrixWindowRequested) }
            }
        }
        onLoaded: item.forceActiveFocus()
    }
}

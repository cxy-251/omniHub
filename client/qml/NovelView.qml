import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 小说：左侧分类（含最近阅读），右侧生成式书封网格（按页懒加载）；点开进入 EPUB 阅读器。
// 阅读位置（章节 + 章内滚动比例）存到账号，下次打开续读。
Item {
    id: view
    property var categories: []
    property var recent: []
    property string category: "__recent"
    property var openBook: null          // {id, title, ...}；非空时显示阅读器
    property string error: ""

    function getJSON(path, done) {
        var url = backendApi.apiUrl(path)
        if (!url) { view.error = "无法连接 omniHub 服务"; return }
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function () {
            if (xhr.readyState !== XMLHttpRequest.DONE || !view) return
            if (xhr.status !== 200) { view.error = "请求失败（" + xhr.status + "）"; return }
            done(JSON.parse(xhr.responseText))
        }
        xhr.open("GET", url)
        xhr.send()
    }
    function loadRecent() { getJSON("/api/media/text/recent", function (list) { view.recent = list }) }
    function flush() { if (readerLoader.item) readerLoader.item.saveNow() }

    Component.onCompleted: {
        getJSON("/api/media/text/novel-categories", function (list) {
            // standard 在前、nsfw 在后，组内按名称；group/name 供分组标题与显示用。
            list.forEach(function (c) { var i = c.category.indexOf("/"); c.group = i > 0 ? c.category.slice(0, i) : c.category; c.name = i > 0 ? c.category.slice(i + 1) : c.category })
            list.sort(function (a, b) { return (a.group === "standard" ? 0 : 1) - (b.group === "standard" ? 0 : 1) || (a.category < b.category ? -1 : 1) })
            view.categories = list
        })
        loadRecent()
    }

    // 开发验证：devScroll < 0 时自动选第一个分类并打开第一本书。
    Timer {
        interval: 300; repeat: true
        running: typeof devScroll !== "undefined" && devScroll < 0 && !view.openBook
        onTriggered: {
            if (view.category === "__recent" && view.categories.length) view.category = view.categories[0].category
            else if (paged.total > 0 && paged.item(0)) view.openBook = paged.item(0)
        }
    }
    PagedModel {
        id: paged
        pageSize: 100
        endpoint: view.category && view.category !== "__recent" && !view.openBook ? "/api/media/text/novels?category=" + encodeURIComponent(view.category) : ""
    }

    // 生成式书封：按分类名取色，印上书名。
    function coverColor(name) {
        var h = 0
        for (var i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360
        return Qt.hsla(h / 360, 0.35, 0.28, 1)
    }

    RowLayout {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 8
        visible: !view.openBook

        // 分类列
        ListView {
            id: categoryList
            Layout.preferredWidth: 200
            Layout.fillHeight: true
            clip: true
            spacing: 2
            model: [{category: "__recent", group: "", name: "最近阅读", count: view.recent.length}].concat(view.categories)
            section.property: "group"
            section.delegate: Label {
                required property string section
                visible: section.length > 0
                height: section.length > 0 ? 26 : 0
                text: section === "standard" ? "标准" : section.toUpperCase()
                color: "#8b949e"; font.pixelSize: 11; font.bold: true
                verticalAlignment: Text.AlignBottom; leftPadding: 8
            }
            ScrollBar.vertical: ScrollBar {}
            delegate: ItemDelegate {
                required property var modelData
                width: ListView.view.width
                height: 30
                highlighted: view.category === modelData.category
                contentItem: RowLayout {
                    Label {
                        Layout.fillWidth: true; elide: Text.ElideRight; font.pixelSize: 13
                        text: modelData.name
                        color: view.category === modelData.category ? "#58a6ff" : "#c9d1d9"
                    }
                    Label { text: modelData.count; color: "#8b949e"; font.pixelSize: 12 }
                }
                onClicked: { view.category = modelData.category; if (modelData.category === "__recent") view.loadRecent() }
            }
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 6
            Label {
                text: view.error || paged.error || (view.category === "__recent" ? (view.recent.length ? "最近阅读" : "还没有阅读记录，从左侧分类选一本书开始") : paged.total + " 本")
                color: view.error || paged.error ? "#f85149" : "#8b949e"; font.pixelSize: 12
            }
            GridView {
                id: grid
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                readonly property int columns: Math.max(3, Math.round(width / 150))
                readonly property bool showingRecent: view.category === "__recent"
                cellWidth: Math.floor(width / columns)
                cellHeight: cellWidth * 1.45
                model: showingRecent ? view.recent.length : paged.total
                ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }
                delegate: Item {
                    id: cell
                    required property int index
                    readonly property var book: { paged.revision; return grid.showingRecent ? view.recent[index] : paged.item(index) }
                    width: grid.cellWidth
                    height: grid.cellHeight
                    Rectangle {
                        anchors.fill: parent
                        anchors.margins: 5
                        radius: 4
                        color: cell.book ? view.coverColor(cell.book.category) : "#161b22"
                        border.color: tap.pressed ? "#58a6ff" : "#30363d"
                        Rectangle { width: 6; height: parent.height; color: "#33000000"; radius: 2 }   // 书脊
                        // 有封面的 EPUB 显示封面，加载失败（无封面）时露出下面的生成式书封。
                        Image {
                            id: cover
                            anchors.fill: parent
                            fillMode: Image.PreserveAspectCrop
                            asynchronous: true
                            sourceSize.width: grid.cellWidth
                            source: cell.book && cell.book.format === "epub" ? backendApi.apiUrl("/api/media/text/epub/cover?path=" + encodeURIComponent(cell.book.id)) : ""
                            z: 1
                            visible: status === Image.Ready && implicitWidth > 1
                        }
                        Label {
                            anchors.fill: parent; anchors.margins: 12; anchors.leftMargin: 16
                            text: cell.book ? cell.book.title : ""
                            wrapMode: Text.Wrap; elide: Text.ElideRight
                            horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
                            color: "#e6edf3"; font.pixelSize: 14; font.bold: true
                        }
                        Label {
                            anchors.bottom: parent.bottom; anchors.left: parent.left; anchors.right: parent.right; anchors.margins: 6
                            horizontalAlignment: Text.AlignHCenter; elide: Text.ElideRight
                            color: "#99e6edf3"; font.pixelSize: 11
                            text: !cell.book ? "" : grid.showingRecent ? "读到第 " + (cell.book.chapter_index + 1) + " 章" : cell.book.category.split("/").pop()
                        }
                        TapHandler { id: tap; onTapped: if (cell.book) view.openBook = cell.book }
                    }
                }
            }
        }
    }

    Loader {
        id: readerLoader
        anchors.fill: parent
        active: !!view.openBook
        sourceComponent: Component {
            EpubReader {
                book: view.openBook
                onClosed: { view.openBook = null; view.loadRecent() }
            }
        }
    }
}

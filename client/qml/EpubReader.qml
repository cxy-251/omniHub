import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// EPUB 阅读器：一次渲染一章（章节 XHTML 只取 <body>，套深色排版样式）。
// 进度 = 章节序号 + 章内滚动比例，滚动停止 1 秒后保存；打开时恢复。
// 键盘：←/→ 上/下一章，空格/PageDown 下翻一屏，PageUp 上翻，↑/↓ 滚动，Esc 返回书架。
Rectangle {
    id: reader
    required property var book
    signal closed()

    property var info: null              // {title, author, chapters:[{title, entry}], asset_base}
    property int chapter: 0
    property real pendingRatio: 0
    property string html: ""
    property string error: ""
    property bool restored: false
    color: "#0d1117"
    focus: true
    Component.onCompleted: { forceActiveFocus(); load() }
    Component.onDestruction: saveNow()

    Settings { id: prefs; category: "reader"; property int fontSize: 20 }

    function request(method, path, body, done) {
        var url = backendApi.apiUrl(path)
        if (!url) { reader.error = "无法连接 omniHub 服务"; return }
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function () {
            if (xhr.readyState !== XMLHttpRequest.DONE || !reader) return
            if (xhr.status !== 200) { reader.error = "请求失败（" + xhr.status + "）"; return }
            if (done) done(xhr.responseText)
        }
        xhr.open(method, url)
        if (body) xhr.setRequestHeader("Content-Type", "application/json")
        xhr.send(body ? JSON.stringify(body) : null)
    }
    function load() {
        var q = encodeURIComponent(book.id)
        request("GET", "/api/media/text/epub/book?path=" + q, null, function (text) {
            reader.info = JSON.parse(text)
            request("GET", "/api/media/text/progress?document_id=" + q, null, function (t) {
                var p = JSON.parse(t).position
                showChapter(p ? Math.min(p.chapter_index, reader.info.chapters.length - 1) : 0, p ? p.scroll_ratio : 0)
            })
        })
    }
    // 章节 XHTML → 可渲染的 HTML：只保留 body，去掉脚本与原样式，换成深色排版。
    function prepare(xhtml) {
        var m = xhtml.match(/<body[^>]*>([\s\S]*)<\/body>/i)
        var body = (m ? m[1] : xhtml).replace(/<script[\s\S]*?<\/script>/gi, "").replace(/<style[\s\S]*?<\/style>/gi, "")
        // Calibre 封面页把图片包在 <svg><image xlink:href> 里，Qt 富文本不支持 SVG，改成 <img>。
        body = body.replace(/<image[^>]*?(?:xlink:)?href="([^"]+)"[^>]*>/gi, '<p align="center"><img src="$1" width="' + Math.round(Math.min(column.width, 480)) + '"/></p>')
                   .replace(/<\/?svg[^>]*>/gi, "")
        // Qt 富文本只识别 <head> 里的 <style>。
        return "<html><head><style>p { text-indent: " + 2 * prefs.fontSize + "px; margin-top: 0; margin-bottom: 0.6em; line-height: 165%; }"
             + " h1, h2, h3 { color: #58a6ff; text-align: center; margin-bottom: 1em; } a { color: #58a6ff; }</style></head><body>"
             + body + "</body></html>"
    }
    function showChapter(index, ratio) {
        if (!info || index < 0 || index >= info.chapters.length) return
        saveNow()
        chapter = index
        pendingRatio = ratio
        restored = false          // 新章节排版完成并恢复位置前不保存
        html = ""
        var entry = info.chapters[index].entry
        request("GET", info.asset_base + encodeURI(entry), null, function (text) {
            reader.html = reader.prepare(text)
        })
    }
    function restoreScroll() {
        flick.contentY = Math.max(0, pendingRatio * (flick.contentHeight - flick.height))
        restored = true
    }
    // 短章节（不足一屏）不会触发高度变化，排版后直接视为已恢复。
    Timer { running: !reader.restored && reader.html.length > 0; interval: 300; onTriggered: if (!reader.restored && flick.contentHeight <= flick.height) reader.restored = true }
    function ratio() { return flick.contentHeight > flick.height ? Math.min(1, Math.max(0, flick.contentY / (flick.contentHeight - flick.height))) : 0 }
    function saveNow() {
        if (!info || !restored) return
        saveTimer.stop()
        request("POST", "/api/media/text/progress", {document_id: book.id, start_line: 1, byte_offset: 0, chapter_index: chapter, scroll_ratio: ratio()}, null)
    }
    function page(delta) { flick.contentY = Math.max(0, Math.min(flick.contentHeight - flick.height, flick.contentY + delta * (flick.height - 60))) }

    Timer { id: saveTimer; interval: 1000; onTriggered: reader.saveNow() }

    Keys.onPressed: function (event) {
        if (event.key === Qt.Key_Escape || event.key === Qt.Key_Back) reader.closed()
        else if (event.key === Qt.Key_Right) showChapter(chapter + 1, 0)
        else if (event.key === Qt.Key_Left) showChapter(chapter - 1, 0)
        else if (event.key === Qt.Key_Space || event.key === Qt.Key_PageDown) page(1)
        else if (event.key === Qt.Key_PageUp) page(-1)
        else if (event.key === Qt.Key_Down) flick.contentY = Math.min(flick.contentHeight - flick.height, flick.contentY + 60)
        else if (event.key === Qt.Key_Up) flick.contentY = Math.max(0, flick.contentY - 60)
        else return
        event.accepted = true
    }

    // 顶栏
    Rectangle {
        id: bar
        anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
        height: 40
        color: "#161b22"
        z: 2
        RowLayout {
            anchors.fill: parent; anchors.leftMargin: 6; anchors.rightMargin: 6
            spacing: 2
            IconButton { glyph: "back"; onClicked: reader.closed(); ToolTip.text: "返回书架（Esc）" }
            IconButton { glyph: "list"; checked: toc.visible; onClicked: toc.visible = !toc.visible; ToolTip.text: "目录" }
            Label {
                Layout.fillWidth: true; elide: Text.ElideRight; color: "#c9d1d9"; font.pixelSize: 14
                text: reader.error || (reader.info ? reader.info.title + "  ·  " + reader.info.chapters[reader.chapter].title + "（" + (reader.chapter + 1) + "/" + reader.info.chapters.length + "）" : "加载中…")
            }
            Chip { text: "A−"; onClicked: prefs.fontSize = Math.max(14, prefs.fontSize - 2) }
            Label { text: prefs.fontSize; color: "#8b949e"; font.pixelSize: 12 }
            Chip { text: "A+"; onClicked: prefs.fontSize = Math.min(36, prefs.fontSize + 2) }
            IconButton { glyph: "chevron_left"; enabled: reader.chapter > 0; onClicked: reader.showChapter(reader.chapter - 1, 0); ToolTip.text: "上一章（←）" }
            IconButton { glyph: "chevron_right"; enabled: !!reader.info && reader.chapter < reader.info.chapters.length - 1; onClicked: reader.showChapter(reader.chapter + 1, 0); ToolTip.text: "下一章（→）" }
        }
    }

    Flickable {
        WheelScroll { }
        id: flick
        anchors.top: bar.bottom; anchors.bottom: parent.bottom
        anchors.left: parent.left; anchors.right: parent.right
        contentWidth: width
        contentHeight: column.height + 80
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        onContentYChanged: if (reader.restored) saveTimer.restart()
        // 长章节排版完成后内容高度才确定，此时再按比例恢复位置。
        onContentHeightChanged: if (!reader.restored && reader.html.length > 0 && contentHeight > height) reader.restoreScroll()
        ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }
        Column {
            id: column
            width: Math.min(flick.width - 48, 880)
            x: (flick.width - width) / 2
            y: 24
            spacing: 24
            Text {
                width: parent.width
                textFormat: Text.RichText
                text: reader.html
                wrapMode: Text.Wrap
                color: "#d0d7de"
                font.pixelSize: prefs.fontSize
                // 章节里的图片按相对路径从同一 EPUB 读取（经本机服务）。
                baseUrl: reader.info ? backendApi.apiUrl(reader.info.asset_base + encodeURI(reader.info.chapters[reader.chapter].entry)) : ""
                onLinkActivated: function (link) { }
            }
            Chip {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: !!reader.info && reader.html.length > 0 && reader.chapter < reader.info.chapters.length - 1
                text: "下一章 ▶"
                onClicked: reader.showChapter(reader.chapter + 1, 0)
            }
        }
    }
    BusyIndicator { anchors.centerIn: parent; running: !reader.error && reader.html.length === 0 }

    // 目录
    Rectangle {
        id: toc
        visible: false
        anchors.top: bar.bottom; anchors.bottom: parent.bottom; anchors.left: parent.left
        width: Math.min(360, parent.width * 0.4)
        color: "#f2161b22"
        z: 2
        ListView {
            WheelScroll { }
            id: tocList
            anchors.fill: parent
            clip: true
            model: reader.info ? reader.info.chapters : []
            currentIndex: reader.chapter
            onVisibleChanged: if (visible) positionViewAtIndex(reader.chapter, ListView.Center)
            ScrollBar.vertical: ScrollBar {}
            delegate: ItemDelegate {
                required property var modelData
                required property int index
                width: ListView.view.width
                height: 32
                highlighted: index === reader.chapter
                contentItem: Label { text: modelData.title; elide: Text.ElideRight; font.pixelSize: 13; color: index === reader.chapter ? "#58a6ff" : "#c9d1d9" }
                onClicked: { reader.showChapter(index, 0); toc.visible = false }
            }
        }
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtCore

// 普通漫画：中文作品目录 → 封面与简介 → 选择资源版本 → 下载列表。
ColumnLayout {
    id: panel
    property var kind: ({fields: []})
    property var tools: ({available: false})
    property var catalog: ({items: [], total: 0, size: 24})
    property bool searching: false
    property bool searched: false
    property string searchedQuery: ""
    property int resultPage: 1
    property string error: ""
    property var queue: []
    property var selectedBook: null
    property var releases: []
    property bool loadingReleases: false
    property bool releasesLoaded: false
    property int releasePage: 1
    property bool moreReleases: false
    property string releaseError: ""
    property string releaseNotice: ""
    property var releaseWarnings: []
    property int releaseRequest: 0
    property bool manual: false
    signal run(var params)
    spacing: 8

    Settings {
        id: prefs; category: "bt-manga"
        property string queueJSON: "[]"
        property string library: ""
        property string lastQuery: ""
        property string language: "3_0"
        property string catalogSource: "all"
    }
    ApiClient { id: api; onErrorChanged: if (error) panel.error = error }
    readonly property var libraryField: (kind.fields || []).find(function (f) { return f.name === "library" }) || {options: [], default: ""}
    function coverURL(book) { return book && book.cover ? backendApi.apiUrl("/api/downloads/bt/cover?url=" + encodeURIComponent(book.cover)) : "" }
    function refreshTools() { api.getJSON("/api/downloads/bt/status", function (s) { panel.tools = s }) }
    function saveQueue(list) { panel.queue = list; prefs.queueJSON = JSON.stringify(list) }
    function bookKey(book) { return book.key || book.subjectKey || ((book.id || book.subjectID) ? "bangumi:" + (book.id || book.subjectID) : "") }
    function queued(key) { return queue.some(function (b) { return panel.bookKey(b) === key }) }
    function hasVersion(torrent) { return queue.some(function (b) { return b.sources.indexOf(torrent) >= 0 }) }
    function loadCatalog(term, page) {
        if (searching) return
        Qt.inputMethod.commit()
        searchedQuery = term.trim(); prefs.lastQuery = searchedQuery
        resultPage = page; searching = true; error = ""
        api.send("GET", "/api/downloads/bt/catalog?q=" + encodeURIComponent(searchedQuery) + "&source=" + prefs.catalogSource + "&page=" + page, null,
                 function (s) { panel.catalog = s; panel.searching = false; panel.searched = true },
                 function (xhr) { panel.searching = false; panel.catalog = {items: [], total: 0, size: 24}; panel.error = xhr.responseText || "连接漫画目录失败，请重试" })
    }
    function search() { Qt.inputMethod.commit(); loadCatalog(query.text, 1) }
    function openBook(book) {
        selectedBook = book; releaseNotice = ""; versions.open(); loadReleases(1)
    }
    function loadReleases(page) {
        if (!selectedBook) return
        var request = ++releaseRequest
        releasePage = page; releases = []; loadingReleases = true; releasesLoaded = false; releaseError = ""; releaseWarnings = []; moreReleases = false
        api.send("GET", "/api/downloads/bt/releases?key=" + encodeURIComponent(bookKey(selectedBook)) + "&category=" + prefs.language + "&page=" + page, null,
                 function (s) {
                     if (request !== panel.releaseRequest) return
                     panel.loadingReleases = false; panel.releasesLoaded = true; panel.releases = s.items || []; panel.moreReleases = !!s.has_more; panel.releaseWarnings = s.warnings || []
                 },
                 function (xhr) {
                     if (request !== panel.releaseRequest) return
                     panel.loadingReleases = false; panel.releaseError = xhr.responseText || "资源查询失败，请重试"
                 })
    }
    function chooseVersion(release) {
        var book = selectedBook
        // 一个作品默认只选择一个版本，换版本时替换，避免把不同语言混订。
        var title = book.title.replace(/[\/\\:*?"<>|\r\n\t]/g, " ").trim().replace(/^\.+|\.+$/g, "").slice(0, 50)
        if (!title) { releaseError = "书名无法用作文件名，请通过已有种子入口填写书名"; return }
        var entry = {title: title, subjectKey: bookKey(book), subjectID: book.id || 0, cover: book.cover, releaseTitle: release.title, language: release.language, sources: [release.torrent]}
        var list = queue.slice(), index = list.findIndex(function (b) { return panel.bookKey(b) === panel.bookKey(book) || b.title.toLowerCase() === title.toLowerCase() })
        if (index >= 0) list[index] = entry
        else list.push(entry)
        saveQueue(list); releaseNotice = "已选择“" + book.title + "”，可关闭此窗口，继续选书或在右侧开始下载。"
    }
    function addBook() {
        Qt.inputMethod.commit()
        var title = bookTitle.text.trim(), src = sources.text.split("\n").map(function (s) { return s.trim() }).filter(function (s) { return !!s })
        if (!title || !src.length) { panel.error = "请填写书名和种子／磁力链接"; return }
        var list = queue.slice(), index = list.findIndex(function (b) { return b.title.toLowerCase() === title.toLowerCase() })
        if (index >= 0) { var entry = Object.assign({}, list[index]); entry.sources = Array.from(new Set(entry.sources.concat(src))); list[index] = entry }
        else list.push({title: title, sources: src})
        saveQueue(list); sources.text = ""; panel.error = ""
    }
    Component.onCompleted: {
        try { var saved = JSON.parse(prefs.queueJSON); queue = Array.isArray(saved) ? saved : [] } catch (_) { queue = [] }
        refreshTools(); query.text = prefs.lastQuery; loadCatalog(query.text, 1)
    }
    FileDialog {
        id: torrentDialog; title: "选择漫画种子"; nameFilters: ["BitTorrent 种子 (*.torrent)"]
        onAccepted: sources.text += (sources.text ? "\n" : "") + decodeURIComponent(selectedFile.toString().replace(/^file:\/\//, ""))
    }

    Label { Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12; text: "搜索日漫、国漫和欧美漫画，点封面看作品、选择下载版本，再加入右侧列表。资料来自 Bangumi、MangaDex 和 Open Library；资源来自动漫花园、Nyaa 和 APIBay 漫画分类。" }
    RowLayout {
        TextField { id: query; Layout.preferredWidth: 290; implicitHeight: 32; placeholderText: "漫画名，如 一人之下、蜘蛛侠、Spider-Man"; onAccepted: if (!inputMethodComposing) panel.search() }
        ComboBox {
            implicitHeight: 32; implicitWidth: 190; font.pixelSize: 12; textRole: "label"; valueRole: "value"
            enabled: !panel.searching
            model: panel.tools.catalogs || [{value: "all", label: "全部目录"}]
            currentIndex: Math.max(0, model.findIndex(function (o) { return o.value === prefs.catalogSource }))
            onActivated: { prefs.catalogSource = currentValue; panel.loadCatalog(query.text, 1) }
        }
        PrimaryButton { text: panel.searching ? "搜索中…" : "搜索漫画"; enabled: !panel.searching && (query.text.trim() !== "" || query.preeditText !== ""); onClicked: panel.search() }
        Chip { text: "浏览漫画"; selected: !panel.searchedQuery; enabled: !panel.searching; onClicked: { query.text = ""; panel.loadCatalog("", 1) } }
        Item { Layout.fillWidth: true }
        Label { text: panel.tools.available ? "aria2 已就绪" : "未找到 aria2，可先选书"; color: panel.tools.available ? "#3fb950" : "#d29922"; font.pixelSize: 11 }
        Chip { text: "刷新依赖"; onClicked: panel.refreshTools(); ToolTip.visible: hovered; ToolTip.text: "aria2：" + (panel.tools.aria2 || "未安装") + "\n7z（用于 RAR）：" + (panel.tools.sevenzip || "未安装") }
        Chip { text: "安装说明"; visible: !panel.tools.available; onClicked: Qt.openUrlExternally("https://aria2.github.io/") }
    }
    Label { visible: panel.error !== ""; text: panel.error; color: "#f85149"; Layout.fillWidth: true; wrapMode: Text.Wrap }
    Label { visible: !!(panel.catalog.warnings || []).length; text: "部分目录查询失败：" + (panel.catalog.warnings || []).join("；"); color: "#d29922"; Layout.fillWidth: true; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight; font.pixelSize: 11 }

    RowLayout {
        Layout.fillWidth: true; Layout.fillHeight: true; spacing: 10
        ColumnLayout {
            Layout.fillWidth: true; Layout.fillHeight: true; spacing: 6
            RowLayout {
                Label { Layout.fillWidth: true; color: "#8b949e"; font.pixelSize: 12; text: panel.searching ? "正在查找漫画…" : panel.searched ? (panel.searchedQuery ? "“" + panel.searchedQuery + "”" : "浏览漫画") + " · 本页 " + panel.catalog.items.length + " 部 · 第 " + panel.resultPage + " 页" : "" }
                Chip { text: "上一页"; enabled: !panel.searching && panel.resultPage > 1; onClicked: panel.loadCatalog(panel.searchedQuery, panel.resultPage - 1) }
                Chip { text: "下一页"; enabled: !panel.searching && !!panel.catalog.has_more; onClicked: panel.loadCatalog(panel.searchedQuery, panel.resultPage + 1) }
            }
            GridView {
                id: grid
                Layout.fillWidth: true; Layout.fillHeight: true; clip: true
                cellWidth: Math.max(150, width / Math.max(1, Math.floor(width / 180)))
                cellHeight: cellWidth * 1.36 + 125
                model: panel.catalog.items
                WheelScroll {}
                ScrollBar.vertical: ScrollBar {}
                Label { anchors.centerIn: parent; visible: !panel.searching && panel.searched && grid.count === 0 && !panel.error; text: panel.catalog.total > 0 ? "本页没有匹配的漫画，可翻页或更换名称。" : "没有找到漫画，可以试试其他中文名或别名。"; color: "#8b949e" }
                delegate: Item {
                    id: cell
                    required property var modelData
                    width: grid.cellWidth; height: grid.cellHeight
                    Rectangle {
                        anchors.fill: parent; anchors.margins: 4; radius: 6; color: "#161b22"; clip: true
                        border.color: panel.queued(panel.bookKey(cell.modelData)) ? "#58a6ff" : cardMouse.containsMouse ? "#484f58" : "#30363d"
                        Image {
                            id: cover
                            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 1
                            height: grid.cellWidth * 1.36 - 8; fillMode: Image.PreserveAspectFit
                            asynchronous: true; sourceSize.width: 360; source: panel.coverURL(cell.modelData)
                        }
                        Label { anchors.centerIn: cover; visible: cover.status !== Image.Ready; text: cover.status === Image.Loading ? "封面加载中…" : "暂无封面"; color: "#6e7681"; font.pixelSize: 12 }
                        Label { visible: panel.queued(panel.bookKey(cell.modelData)); anchors.left: cover.left; anchors.top: cover.top; anchors.margins: 6; padding: 4; text: "✓ 在下载列表"; color: "white"; font.pixelSize: 11; background: Rectangle { radius: 4; color: "#1f6feb" } }
                        Column {
                            anchors.left: parent.left; anchors.right: parent.right; anchors.top: cover.bottom; anchors.margins: 7; spacing: 3
                            Label { width: parent.width; text: cell.modelData.title; textFormat: Text.PlainText; color: "#e6edf3"; font.pixelSize: 13; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight }
                            Label { width: parent.width; text: cell.modelData.author || cell.modelData.original; textFormat: Text.PlainText; color: "#8b949e"; font.pixelSize: 11; elide: Text.ElideRight }
                            Label { width: parent.width; text: (cell.modelData.source || "Bangumi") + (cell.modelData.origin ? " · " + cell.modelData.origin : ""); color: "#58a6ff"; font.pixelSize: 10; elide: Text.ElideRight }
                            Label { width: parent.width; text: cell.modelData.summary || "点封面查看作品与资源"; textFormat: Text.PlainText; color: "#8b949e"; font.pixelSize: 11; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight }
                        }
                        MouseArea { id: cardMouse; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor; onClicked: panel.openBook(cell.modelData); ToolTip.visible: containsMouse; ToolTip.text: cell.modelData.title + "\n" + cell.modelData.original; ToolTip.delay: 600 }
                    }
                }
            }
        }

        Rectangle {
            Layout.preferredWidth: 300; Layout.fillHeight: true; color: "#161b22"; radius: 6; border.color: "#30363d"
            ColumnLayout {
                anchors.fill: parent; anchors.margins: 8; spacing: 7
                RowLayout {
                    Label { Layout.fillWidth: true; text: "下载列表（" + panel.queue.length + "）"; color: "#e6edf3"; font.pixelSize: 14 }
                    Chip { text: "清空"; visible: panel.queue.length > 0; onClicked: panel.saveQueue([]) }
                }
                ListView {
                    id: queueList
                    Layout.fillWidth: true; Layout.fillHeight: true; model: panel.queue; clip: true; spacing: 6
                    WheelScroll {}
                    ScrollBar.vertical: ScrollBar {}
                    Label { anchors.centerIn: parent; width: parent.width - 16; visible: queueList.count === 0; text: "点左边的漫画封面，\n选择一个下载版本。"; color: "#6e7681"; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap; font.pixelSize: 12 }
                    delegate: RowLayout {
                        required property var modelData
                        required property int index
                        width: queueList.width; spacing: 7
                        Image { Layout.preferredWidth: 44; Layout.preferredHeight: 64; visible: !!modelData.cover; source: panel.coverURL(modelData); sourceSize.width: 100; asynchronous: true; fillMode: Image.PreserveAspectFit }
                        ColumnLayout {
                            Layout.fillWidth: true; spacing: 3
                            Label { Layout.fillWidth: true; text: modelData.title; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight; color: "#c9d1d9"; font.pixelSize: 12 }
                            Label { Layout.fillWidth: true; text: modelData.language || (modelData.sources.length + " 个来源"); color: "#8b949e"; font.pixelSize: 11 }
                            Label { Layout.fillWidth: true; visible: !!modelData.releaseTitle; text: modelData.releaseTitle || ""; textFormat: Text.PlainText; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight; color: "#6e7681"; font.pixelSize: 10 }
                        }
                        Column {
                            Chip { text: "编辑"; onClicked: { panel.manual = true; bookTitle.text = modelData.title; sources.text = modelData.sources.join("\n") } }
                            Chip { text: "移除"; onClicked: panel.saveQueue(panel.queue.filter(function (_, i) { return i !== index })) }
                        }
                    }
                }
                Chip { text: panel.manual ? "收起已有种子" : "已有种子／合并分卷…"; onClicked: panel.manual = !panel.manual }
                ColumnLayout {
                    visible: panel.manual; Layout.fillWidth: true
                    TextField { id: bookTitle; Layout.fillWidth: true; implicitHeight: 30; placeholderText: "书名（最终文件名）" }
                    ScrollView { Layout.fillWidth: true; Layout.preferredHeight: 72; TextArea { id: sources; wrapMode: TextEdit.WrapAnywhere; placeholderText: "种子网址／磁力链接，一行一个" } }
                    RowLayout {
                        Chip { text: "本机种子…"; onClicked: torrentDialog.open() }
                        Chip { text: "保存到列表"; onClicked: panel.addBook() }
                    }
                    Label { Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 10; color: "#8b949e"; text: "同书名会合并来源，仅合并同一版本的不同分卷。" }
                }
                ComboBox {
                    id: library
                    Layout.fillWidth: true; implicitHeight: 30; font.pixelSize: 12; model: panel.libraryField.options || []; textRole: "label"; valueRole: "value"
                    currentIndex: Math.max(0, model.findIndex(function (o) { return o.value === (prefs.library || panel.libraryField.default) }))
                    onActivated: prefs.library = currentValue
                }
                PrimaryButton { Layout.fillWidth: true; text: "开始下载（" + panel.queue.length + " 部）"; enabled: panel.tools.available && panel.queue.length > 0 && library.count > 0; onClicked: panel.run({books: panel.queue.map(function (b) { return {title: b.title, sources: b.sources} }), library: library.currentValue}) }
                Label { Layout.fillWidth: true; wrapMode: Text.Wrap; text: "按列表顺序下载、合订为 CBZ，放入漫画库 standard/。列表会保留，已完成项目可手动移除。"; color: "#8b949e"; font.pixelSize: 11 }
            }
        }
    }

    Dialog {
        id: versions
        anchors.centerIn: Overlay.overlay
        width: Math.min(1000, panel.width - 24); height: Math.min(740, panel.height - 12)
        modal: true; title: panel.selectedBook ? panel.selectedBook.title : "漫画详情"
        header: Label { text: versions.title; textFormat: Text.PlainText; color: "#c9d1d9"; font.pixelSize: 14; font.bold: true; leftPadding: 12; rightPadding: 12; topPadding: 12; bottomPadding: 8; elide: Text.ElideRight }
        onClosed: { panel.releaseRequest++; panel.loadingReleases = false }
        background: Rectangle { color: "#0d1117"; border.color: "#30363d"; radius: 8 }
        contentItem: ColumnLayout {
            spacing: 8
            RowLayout {
                Layout.fillWidth: true; Layout.minimumHeight: 174; Layout.preferredHeight: 174; Layout.maximumHeight: 174; spacing: 14
                Image { Layout.minimumWidth: 122; Layout.preferredWidth: 122; Layout.maximumWidth: 122; Layout.preferredHeight: 174; Layout.alignment: Qt.AlignTop; source: panel.coverURL(panel.selectedBook); sourceSize.width: 250; asynchronous: true; fillMode: Image.PreserveAspectFit }
                ColumnLayout {
                    Layout.fillWidth: true; Layout.minimumWidth: 0; Layout.fillHeight: true; spacing: 5
                    Label { Layout.fillWidth: true; text: panel.selectedBook ? panel.selectedBook.title : ""; textFormat: Text.PlainText; color: "#e6edf3"; font.pixelSize: 20; wrapMode: Text.Wrap }
                    Label { Layout.fillWidth: true; text: panel.selectedBook ? panel.selectedBook.original + (panel.selectedBook.author ? " · " + panel.selectedBook.author : "") : ""; textFormat: Text.PlainText; color: "#8b949e"; font.pixelSize: 12; wrapMode: Text.Wrap }
                    Label { Layout.fillWidth: true; text: panel.selectedBook ? (panel.selectedBook.source || "Bangumi") + (panel.selectedBook.origin ? " · " + panel.selectedBook.origin : "") + (panel.selectedBook.volumes ? " · 目录记载 " + panel.selectedBook.volumes + " 卷" : "") + " · " + (panel.selectedBook.tags || []).slice(0, 5).join(" / ") : ""; color: "#8b949e"; font.pixelSize: 11; elide: Text.ElideRight }
                    ScrollView { id: summaryView; Layout.fillWidth: true; Layout.minimumWidth: 0; Layout.fillHeight: true; Layout.minimumHeight: 0; clip: true; contentWidth: availableWidth; Label { width: summaryView.availableWidth; text: panel.selectedBook ? panel.selectedBook.summary || "暂无作品简介" : ""; color: "#c9d1d9"; font.pixelSize: 12; wrapMode: Text.Wrap; textFormat: Text.PlainText } }
                }
            }
            RowLayout {
                Label { Layout.fillWidth: true; text: "选择下载版本"; color: "#e6edf3"; font.pixelSize: 14 }
                ComboBox {
                    implicitHeight: 30; implicitWidth: 180; font.pixelSize: 12; textRole: "label"; valueRole: "value"
                    model: [{label: "全部语言（中文优先）", value: "3_0"}, {label: "仅标注中文的资源", value: "zh"}, {label: "英文译本", value: "3_1"}, {label: "原文", value: "3_3"}]
                    currentIndex: Math.max(0, model.findIndex(function (o) { return o.value === prefs.language }))
                    onActivated: { prefs.language = currentValue; panel.loadReleases(1) }
                }
                Chip { text: "重新查询"; enabled: !panel.loadingReleases; onClicked: panel.loadReleases(panel.releasePage) }
                Chip { text: "作品资料"; onClicked: if (panel.selectedBook) Qt.openUrlExternally(panel.selectedBook.page_url || ("https://bgm.tv/subject/" + panel.selectedBook.id)) }
            }
            Label { Layout.fillWidth: true; wrapMode: Text.Wrap; text: "封面和简介用于识别作品；下面的资源由不同发布者提供，语言、版次、卷数和彩色版以资源标题和发布页为准。"; color: "#8b949e"; font.pixelSize: 11 }
            Label { visible: panel.releaseNotice !== ""; Layout.fillWidth: true; wrapMode: Text.Wrap; text: panel.releaseNotice; color: "#3fb950"; font.pixelSize: 12 }
            Label { visible: panel.releaseError !== ""; Layout.fillWidth: true; wrapMode: Text.Wrap; text: panel.releaseError; color: "#f85149"; font.pixelSize: 12 }
            Label { visible: panel.releaseWarnings.length > 0; Layout.fillWidth: true; wrapMode: Text.Wrap; text: "部分来源查询失败：" + panel.releaseWarnings.join("；"); color: "#d29922"; font.pixelSize: 11 }
            ListView {
                id: versionList
                Layout.fillWidth: true; Layout.fillHeight: true; Layout.minimumHeight: 120; model: panel.releases; spacing: 6; clip: true
                ScrollBar.vertical: ScrollBar {}
                WheelScroll {}
                Label {
                    anchors.centerIn: parent; width: parent.width - 24; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 13
                    visible: panel.loadingReleases || (panel.releasesLoaded && !versionList.count)
                    text: panel.loadingReleases ? "正在按作品原名和别名查找下载资源…" : panel.releaseWarnings.length ? "已成功连接的来源没有找到资源，另有来源查询失败，可重试。" : prefs.language === "zh" ? "暂未找到明确标注中文的资源，可以切换到全部语言。" : "已找到作品资料，但暂未找到下载资源。也可以使用已有种子入口。"
                }
                delegate: Rectangle {
                    required property var modelData
                    width: versionList.width; height: versionColumn.implicitHeight + 16; color: "#161b22"; radius: 5
                    ColumnLayout {
                        id: versionColumn
                        anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 8; spacing: 5
                        Label { Layout.fillWidth: true; text: modelData.title; textFormat: Text.PlainText; wrapMode: Text.Wrap; color: "#e6edf3"; font.pixelSize: 13 }
                        RowLayout {
                            Label { Layout.fillWidth: true; text: modelData.language + " · " + modelData.source + " · " + (modelData.size || "大小未提供") + (modelData.seeders >= 0 ? " · 做种 " + modelData.seeders : " · 做种数未提供"); color: modelData.language.indexOf("中文") >= 0 ? "#3fb950" : "#8b949e"; font.pixelSize: 11; wrapMode: Text.Wrap }
                            Chip { text: "发布页"; onClicked: Qt.openUrlExternally(modelData.page) }
                            Chip { text: panel.hasVersion(modelData.torrent) ? "已在列表" : panel.selectedBook && panel.queued(panel.bookKey(panel.selectedBook)) ? "改选此版" : "加入下载列表"; enabled: !panel.hasVersion(modelData.torrent); selected: !enabled; onClicked: panel.chooseVersion(modelData) }
                        }
                    }
                }
            }
            RowLayout {
                Chip { text: "上一页"; enabled: !panel.loadingReleases && panel.releasePage > 1; onClicked: panel.loadReleases(panel.releasePage - 1) }
                Label { text: "第 " + panel.releasePage + " 页"; color: "#8b949e"; font.pixelSize: 12 }
                Chip { text: "下一页"; enabled: !panel.loadingReleases && panel.moreReleases; onClicked: panel.loadReleases(panel.releasePage + 1) }
                Item { Layout.fillWidth: true }
                PrimaryButton { text: "继续选书"; onClicked: versions.close() }
            }
        }
    }
}

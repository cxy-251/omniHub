import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 18comic 漫画下载（参照 omniDeck 的做法，但不并发）：
//   联网搜索（关键词、本子编号或网址）或看日/周/月榜 → 点封面加进下载列表 → 开始下载。
//   下载按列表顺序一本一本来，每本整本下好后封成 CBZ 放进所选资源库的 manga/nsfw/。
//   搜索结果标出已经下载过的本子（各库漫画包里记着本子编号），加列表时自动跳过；下载时已有的也会跳过，线上有新章节的整本更新。
// 下载列表存在本机设置里，关掉再开还在；开始下载后列表清空，进度在「任务」里。
ColumnLayout {
    id: panel
    required property var kind
    signal run(var params)
    spacing: 8

    Settings { id: store; category: "manga-download"; property string queueJson: "[]"; property string order: "mv"; property bool hideDownloaded: false; property string library: ""; property string lastSource: ""; property string lastQuery: "" }
    property var queue: JSON.parse(store.queueJson)
    function saveQueue(list) { queue = list; store.queueJson = JSON.stringify(list) }
    function queued(id) { return queue.some(function (q) { return q.id === id }) }
    function toggle(album) {
        if (album.downloaded || album.active) return
        if (queued(album.id)) saveQueue(queue.filter(function (q) { return q.id !== album.id }))
        else saveQueue(queue.concat([{id: album.id, title: album.title}]))
    }

    property var result: null           // 当前一页结果
    property string source: ""          // "search" / "day" / "week" / "month"
    property string query: ""
    property int page: 1
    property bool busy: false
    property string error: ""
    ApiClient { id: api }

    readonly property var libraryField: (kind.fields || []).find(function (f) { return f.name === "library" }) || null
    readonly property string library: !libraryField ? "" : (libraryField.options || []).some(function (o) { return o.value === store.library }) ? store.library : (libraryField.default || "")

    // 以前的任务没下完（被停止、失败或服务端重启打断）的本子：提示一键加回下载列表
    property var unfinished: []
    function loadUnfinished() { api.getJSON("/api/downloads/manga/unfinished", function (list) { panel.unfinished = list.filter(function (a) { return !panel.queued(a.id) }) }) }
    // 再打开这页时接着显示上次的搜索或榜单（第一页）
    Component.onCompleted: {
        loadUnfinished()
        if (store.lastSource) { searchField.text = store.lastQuery; load(store.lastSource, store.lastQuery, 1) }
    }
    function load(src, q, p) {
        source = src; query = q; page = p; busy = true; error = ""
        store.lastSource = src; store.lastQuery = q
        var path = src === "search"
            ? "/api/downloads/manga/search?q=" + encodeURIComponent(q) + "&page=" + p + "&order=" + store.order
            : "/api/downloads/manga/ranking?type=" + src + "&page=" + p
        api.send("GET", path, null, function (r) { panel.result = r; panel.busy = false },
                 function (xhr) { panel.busy = false; panel.error = xhr.responseText || ("请求失败（" + xhr.status + "）") })
    }
    readonly property var shown: !result ? [] : result.results.filter(function (a) { return !(store.hideDownloaded && a.downloaded) })

    Label {
        Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
        text: "搜索 18comic（禁漫）的本子，点封面加进右边的下载列表，再「开始下载」。一本一本下，整本下好后封成 CBZ 放进漫画库的 nsfw/。已经下载过的、正在下载的会标出来，不能再加；下载时已有的跳过，线上出了新章节的整本更新。"
    }

    Rectangle {
        visible: panel.unfinished.length > 0
        Layout.fillWidth: true
        implicitHeight: unfinishedRow.implicitHeight + 12
        radius: 6; color: "#2d2410"; border.color: "#9e6a03"
        RowLayout {
            id: unfinishedRow
            anchors.left: parent.left; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter; anchors.margins: 8
            Label {
                Layout.fillWidth: true; elide: Text.ElideRight; color: "#e3b341"; font.pixelSize: 12
                text: "上次有 " + panel.unfinished.length + " 本没下完：" + panel.unfinished.slice(0, 3).map(function (a) { return a.title }).join("、") + (panel.unfinished.length > 3 ? " 等" : "")
            }
            Chip {
                text: "加回下载列表"
                onClicked: { panel.saveQueue(panel.queue.concat(panel.unfinished.map(function (a) { return {id: a.id, title: a.title} }))); panel.unfinished = [] }
            }
        }
    }

    RowLayout {
        spacing: 6
        TextField {
            id: searchField
            Layout.preferredWidth: 340; implicitHeight: 32
            placeholderText: "关键词、本子编号或网址（多个编号用空格隔开）"
            onAccepted: if (text.trim()) panel.load("search", text.trim(), 1)
        }
        ComboBox {
            implicitHeight: 32; implicitWidth: 120; font.pixelSize: 12
            textRole: "label"; valueRole: "value"
            model: [{value: "mv", label: "最多点击"}, {value: "mr", label: "最新上架"}, {value: "tf", label: "最多爱心"}, {value: "mp", label: "最多图片"}]
            currentIndex: Math.max(0, model.findIndex(function (o) { return o.value === store.order }))
            onActivated: { store.order = currentValue; if (panel.source === "search") panel.load("search", panel.query, 1) }
        }
        PrimaryButton { text: "搜索"; enabled: !panel.busy && searchField.text.trim() !== ""; onClicked: panel.load("search", searchField.text.trim(), 1) }
        Divider { Layout.leftMargin: 4; Layout.rightMargin: 4 }
        Chip { text: "日榜"; selected: panel.source === "day"; enabled: !panel.busy; onClicked: panel.load("day", "", 1) }
        Chip { text: "周榜"; selected: panel.source === "week"; enabled: !panel.busy; onClicked: panel.load("week", "", 1) }
        Chip { text: "月榜"; selected: panel.source === "month"; enabled: !panel.busy; onClicked: panel.load("month", "", 1) }
        Divider { Layout.leftMargin: 4; Layout.rightMargin: 4 }
        Toggle { text: "隐藏已下载"; checked: store.hideDownloaded; onToggled: store.hideDownloaded = checked }
    }

    RowLayout {
        Layout.fillWidth: true
        Layout.fillHeight: true
        spacing: 10

        // 结果
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 6
            RowLayout {
                visible: panel.busy || panel.error !== "" || !!panel.result
                Label {
                    Layout.fillWidth: true; elide: Text.ElideRight; font.pixelSize: 12
                    color: panel.error ? "#f85149" : "#8b949e"
                    text: panel.busy ? "正在查询 18comic…（每次要几秒）"
                        : panel.error ? panel.error
                        : panel.result ? "共 " + panel.result.total + " 本 · 第 " + panel.page + "/" + Math.max(1, panel.result.page_count) + " 页"
                            + " · 本页已下载 " + panel.result.results.filter(function (a) { return a.downloaded }).length + " 本" : ""
                }
                Chip {
                    text: "本页全部加入"
                    visible: panel.shown.some(function (a) { return !a.downloaded && !a.active && !panel.queued(a.id) })
                    onClicked: panel.saveQueue(panel.queue.concat(panel.shown.filter(function (a) { return !a.downloaded && !a.active && !panel.queued(a.id) }).map(function (a) { return {id: a.id, title: a.title} })))
                }
                Chip { text: "上一页"; enabled: !panel.busy && panel.page > 1; onClicked: panel.load(panel.source, panel.query, panel.page - 1) }
                Chip { text: "下一页"; enabled: !panel.busy && !!panel.result && panel.page < panel.result.page_count; onClicked: panel.load(panel.source, panel.query, panel.page + 1) }
            }
            GridView {
                id: grid
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                cellWidth: Math.max(150, width / Math.max(1, Math.floor(width / 170)))
                cellHeight: cellWidth * 4 / 3 + 58
                model: panel.shown
                WheelScroll { }
                ScrollBar.vertical: ScrollBar {}
                Label { anchors.centerIn: parent; visible: !panel.busy && !panel.result; text: "搜索或打开一个榜单"; color: "#6e7681" }
                delegate: Item {
                    id: cell
                    required property var modelData
                    readonly property bool inQueue: panel.queued(modelData.id)
                    width: grid.cellWidth; height: grid.cellHeight
                    Rectangle {
                        anchors.fill: parent; anchors.margins: 4
                        radius: 6; color: "#161b22"
                        border.color: cell.inQueue ? "#58a6ff" : mouse.containsMouse ? "#484f58" : "#30363d"
                        border.width: cell.inQueue ? 2 : 1
                        clip: true
                        Image {
                            id: cover
                            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 1
                            height: width * 4 / 3
                            fillMode: Image.PreserveAspectCrop
                            asynchronous: true
                            source: backendApi.apiUrl("/api/downloads/manga/cover?id=" + cell.modelData.id)
                            opacity: cell.modelData.downloaded || cell.modelData.active ? 0.45 : 1
                        }
                        Label { anchors.centerIn: cover; visible: cover.status !== Image.Ready; text: cover.status === Image.Error ? "无封面" : "…"; color: "#6e7681" }
                        Label {
                            visible: !!cell.modelData.downloaded || !!cell.modelData.active || cell.inQueue
                            anchors.left: cover.left; anchors.top: cover.top; anchors.margins: 6
                            padding: 4; font.pixelSize: 11; color: "#ffffff"
                            text: cell.modelData.downloaded ? "已下载 · " + cell.modelData.downloaded : cell.modelData.active ? "下载中" : "✓ 在下载列表"
                            background: Rectangle { radius: 4; color: cell.modelData.downloaded ? "#2d333b" : cell.modelData.active ? "#9e6a03" : "#1f6feb" }
                        }
                        Column {
                            anchors.left: parent.left; anchors.right: parent.right; anchors.top: cover.bottom; anchors.margins: 6
                            spacing: 2
                            Label { width: parent.width; text: cell.modelData.title; color: "#e6edf3"; font.pixelSize: 12; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight }
                            Label { width: parent.width; text: "JM" + cell.modelData.id + (cell.modelData.author ? " · " + cell.modelData.author : ""); color: "#8b949e"; font.pixelSize: 11; elide: Text.ElideRight }
                        }
                        MouseArea {
                            id: mouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: cell.modelData.downloaded || cell.modelData.active ? Qt.ArrowCursor : Qt.PointingHandCursor
                            onClicked: panel.toggle(cell.modelData)
                            ToolTip.visible: containsMouse && (cell.modelData.tags || []).length > 0
                            ToolTip.text: (cell.modelData.tags || []).join("、")
                            ToolTip.delay: 600
                        }
                    }
                }
            }
        }

        // 下载列表
        Rectangle {
            Layout.preferredWidth: 300
            Layout.fillHeight: true
            radius: 6; color: "#161b22"; border.color: "#30363d"
            ColumnLayout {
                anchors.fill: parent; anchors.margins: 8
                spacing: 6
                RowLayout {
                    Label { text: "下载列表（" + panel.queue.length + "）"; color: "#e6edf3"; font.pixelSize: 14; Layout.fillWidth: true }
                    Chip { text: "清空"; visible: panel.queue.length > 0; onClicked: panel.saveQueue([]) }
                }
                ListView {
                    id: queueList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: 4
                    model: panel.queue
                    WheelScroll { }
                    ScrollBar.vertical: ScrollBar {}
                    Label { anchors.centerIn: parent; width: parent.width - 16; visible: queueList.count === 0; text: "点左边的封面加进来"; color: "#6e7681"; font.pixelSize: 12; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap }
                    delegate: RowLayout {
                        required property var modelData
                        required property int index
                        width: queueList.width
                        spacing: 4
                        Label { text: (index + 1) + "."; color: "#6e7681"; font.pixelSize: 12 }
                        Label { Layout.fillWidth: true; text: modelData.title; color: "#c9d1d9"; font.pixelSize: 12; elide: Text.ElideRight; ToolTip.visible: hoverArea.containsMouse; ToolTip.text: "JM" + modelData.id + " " + modelData.title
                            MouseArea { id: hoverArea; anchors.fill: parent; hoverEnabled: true } }
                        IconButton { glyph: "close"; size: 22; onClicked: panel.saveQueue(panel.queue.filter(function (q) { return q.id !== modelData.id })); ToolTip.text: "移出列表" }
                    }
                }
                RowLayout {
                    visible: !!panel.libraryField
                    Label { text: "放进"; color: "#8b949e"; font.pixelSize: 12 }
                    ComboBox {
                        Layout.fillWidth: true; implicitHeight: 28; font.pixelSize: 12
                        textRole: "label"; valueRole: "value"
                        model: panel.libraryField ? (panel.libraryField.options || []) : []
                        currentIndex: Math.max(0, model.findIndex(function (o) { return o.value === panel.library }))
                        onActivated: store.library = currentValue
                    }
                }
                PrimaryButton {
                    Layout.fillWidth: true
                    text: "开始下载（" + panel.queue.length + " 本）"
                    enabled: panel.queue.length > 0 && panel.queue.length <= 100
                    onClicked: {
                        panel.run({ids: panel.queue.map(function (q) { return q.id }).join("\n"),
                                   titles: panel.queue.map(function (q) { return q.title.replace(/\n/g, " ") }).join("\n"), library: panel.library})
                        panel.saveQueue([])
                    }
                }
                Label { visible: panel.queue.length > 100; text: "一次最多 100 本"; color: "#f85149"; font.pixelSize: 12 }
            }
        }
    }
}

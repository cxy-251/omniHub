import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import omniHub.Media 1.0

// 暂存区：各资源库根目录下的 staging/。新下载的内容先放这里，按批次预览、删掉不要的，再「移入资源库」。
// 暂存区不属于任何分类，扫描和索引都不碰它；移入时同一块盘上只是改名。
// 文件都在本机，预览直接读本机路径（图片用 Image，视频与音频用内嵌 mpv）。
Item {
    id: panel
    ApiClient { id: api }
    property var libraries: []          // [{library_id, name, batches, error}]
    property string libraryId: ""
    property string batchName: ""
    property string kind: ""             // "" | video | image | audio | other
    property var status: ({})
    property var selected: ({})          // 批次内相对路径 → true
    property var preview: null           // 正在预览的文件
    property bool confirmDelete: false
    property bool confirmImport: false
    property bool confirmCollect: false
    readonly property bool busy: !!status.job
    readonly property var library: libraries.find(function (l) { return l.library_id === libraryId }) || null
    readonly property var batches: library ? library.batches : []
    readonly property var batch: batches.find(function (b) { return b.name === batchName }) || null
    readonly property int selectedCount: Object.keys(selected).length
    readonly property var jobNames: ({collect: "收件", import: "移入资源库"})

    function refresh() {
        api.getJSON("/api/staging/list", function (r) {
            panel.libraries = r
            // 默认选有批次的库；都没有时选和 Chrome 下载目录同一块盘的库（收件只是改名）。
            if (!panel.library && r.length) {
                var pick = r.find(function (l) { return l.batches && l.batches.length }) || r.find(function (l) { return l.near_downloads }) || r[0]
                panel.libraryId = pick.library_id
            }
            if (panel.batchName && !panel.batch) panel.batchName = ""
            if (!panel.batchName && panel.batches.length) panel.batchName = panel.batches[0].name
        })
        api.getJSON("/api/staging/status", function (s) { panel.status = s })
    }
    function act(action, body, done) {
        api.send("POST", "/api/staging/" + action, body, function (r) { if (done) done(r); panel.refresh() })
    }
    // 本机路径 → file:// 地址（逐段编码，文件名里的 # ? 不会被当成地址的一部分）
    function fileUrl(abs) { return "file://" + abs.split("/").map(encodeURIComponent).join("/") }
    function size(bytes) {
        return bytes >= 1 << 30 ? (bytes / (1 << 30)).toFixed(1) + " GB" : bytes >= 1 << 20 ? (bytes / (1 << 20)).toFixed(1) + " MB" : Math.ceil(bytes / 1024) + " KB"
    }
    function kindsText(k) {
        var names = {video: "视频", image: "图片", audio: "音频", other: "其他"}, out = []
        for (var key in names) if (k && k[key]) out.push(names[key] + " " + k[key])
        return out.join(" · ")
    }
    onBatchNameChanged: { selected = {}; preview = null }
    onKindChanged: { selected = {}; preview = null }
    Component.onCompleted: refresh()
    // 后台任务进行中每秒刷新进度；结束时再刷新一次批次列表。
    Timer { interval: 1000; repeat: true; running: panel.busy; onTriggered: api.getJSON("/api/staging/status", function (s) { var was = panel.busy; panel.status = s; if (was && !s.job) panel.refresh() }) }

    PagedModel {
        id: paged
        pageSize: 120
        endpoint: panel.libraryId && panel.batchName
                  ? "/api/staging/files?library_id=" + encodeURIComponent(panel.libraryId) + "&batch=" + encodeURIComponent(panel.batchName) + "&kind=" + panel.kind
                  : ""
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 8

        RowLayout {
            spacing: 6
            Label { text: "资源库"; color: "#c9d1d9"; font.pixelSize: 12 }
            ComboBox {
                implicitHeight: 30; Layout.preferredWidth: 160
                model: panel.libraries; textRole: "name"
                currentIndex: panel.libraries.findIndex(function (l) { return l.library_id === panel.libraryId })
                onActivated: function (i) { panel.libraryId = panel.libraries[i].library_id; panel.batchName = ""; panel.refresh() }
            }
            Chip { text: "从浏览器下载目录收件"; enabled: !panel.busy && !!panel.libraryId; onClicked: panel.library && !panel.library.near_downloads ? panel.confirmCollect = true : panel.act("collect", {library_id: panel.libraryId}) }
            Chip { text: "刷新"; onClicked: panel.refresh() }
            Chip { text: "取消"; visible: panel.busy; onClicked: panel.act("cancel", {}) }
            Label {
                Layout.fillWidth: true; elide: Text.ElideRight; font.pixelSize: 12
                color: panel.status.last_error && !panel.busy ? "#f85149" : "#8b949e"
                text: api.error ? api.error
                    : panel.busy ? "正在" + (panel.jobNames[panel.status.job] || panel.status.job) + "（" + panel.status.library + "）：" + panel.status.done + "/" + panel.status.total
                    : panel.status.last_job ? "上次" + (panel.jobNames[panel.status.last_job] || "") + "：" + (panel.status.last_error ? panel.status.last_error + "；" : "") + (panel.status.last_note || "完成")
                    : "收件：把 Chrome 下载目录里插件下好的「平台/作者」作品移进暂存区（downloaded.txt 留在原处，还在下载的不收）。"
                ToolTip.visible: hoverHandler.hovered && text.length > 40
                ToolTip.text: text
                HoverHandler { id: hoverHandler }
            }
        }

        // 跨盘收件要复制，先确认
        RowLayout {
            visible: panel.confirmCollect
            spacing: 6
            Label {
                Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#e3b341"; font.pixelSize: 12
                text: "「" + (panel.library ? panel.library.name : "") + "」和 Chrome 下载目录不在同一块盘上，收件要把视频复制过去再删除原文件，量大时会很慢。"
                      + "建议收进同一块盘上的资源库，整理好后再移动。仍然收到这里？"
            }
            PrimaryButton { text: "仍然收件"; onClicked: { panel.confirmCollect = false; panel.act("collect", {library_id: panel.libraryId}) } }
            Button { text: "取消"; onClicked: panel.confirmCollect = false }
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 8

            // 批次列表
            ListView {
                Layout.preferredWidth: 260
                Layout.fillHeight: true
                clip: true
                spacing: 4
                model: panel.batches
                WheelScroll { }
                delegate: Rectangle {
                    required property var modelData
                    width: ListView.view.width
                    height: col.implicitHeight + 12
                    radius: 6
                    color: panel.batchName === modelData.name ? "#1f2937" : "#161b22"
                    border.color: panel.batchName === modelData.name ? "#58a6ff" : "#30363d"
                    ColumnLayout {
                        id: col
                        anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 6
                        spacing: 2
                        Label { Layout.fillWidth: true; text: modelData.label; color: "#e6edf3"; font.pixelSize: 13; elide: Text.ElideRight }
                        Label { text: new Date(modelData.created).toLocaleString(Qt.locale(), "yyyy-MM-dd hh:mm"); color: "#8b949e"; font.pixelSize: 11 }
                        Label { Layout.fillWidth: true; text: modelData.files + " 个文件 · " + panel.size(modelData.bytes) + "\n" + panel.kindsText(modelData.kinds); color: "#8b949e"; font.pixelSize: 11; wrapMode: Text.Wrap }
                    }
                    TapHandler { onTapped: panel.batchName = modelData.name }
                }
                Label {
                    anchors.centerIn: parent; visible: !panel.batches.length; width: parent.width - 20
                    text: "暂存区是空的。"; color: "#8b949e"; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap
                }
            }

            // 批次内容
            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: 6
                visible: !!panel.batch
                RowLayout {
                    spacing: 4
                    Repeater {
                        model: [["", "全部"], ["video", "视频"], ["image", "图片"], ["audio", "音频"], ["other", "其他"]]
                        delegate: Chip { required property var modelData; text: modelData[1]; selected: panel.kind === modelData[0]; onClicked: panel.kind = modelData[0] }
                    }
                    Item { Layout.fillWidth: true }
                    Label { text: paged.total + " 个" + (panel.selectedCount ? "，选中 " + panel.selectedCount : ""); color: "#8b949e"; font.pixelSize: 12 }
                    Chip { text: "删除选中"; visible: panel.selectedCount > 0 && !panel.busy; onClicked: panel.act("delete", {library_id: panel.libraryId, batch: panel.batchName, paths: Object.keys(panel.selected)}, function () { panel.selected = {}; panel.preview = null; paged.reset() }) }
                    Chip { text: "删除整批"; enabled: !panel.busy; onClicked: panel.confirmDelete = true }
                    PrimaryButton { text: "移入资源库"; enabled: !panel.busy; onClicked: panel.confirmImport = true }
                }
                // 确认条
                RowLayout {
                    visible: panel.confirmDelete || panel.confirmImport
                    spacing: 6
                    Label {
                        Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#c9d1d9"; font.pixelSize: 12
                        text: panel.confirmDelete ? "把整个批次「" + (panel.batch ? panel.batch.label : "") + "」移到系统回收站？"
                            : "把这个批次的文件移到资源库对应目录（短视频移入后自动扫描）。资源库里已有同名文件的不覆盖，留在暂存区。"
                    }
                    PrimaryButton {
                        text: panel.confirmDelete ? "移到回收站" : "移入"
                        onClicked: {
                            if (panel.confirmDelete) panel.act("delete", {library_id: panel.libraryId, batch: panel.batchName}, function () { panel.batchName = "" })
                            else panel.act("import", {library_id: panel.libraryId, batch: panel.batchName})
                            panel.confirmDelete = false; panel.confirmImport = false
                        }
                    }
                    Button { text: "取消"; onClicked: { panel.confirmDelete = false; panel.confirmImport = false } }
                }
                GridView {
                    id: grid
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    WheelScroll { }
                    readonly property int columns: Math.max(2, Math.round(width / 170))
                    cellWidth: Math.floor(width / columns)
                    cellHeight: cellWidth + 34
                    model: paged.total
                    cacheBuffer: cellHeight * 2
                    ScrollBar.vertical: ScrollBar { minimumSize: 0.05 }
                    delegate: Item {
                        required property int index
                        readonly property var file: { paged.revision; return paged.item(index) }
                        readonly property bool isSelected: !!(file && panel.selected[file.path])
                        width: grid.cellWidth; height: grid.cellHeight
                        Rectangle {
                            anchors.fill: parent; anchors.margins: 3
                            radius: 6; clip: true
                            color: "#161b22"
                            border.color: isSelected ? "#58a6ff" : (panel.preview && file && panel.preview.path === file.path ? "#8b949e" : "#30363d")
                            border.width: isSelected ? 2 : 1
                            Image {
                                visible: !!file && file.kind === "image"
                                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
                                height: parent.width
                                fillMode: Image.PreserveAspectCrop
                                asynchronous: true; cache: false
                                sourceSize.width: width; sourceSize.height: height
                                source: file && file.kind === "image" ? panel.fileUrl(file.abs) : ""
                            }
                            Label {
                                visible: !!file && file.kind !== "image"
                                anchors.horizontalCenter: parent.horizontalCenter; y: parent.width / 2 - height / 2
                                text: !file ? "" : file.kind === "video" ? "▶" : file.kind === "audio" ? "♪" : "📄"
                                color: "#58a6ff"; font.pixelSize: 40
                            }
                            Label {
                                anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom; anchors.margins: 4
                                text: file ? file.path.split("/").slice(-2).join("/") + "\n" + panel.size(file.size) : ""
                                color: "#adbac7"; font.pixelSize: 10; elide: Text.ElideMiddle; maximumLineCount: 2
                            }
                            CheckBox {
                                anchors.right: parent.right; anchors.top: parent.top
                                checked: isSelected
                                onToggled: { var m = Object.assign({}, panel.selected); if (checked) m[file.path] = true; else delete m[file.path]; panel.selected = m }
                            }
                        }
                        TapHandler { onTapped: if (file) panel.preview = file }
                    }
                }
            }

            // 预览
            Rectangle {
                Layout.preferredWidth: Math.round(panel.width * 0.32)
                Layout.fillHeight: true
                visible: !!panel.preview
                color: "black"; radius: 6; clip: true
                Image {
                    anchors.fill: parent
                    visible: !!panel.preview && panel.preview.kind === "image"
                    fillMode: Image.PreserveAspectFit
                    asynchronous: true; cache: false
                    sourceSize.width: width * 2; sourceSize.height: height * 2
                    source: panel.preview && panel.preview.kind === "image" ? panel.fileUrl(panel.preview.abs) : ""
                }
                MpvItem {
                    id: player
                    anchors.fill: parent
                    visible: !!panel.preview && panel.preview.kind === "video"
                    source: panel.preview && (panel.preview.kind === "video" || panel.preview.kind === "audio") ? panel.preview.abs : ""
                    TapHandler { onTapped: player.playing ? player.pause() : player.play() }
                }
                Label {
                    anchors.centerIn: parent
                    visible: !!panel.preview && panel.preview.kind === "audio"
                    text: "♪ " + (player.playing ? "播放中" : "已暂停") + "（点下方按钮）"; color: "#c9d1d9"
                }
                RowLayout {
                    anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom; anchors.margins: 6
                    IconButton { glyph: player.playing ? "pause" : "play"; visible: !!panel.preview && panel.preview.kind !== "image"; onClicked: player.playing ? player.pause() : player.play() }
                    Label { Layout.fillWidth: true; text: panel.preview ? panel.preview.path : ""; color: "#adbac7"; font.pixelSize: 11; elide: Text.ElideMiddle }
                    IconButton { glyph: "close"; onClicked: panel.preview = null }
                }
            }
        }
    }
}

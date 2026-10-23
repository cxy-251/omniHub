import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs

// 相册管理：把 Deck 上的一个文件夹（含子文件夹）导入某个资源库的相册。
// 与 gallery 项目同一套格式：原图写进 originals 里未满 4GB 的分卷，gallery.db 记照片、480px 缩略图和色系。
// 内容相同的照片跳过；源文件夹不改动。导入在服务端进行，切走页面不影响。
ColumnLayout {
    id: page
    spacing: 10
    ApiClient { id: api }
    property var status: ({})
    readonly property bool running: !!status.running
    readonly property var writable: backendApi.libraries.filter(function (l) { return l.is_online && !l.read_only })
    property string folder: ""

    function refresh() { api.getJSON("/api/media/gallery/admin/import", function (r) { page.status = r }) }
    Component.onCompleted: { backendApi.refreshLibraries(); refresh() }
    Timer { interval: 1000; repeat: true; running: page.running; onTriggered: page.refresh() }

    FolderDialog {
        id: folderDialog
        title: "选择要导入的照片文件夹"
        onAccepted: page.folder = decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))
    }

    Label { text: "相册"; color: "#e6edf3"; font.pixelSize: 20 }
    Label {
        Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
        text: "把文件夹里的照片（JPEG、PNG、WebP、GIF，含子文件夹）加进相册。已在相册里的同一张照片会跳过，源文件不会被移动或删除。HEIC 等其他格式暂不支持，会计入「跳过」。"
    }
    RowLayout {
        spacing: 8
        Label { text: "导入到"; color: "#c9d1d9" }
        ComboBox {
            id: libraryBox
            Layout.preferredWidth: 220
            model: page.writable
            textRole: "name"
            enabled: !page.running
            currentIndex: Math.max(0, page.writable.findIndex(function (l) { return l.is_primary }))
        }
    }
    RowLayout {
        spacing: 8
        Button { text: "选择文件夹…"; enabled: !page.running; onClicked: folderDialog.open() }
        Label { Layout.fillWidth: true; text: page.folder || "未选择"; color: page.folder ? "#e6edf3" : "#6e7681"; elide: Text.ElideMiddle; font.family: "monospace" }
    }
    RowLayout {
        spacing: 8
        Button {
            text: "开始导入"
            enabled: !page.running && page.folder !== "" && page.writable.length > 0
            onClicked: api.send("POST", "/api/media/gallery/admin/import",
                                {library_id: page.writable[libraryBox.currentIndex].id, path: page.folder}, function (r) { page.status = r })
        }
        Button { text: "取消"; visible: page.running; onClicked: api.send("DELETE", "/api/media/gallery/admin/import", null, function (r) { page.status = r }) }
    }
    ProgressBar {
        Layout.fillWidth: true
        visible: page.running
        from: 0; to: Math.max(1, page.status.progress ? page.status.progress.Total : 1)
        value: page.status.progress ? page.status.progress.Done : 0
    }
    Label {
        Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 13
        readonly property var p: page.status.progress || {}
        visible: page.running || !!page.status.finished
        color: page.status.error ? "#f85149" : (page.running ? "#c9d1d9" : "#3fb950")
        text: (page.running ? "正在导入 " : (page.status.error ? "导入结束（" + page.status.error + "）：" : "导入完成："))
              + (page.status.source || "") + " → " + (page.status.library || "") + "\n"
              + "已处理 " + (p.Done || 0) + " / " + (p.Total || 0) + "，新增 " + (p.Added || 0) + "，重复跳过 " + (p.Duplicates || 0)
              + "，格式不支持 " + (p.Unsupported || 0) + ((p.Errors || []).length ? "，出错 " + p.Errors.length : "")
    }
    Repeater {
        model: page.status.progress && page.status.progress.Errors ? page.status.progress.Errors : []
        delegate: Label { required property string modelData; text: modelData; color: "#f85149"; font.pixelSize: 12 }
    }
    Label { visible: api.error !== ""; text: api.error; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true }
}

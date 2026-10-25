import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs

// 短视频管理（与网页端管理页同一组接口）：缩略图开关、重新扫描、图集入库、校验后清理已入库的原目录。
// 有任务或缩略图在生成时每 2 秒刷新进度，空闲时不轮询。
ColumnLayout {
    id: page
    property var status: null
    property string error: ""
    property bool confirmClean: false
    property var copyPlan: null
    spacing: 10

    readonly property var worker: status ? status.worker : null
    readonly property bool busy: !!(worker && worker.job)
    readonly property var libraries: status && status.libraries ? status.libraries : []
    readonly property bool thumbsRunning: !!(worker && worker.thumbnails_enabled && libraries.some(function (l) { return l.stats && l.stats.thumbnails + l.stats.thumbnail_failures < l.stats.videos + l.stats.albums }))
    readonly property int cleanable: libraries.reduce(function (n, l) { return n + (l.stats ? l.stats.albums_cleanable : 0) }, 0)
    readonly property int importable: libraries.reduce(function (n, l) { return n + (l.stats ? l.stats.albums_on_disk - l.stats.albums_cleanable : 0) }, 0)
    readonly property var jobNames: ({scan: "扫描", import: "图集入库", clean: "清理已入库原目录", thumbnails: "生成缩略图", merge: "合并其他设备的数据", copy: "补充到其他资源库"})

    readonly property var mergeTargets: libraries.filter(function (l) { return !l.error })
    FolderDialog {
        id: mergeDialog
        title: "选择另一台设备的 shortvideo 数据库所在文件夹"
        onAccepted: page.act("merge", {library_id: page.mergeTargets[Math.max(0, mergeTarget.currentIndex)].library_id,
                                        path: decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))})
    }
    ApiClient { id: api; onErrorChanged: page.error = error }
    function load() { api.getJSON("/api/media/shortvideo/admin/status", function (s) { page.status = s }) }
    function copyEnds() {
        return {from: page.libraries[Math.max(0, copyFrom.currentIndex)].library_id,
                to: page.mergeTargets[Math.max(0, copyTo.currentIndex)].library_id}
    }
    function planCopy() { page.copyPlan = null; api.send("POST", "/api/media/shortvideo/admin/copy-plan", copyEnds(), function (p) { page.copyPlan = p }) }
    function gb(bytes) { return (bytes / 1e9).toFixed(1) + " GB" }
    function act(action, body) { api.send("POST", "/api/media/shortvideo/admin/" + action, body || {}, function () { page.load() }) }
    Component.onCompleted: load()
    Timer { interval: 2000; repeat: true; running: page.busy || page.thumbsRunning; onTriggered: page.load() }

    Label { text: "短视频"; color: "#e6edf3"; font.pixelSize: 20 }
    Label { visible: page.error !== ""; text: page.error; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true }

    Rectangle {
        Layout.fillWidth: true
        implicitHeight: box.implicitHeight + 24
        radius: 6; color: "#161b22"; border.color: "#30363d"
        ColumnLayout {
            id: box
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 12
            spacing: 8
            Toggle {
                text: "生成缩略图"
                checked: !!(page.worker && page.worker.thumbnails_enabled)
                enabled: !!page.worker
                onToggled: page.act("thumbnails", {enabled: checked})
            }
            Label {
                Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
                text: "开启后在后台用 ffmpeg 逐个生成（低优先级，480px 宽），存入各库的 shortvideo/thumbnails.db。关闭后立即停止，只显示已有缩略图。"
            }
            Flow {
                Layout.fillWidth: true
                spacing: 6
                Button { text: "重新扫描"; enabled: !page.busy; onClicked: page.act("scan") }
                Button { text: "图集入库（" + page.importable + "）"; enabled: !page.busy && page.importable > 0; onClicked: page.act("import") }
                Button { text: "清理已入库原目录（" + page.cleanable + "）"; enabled: !page.busy && page.cleanable > 0; onClicked: page.confirmClean = true }
                Button { text: "取消当前任务"; visible: page.busy && page.worker.job !== "thumbnails"; onClicked: page.act("cancel") }
                Button { text: "合并其他设备的数据…"; enabled: !page.busy && page.mergeTargets.length > 0; onClicked: mergeDialog.open() }
            }
            // 跨设备合并：选一个装着另一台设备 shortvideo 数据库（albums.db，可带 index.db、thumbnails.db）的文件夹。
            RowLayout {
                visible: page.mergeTargets.length > 1
                spacing: 6
                Label { text: "合并到"; color: "#c9d1d9"; font.pixelSize: 12 }
                ComboBox { id: mergeTarget; model: page.mergeTargets; textRole: "name"; implicitHeight: 30 }
            }
            Label {
                Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
                text: "合并：视频和没入库的图集目录是文件，直接拷进对应博主目录再「重新扫描」即可；已入库并清理了原目录的图集只在数据库里，用这个按钮把另一台设备的 albums.db（同目录的 index.db、thumbnails.db 一起）并进来。同一作品内容不同时保留本库的。"
            }
            // 增量补充：把来源库的新视频补到目标库已有的博主，只增加不删除。
            RowLayout {
                visible: page.libraries.length > 1 && page.mergeTargets.length > 0
                spacing: 6
                Label { text: "从"; color: "#c9d1d9"; font.pixelSize: 12 }
                ComboBox { id: copyFrom; model: page.libraries; textRole: "name"; implicitHeight: 30; onActivated: page.copyPlan = null }
                Label { text: "补充到"; color: "#c9d1d9"; font.pixelSize: 12 }
                ComboBox { id: copyTo; model: page.mergeTargets; textRole: "name"; implicitHeight: 30; currentIndex: page.mergeTargets.length > 1 ? 1 : 0; onActivated: page.copyPlan = null }
                Button { text: "查看要补充的"; enabled: !page.busy; onClicked: page.planCopy() }
            }
            Label {
                visible: page.libraries.length > 1
                Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
                text: "只处理目标库里已有的博主：目标没有的文件才复制（先写临时文件，写完核对大小再改名），下载记录 downloaded.txt 两边合并；同名但大小不同的文件不覆盖，只列出来。不删除目标库的任何文件，中断后再点一次接着补。"
            }
            Rectangle {
                visible: !!page.copyPlan
                Layout.fillWidth: true
                implicitHeight: copyBox.implicitHeight + 16
                radius: 4; color: "transparent"; border.color: "#58a6ff"
                ColumnLayout {
                    id: copyBox
                    anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 8
                    Label {
                        Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#c9d1d9"; font.pixelSize: 12
                        text: !page.copyPlan ? "" : page.copyPlan.files === 0 && !(page.copyPlan.archives || []).length
                            ? "没有要补充的。"
                            : (page.copyPlan.creators || []).length + " 个博主：复制 " + page.copyPlan.files + " 个文件（" + page.gb(page.copyPlan.bytes) + "），合并下载记录 " + (page.copyPlan.archives || []).length + " 个"
                              + "\n" + (page.copyPlan.creators || []).slice(0, 8).join("、") + ((page.copyPlan.creators || []).length > 8 ? " 等" : "")
                    }
                    Label {
                        visible: !!page.copyPlan && (page.copyPlan.conflicts || []).length > 0
                        Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: "#d29922"; font.pixelSize: 12
                        text: page.copyPlan ? (page.copyPlan.conflicts || []).length + " 个同名文件大小不同，不会覆盖：" + (page.copyPlan.conflicts || []).slice(0, 5).join("、") : ""
                    }
                    RowLayout {
                        PrimaryButton {
                            text: "开始补充"
                            enabled: !page.busy && !!page.copyPlan && (page.copyPlan.files > 0 || (page.copyPlan.archives || []).length > 0)
                            onClicked: { page.act("copy", page.copyEnds()); page.copyPlan = null }
                        }
                        Button { text: "取消"; onClicked: page.copyPlan = null }
                    }
                }
            }
            Rectangle {
                visible: page.confirmClean
                Layout.fillWidth: true
                implicitHeight: cleanBox.implicitHeight + 16
                radius: 4; color: "transparent"; border.color: "#58a6ff"
                ColumnLayout {
                    id: cleanBox
                    anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 8
                    Label {
                        Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#c9d1d9"; font.pixelSize: 12
                        text: "逐个核对原目录与 albums.db 中的入库内容（文件名、数量、SHA-256），完全一致的目录移入系统回收站；有任何差异的保留不动。背景音乐 mp3 不受影响。"
                    }
                    RowLayout {
                        PrimaryButton { text: "校验并移入回收站"; onClicked: { page.confirmClean = false; page.act("clean") } }
                        Button { text: "取消"; onClicked: page.confirmClean = false }
                    }
                }
            }
            Label {
                color: "#c9d1d9"; font.pixelSize: 12
                text: !page.worker ? "正在读取…"
                    : page.worker.job && page.worker.job !== "thumbnails" ? "正在" + (page.jobNames[page.worker.job] || page.worker.job) + "：" + page.worker.done + "/" + page.worker.total
                    : page.worker.last_job ? "上次" + (page.jobNames[page.worker.last_job] || page.worker.last_job) + "：" + (page.worker.last_note || "完成")
                    : "空闲"
            }
            Label {
                visible: !!(page.worker && page.worker.last_error)
                text: page.worker ? page.worker.last_error : ""
                color: "#f85149"; font.pixelSize: 12; wrapMode: Text.WrapAnywhere; Layout.fillWidth: true
            }
        }
    }

    Flow {
        Layout.fillWidth: true
        spacing: 10
        Repeater {
            model: page.libraries
            delegate: Rectangle {
                required property var modelData
                width: 340; height: libCol.implicitHeight + 20
                radius: 6; color: "#161b22"; border.color: "#30363d"
                Column {
                    id: libCol
                    anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 10
                    spacing: 3
                    Label { text: modelData.name; color: "#e6edf3"; font.pixelSize: 14; font.bold: true; bottomPadding: 4 }
                    Label { visible: !!modelData.error; text: modelData.error || ""; color: "#8b949e"; font.pixelSize: 12 }
                    Repeater {
                        model: modelData.error ? [] : [
                            "视频 " + modelData.stats.videos + "，图集 " + modelData.stats.albums,
                            "编码/尺寸已探测 " + modelData.stats.videos_probed + " / " + modelData.stats.videos,
                            "缩略图 " + modelData.stats.thumbnails + " / " + (modelData.stats.videos + modelData.stats.albums) + (modelData.stats.thumbnail_failures ? "（" + modelData.stats.thumbnail_failures + " 个无法解码）" : ""),
                            "图集已入库 " + modelData.stats.albums_imported + "，原目录仍在磁盘 " + modelData.stats.albums_on_disk
                        ]
                        delegate: Label { required property string modelData; text: modelData; color: "#adbac7"; font.pixelSize: 12 }
                    }
                }
            }
        }
    }
}

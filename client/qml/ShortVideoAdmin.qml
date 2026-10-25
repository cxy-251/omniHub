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
    property var review: null          // {text, warn, start（空表示没有要做的）, action, body}
    property string reviewLoading: ""
    spacing: 10

    readonly property var worker: status ? status.worker : null
    readonly property bool busy: !!(worker && worker.job)
    readonly property var libraries: status && status.libraries ? status.libraries : []
    // 补充方向固定：来源是下载落地的主库（SSD），它不生成缩略图、不入库图集，统计里也不算它
    readonly property var source: libraries.filter(function (l) { return l.copy_source })[0] || null
    readonly property var copyTargets: status && status.copy_targets ? status.copy_targets : []
    readonly property var processed: libraries.filter(function (l) { return !l.copy_source })
    readonly property bool thumbsRunning: !!(worker && worker.thumbnails_enabled && processed.some(function (l) { return l.stats && l.stats.thumbnails + l.stats.thumbnail_failures < l.stats.videos + l.stats.albums }))
    readonly property int cleanable: processed.reduce(function (n, l) { return n + (l.stats ? l.stats.albums_cleanable : 0) }, 0)
    readonly property int importable: processed.reduce(function (n, l) { return n + (l.stats ? l.stats.albums_on_disk - l.stats.albums_cleanable : 0) }, 0)
    readonly property var jobNames: ({scan: "扫描", import: "图集入库", clean: "清理已入库原目录", thumbnails: "生成缩略图", merge: "合并其他设备的数据", copy: "补充到其他资源库", phone: "同步到手机", "clean-phone": "清理已同步到手机的视频"})

    readonly property var mergeTargets: libraries.filter(function (l) { return !l.error })
    FolderDialog {
        id: mergeDialog
        title: "选择另一台设备的 shortvideo 数据库所在文件夹"
        onAccepted: page.act("merge", {library_id: page.mergeTargets[Math.max(0, mergeTarget.currentIndex)].library_id,
                                        path: decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))})
    }
    ApiClient { id: api; onErrorChanged: page.error = error }
    function load() { api.getJSON("/api/media/shortvideo/admin/status", function (s) { page.status = s }) }
    function creatorsText(list) { return list.length ? "\n" + list.slice(0, 8).join("、") + (list.length > 8 ? " 等" : "") : "" }
    function conflictsText(list) { return list.length ? list.length + " 个同名文件大小不同，不会覆盖：" + list.slice(0, 5).join("、") : "" }
    // plan 请求：失败时把错误（比如手机没连上）显示在预览框里
    function plan(action, body, loading, build) {
        page.review = null; page.reviewLoading = loading
        api.send("POST", "/api/media/shortvideo/admin/" + action, body, function (p) { page.reviewLoading = ""; page.review = build(p) },
                 function (xhr) { page.reviewLoading = ""; page.review = {text: xhr.responseText || ("请求失败（" + xhr.status + "）"), warn: "", start: "", action: "", body: {}} })
    }
    function planCopy(target) {
        plan("copy-plan", {to: target.library_id}, "正在比对…", function (p) {
            var creators = p.creators || [], todo = p.files > 0
            return {text: page.source.name + " → " + target.name + "：" + (todo
                        ? creators.length + " 个博主，复制 " + p.files + " 个文件（" + page.gb(p.bytes) + "）" + page.creatorsText(creators)
                        : "没有要补充的。"),
                    warn: page.conflictsText(p.conflicts || []), start: todo ? "开始补充" : "", action: "copy", body: {to: target.library_id}}
        })
    }
    function planPhone() {
        plan("phone-plan", {}, "正在连接手机、读取手机上的文件清单（文件多时要十几秒）…", function (p) {
            var creators = p.creators || [], todo = p.files > 0
            return {text: page.source.name + " → 手机：" + (todo
                        ? creators.length + " 个博主，复制 " + p.files + " 个文件（" + page.gb(p.bytes) + "）" + page.creatorsText(creators)
                          + "\n传输时别拔线；断了重新点一次，从断点接着传。"
                        : "手机上已经都有了。"),
                    warn: [p.phone_free && p.bytes > p.phone_free - 10 * 1073741824
                              ? "手机只剩 " + page.gb(p.phone_free) + "，放不下这次的 " + page.gb(p.bytes) + "：会传到手机剩 10 GB 左右时停下，没传的下次接着传。"
                              : p.phone_free ? "手机剩余 " + page.gb(p.phone_free) + "。" : "",
                           page.conflictsText(p.conflicts || [])].filter(function (x) { return x !== "" }).join("\n"),
                    start: todo ? "开始同步到手机" : "", action: "phone", body: {}}
        })
    }
    function planClean() {
        plan("clean-plan", {}, "正在连接手机、核对哪些视频手机上已经有了…", function (p) {
            var kept = (p.not_on_phone ? "手机上还没有（或大小不同）的 " + p.not_on_phone + " 个" : "")
                + (p.not_on_keep ? (p.not_on_phone ? "，" : "") + "SD 卡已有的博主那边还没补上的 " + p.not_on_keep + " 个" : "")
            return {text: p.files > 0
                        ? "移进回收站：" + p.creators + " 个博主的 " + p.files + " 个文件（" + page.gb(p.bytes) + "），都已在手机上（同名同大小）。downloaded.txt 全部保留。"
                        : "没有可以清理的。",
                    warn: kept ? "保留不动：" + kept + "。" : "", start: p.files > 0 ? "移进回收站" : "", action: "clean-phone", body: {}}
        })
    }
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
            // 短视频移动：方向固定从 SSD 出去。每个按钮先读出要做的事放进 review，确认后才执行。
            Label { visible: !!page.source; text: "短视频移动"; color: "#e6edf3"; font.pixelSize: 14; topPadding: 6 }
            Flow {
                visible: !!page.source
                Layout.fillWidth: true
                spacing: 6
                Repeater {
                    model: page.copyTargets
                    delegate: Button {
                        required property var modelData
                        text: page.source.name + " → " + modelData.name
                        enabled: !page.busy && !page.reviewLoading
                        onClicked: page.planCopy(modelData)
                    }
                }
                Button { text: page.source ? page.source.name + " → 手机（数据线）" : ""; enabled: !page.busy && !page.reviewLoading; onClicked: page.planPhone() }
                Button { text: "清理已同步到手机的视频"; enabled: !page.busy && !page.reviewLoading; onClicked: page.planClean() }
            }
            Label {
                visible: !!page.source
                Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
                text: page.source ? "新下载的视频在「" + page.source.name + "」，这里只做中转：不生成缩略图、不入库图集。每个按钮先列出要做的事，确认后才执行；中断后再点一次接着做。\n"
                    + "只处理浏览器插件下载的平台（抖音、快手、TikTok、小红书），MV 等不动。\n"
                    + "• 补到 SD 卡：只补 SD 卡已有的博主。• 补到手机：所有博主都补（手机放全量）；数据线断了会等手机重新连上接着传，重新点一次也从断点接着传。目标没有的文件才复制，写完核对大小再放进去；同名但大小不同的不覆盖，只列出来；不删除目标的任何文件。\n"
                    + "• downloaded.txt 只留在「" + page.source.name + "」（浏览器插件按它判断下过没有），不复制到 SD 卡和手机。\n"
                    + "• 清理：手机上有同名同大小的文件、且 SD 卡已有的博主那边也补上了，才移进回收站；downloaded.txt 全部保留。" : ""
            }
            Label { visible: page.reviewLoading !== ""; text: page.reviewLoading; color: "#c9d1d9"; font.pixelSize: 12 }
            Rectangle {
                visible: !!page.review
                Layout.fillWidth: true
                implicitHeight: reviewBox.implicitHeight + 16
                radius: 4; color: "transparent"; border.color: "#58a6ff"
                ColumnLayout {
                    id: reviewBox
                    anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 8
                    Label {
                        Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#c9d1d9"; font.pixelSize: 12
                        text: page.review ? page.review.text : ""
                    }
                    Label {
                        visible: !!page.review && page.review.warn !== ""
                        Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: "#d29922"; font.pixelSize: 12
                        text: page.review ? page.review.warn : ""
                    }
                    RowLayout {
                        PrimaryButton {
                            visible: !!page.review && page.review.start !== ""
                            text: page.review ? page.review.start : ""
                            enabled: !page.busy
                            onClicked: { page.act(page.review.action, page.review.body); page.review = null }
                        }
                        Button { text: page.review && page.review.start === "" ? "知道了" : "取消"; onClicked: page.review = null }
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
                    : page.worker.job && page.worker.job !== "thumbnails" ? "正在" + (page.jobNames[page.worker.job] || page.worker.job) + "：" + page.worker.done + "/" + page.worker.total + (page.worker.detail ? "\n" + page.worker.detail : "")
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
                        model: modelData.error ? [] : modelData.copy_source ? [
                            "视频 " + modelData.stats.videos + "，图集 " + modelData.stats.albums,
                            "新下载的中转区：不生成缩略图、不入库图集"
                        ] : [
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

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtCore

// MEGA 网盘：账号与配额、粘贴链接下载、云盘与回收站浏览、传输队列。
// 页面打开时每 2 秒刷新传输；离开页面后服务端在没有传输时 5 分钟内关闭 MEGAcmd。
Item {
    id: panel
    property var status: ({})
    property string remotePath: "/"
    property bool showTrash: false
    property var entries: []
    property var transfers: []
    property var quota: ({})
    property var destinations: []
    property string message: ""
    property bool messageIsError: false
    property bool busy: false

    Settings { id: prefs; category: "mega"; property string dest: ""; property var customDests: [] }
    ApiClient { id: api; onErrorChanged: if (error) panel.say(error, true) }

    function say(text, isError) { message = text; messageIsError = !!isError }
    function refreshStatus() {
        busy = true
        api.getJSON("/api/mega/status", function (s) {
            panel.busy = false
            panel.status = s
            if (s.logged_in) panel.loadEntries()
        })
        api.getJSON("/api/mega/destinations", function (list) {
            panel.destinations = list.concat(prefs.customDests.map(function (p) { return {label: p, path: p} }))
            if (!prefs.dest && list.length) prefs.dest = list[0].path
        })
    }
    function loadEntries() {
        var path = showTrash ? "/api/mega/trash" : "/api/mega/files?path=" + encodeURIComponent(remotePath)
        api.getJSON(path, function (r) { panel.entries = r.items })
    }
    function refreshTransfers() {
        api.getJSON("/api/mega/transfers", function (r) {
            panel.transfers = r.transfers
            panel.quota = r
        })
    }
    function post(path, body, okText, after) {
        api.send("POST", path, body, function () { if (okText) panel.say(okText, false); if (after) after() })
    }
    function download(source) {
        if (!prefs.dest) { say("先选择保存位置", true); return }
        say("正在加入下载队列…", false)
        post("/api/mega/download", {source: source, dest: prefs.dest}, "已加入下载队列，保存到 " + prefs.dest, refreshTransfers)
    }
    function enter(path) { remotePath = path; showTrash = false; loadEntries() }
    function formatSize(b) {
        if (b < 1024) return b + " B"
        var units = ["KB", "MB", "GB", "TB"], v = b / 1024, i = 0
        while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
        return v.toFixed(v < 10 ? 1 : 0) + " " + units[i]
    }

    Component.onCompleted: { refreshStatus(); refreshTransfers() }
    Timer { interval: 2000; repeat: true; running: !!panel.status.logged_in; onTriggered: panel.refreshTransfers() }

    FolderDialog {
        id: folderDialog
        title: "选择保存位置"
        onAccepted: {
            var p = decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))
            if (prefs.customDests.indexOf(p) < 0) prefs.customDests = prefs.customDests.concat([p]).slice(-5)
            prefs.dest = p
            panel.refreshStatus()
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 10
        spacing: 8

        // 账号行
        RowLayout {
            spacing: 8
            Label { text: "MEGA"; color: "#e6edf3"; font.pixelSize: 17; font.bold: true }
            Label {
                color: "#8b949e"; font.pixelSize: 12
                text: panel.busy ? "正在连接 MEGAcmd（首次约几秒）…"
                    : panel.status.available === false ? (panel.status.error || "没有找到 MEGAcmd")
                    : panel.status.logged_in ? panel.status.email + " · 已用 " + panel.status.used + " / " + panel.status.total : "未登录"
            }
            Label {
                visible: !!panel.quota.over_quota
                color: "#d29922"; font.pixelSize: 12
                text: "带宽配额已用完，约 " + Math.ceil((panel.quota.quota_remaining_sec || 0) / 60) + " 分钟后自动继续"
            }
            Item { Layout.fillWidth: true }
            Chip { text: "刷新"; visible: !!panel.status.logged_in; onClicked: panel.post("/api/mega/reload", {}, "已同步云端变化", panel.loadEntries) }
            Chip { text: "退出登录"; visible: !!panel.status.logged_in; onClicked: panel.post("/api/mega/logout", {}, "已退出登录", panel.refreshStatus) }
        }

        // 登录表单
        RowLayout {
            visible: panel.status.available !== false && !panel.busy && !panel.status.logged_in
            spacing: 6
            TextField { id: email; placeholderText: "邮箱"; Layout.preferredWidth: 220; implicitHeight: 30 }
            TextField { id: password; placeholderText: "密码"; echoMode: TextInput.Password; Layout.preferredWidth: 180; implicitHeight: 30 }
            TextField { id: authCode; placeholderText: "两步验证码（没开可不填）"; Layout.preferredWidth: 180; implicitHeight: 30 }
            PrimaryButton {
                text: "登录"
                enabled: email.text && password.text
                onClicked: { panel.say("正在登录…", false); panel.post("/api/mega/login", {email: email.text, password: password.text, auth_code: authCode.text}, "登录成功", function () { password.text = ""; panel.refreshStatus() }) }
            }
        }

        // 下载行
        RowLayout {
            visible: !!panel.status.logged_in
            spacing: 6
            TextField {
                id: link
                Layout.fillWidth: true
                implicitHeight: 30
                placeholderText: "粘贴 mega.nz 链接（文件或文件夹）"
                onAccepted: if (text.trim()) { panel.download(text.trim()); text = "" }
            }
            Label { text: "保存到"; color: "#8b949e"; font.pixelSize: 12 }
            ComboBox {
                Layout.preferredWidth: 260
                implicitHeight: 30
                font.pixelSize: 12
                textRole: "label"; valueRole: "path"
                model: panel.destinations
                currentIndex: Math.max(0, indexOfValue(prefs.dest))
                onActivated: prefs.dest = currentValue
                ToolTip.visible: hovered; ToolTip.text: prefs.dest
            }
            Chip { text: "选择目录…"; onClicked: folderDialog.open() }
            PrimaryButton { text: "下载"; enabled: link.text.trim().length > 0; onClicked: { panel.download(link.text.trim()); link.text = "" } }
        }
        Label {
            visible: panel.message !== ""
            text: panel.message; wrapMode: Text.Wrap; Layout.fillWidth: true
            color: panel.messageIsError ? "#f85149" : "#3fb950"; font.pixelSize: 12
        }

        // 云盘浏览 + 传输队列
        RowLayout {
            visible: !!panel.status.logged_in
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 10

            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: 4
                RowLayout {
                    spacing: 4
                    Chip { text: "云盘"; selected: !panel.showTrash; onClicked: panel.enter("/") }
                    Chip { text: "回收站"; selected: panel.showTrash; onClicked: { panel.showTrash = true; panel.loadEntries() } }
                    Chip { text: "上一级"; visible: !panel.showTrash && panel.remotePath !== "/"; onClicked: panel.enter(panel.remotePath.slice(0, panel.remotePath.lastIndexOf("/")) || "/") }
                    Label { text: panel.showTrash ? "回收站里的文件可以恢复到云盘根目录" : panel.remotePath; color: "#8b949e"; font.pixelSize: 12; elide: Text.ElideMiddle; Layout.fillWidth: true }
                }
                ListView {
                    id: files
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    model: panel.entries
                    WheelScroll { }
                    ScrollBar.vertical: ScrollBar {}
                    Label { anchors.centerIn: parent; visible: files.count === 0; text: "空"; color: "#6e7681" }
                    delegate: ItemDelegate {
                        id: row
                        required property var modelData
                        width: files.width
                        height: 36
                        padding: 4
                        contentItem: RowLayout {
                            spacing: 8
                            Image { source: "qrc:/icons/" + (row.modelData.is_dir ? "folder" : "list") + ".svg"; sourceSize: Qt.size(18, 18) }
                            Label { Layout.fillWidth: true; elide: Text.ElideRight; text: row.modelData.name; color: "#e6edf3"; font.pixelSize: 13 }
                            Label { text: row.modelData.is_dir ? "" : panel.formatSize(row.modelData.size); color: "#8b949e"; font.pixelSize: 11 }
                            Label { text: row.modelData.mtime.replace("T", " ").slice(0, 16); color: "#6e7681"; font.pixelSize: 11 }
                            Chip { text: "下载"; visible: !panel.showTrash; onClicked: panel.download(row.modelData.path + (row.modelData.is_dir ? "/" : "")) }
                            Chip {
                                id: del
                                visible: !panel.showTrash
                                property bool armed: false
                                text: armed ? "确认移到回收站" : "删除"; selected: armed
                                onClicked: { if (armed) panel.post("/api/mega/trash", {path: row.modelData.path}, "已移到网盘回收站", panel.loadEntries); else { armed = true; disarm.restart() } }
                                Timer { id: disarm; interval: 3000; onTriggered: del.armed = false }
                            }
                            Chip { text: "恢复"; visible: panel.showTrash; onClicked: panel.post("/api/mega/restore", {path: row.modelData.path}, "已恢复到云盘根目录", panel.loadEntries) }
                        }
                        onClicked: if (row.modelData.is_dir && !panel.showTrash) panel.enter(row.modelData.path)
                    }
                }
            }

            // 传输队列
            Rectangle {
                Layout.preferredWidth: 360
                Layout.fillHeight: true
                radius: 6; color: "#161b22"; border.color: "#30363d"
                ColumnLayout {
                    anchors.fill: parent; anchors.margins: 8
                    spacing: 6
                    RowLayout {
                        Label { text: "传输（" + panel.transfers.length + "）"; color: "#e6edf3"; font.pixelSize: 14 }
                        Item { Layout.fillWidth: true }
                        Chip { text: "全部暂停"; visible: panel.transfers.length > 0; onClicked: panel.post("/api/mega/transfer", {tag: "all", action: "pause"}, "", panel.refreshTransfers) }
                        Chip { text: "全部继续"; visible: panel.transfers.length > 0; onClicked: panel.post("/api/mega/transfer", {tag: "all", action: "resume"}, "", panel.refreshTransfers) }
                    }
                    ListView {
                        id: queue
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        clip: true
                        spacing: 6
                        model: panel.transfers
                        Label { anchors.centerIn: parent; visible: queue.count === 0; text: "没有进行中的传输"; color: "#6e7681"; font.pixelSize: 12 }
                        delegate: Column {
                            required property var modelData
                            width: queue.width
                            spacing: 3
                            Label { width: parent.width; elide: Text.ElideMiddle; text: modelData.name; color: "#e6edf3"; font.pixelSize: 12 }
                            Rectangle {
                                width: parent.width; height: 4; radius: 2; color: "#30363d"
                                Rectangle { height: parent.height; radius: 2; color: "#58a6ff"; width: parent.width * Math.min(1, modelData.percent / 100) }
                            }
                            RowLayout {
                                width: parent.width
                                Label { Layout.fillWidth: true; elide: Text.ElideRight; text: modelData.progress + " · " + modelData.state; color: "#8b949e"; font.pixelSize: 11 }
                                Chip {
                                    readonly property bool paused: modelData.state.toUpperCase().indexOf("PAUSE") >= 0
                                    text: paused ? "继续" : "暂停"
                                    onClicked: panel.post("/api/mega/transfer", {tag: modelData.tag, action: paused ? "resume" : "pause"}, "", panel.refreshTransfers)
                                }
                                Chip { text: "取消"; onClicked: panel.post("/api/mega/transfer", {tag: modelData.tag, action: "cancel"}, "已取消", panel.refreshTransfers) }
                            }
                        }
                    }
                }
            }
        }
        Item { Layout.fillHeight: true; visible: !panel.status.logged_in }
    }
}

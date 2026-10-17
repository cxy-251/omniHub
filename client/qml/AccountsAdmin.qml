import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 账号与权限（与网页端管理页同一组接口）：
//   分区等级（相册、短视频……）与子分区等级（如 text/novels/nsfw；未单独设置的沿用分区，nsfw 默认仅管理员）
//   账号：新建、改等级、停用/恢复、重置密码（改等级、改密码会撤销该账号已登录的凭证）
//   受信任的浏览器：查看与撤销（登记要在那台设备的浏览器里做）
ColumnLayout {
    id: page
    spacing: 12
    property var permissions: ({})
    property var subsections: []
    property var users: []
    property bool moreUsers: false
    property int offset: 0
    property var devices: []
    property string notice: ""
    property bool noticeError: false
    property int resetFor: 0
    // 输入新密码时可以显示明文；已有密码只存了哈希，无法显示，忘了就在这里重置。
    property bool showPasswords: false

    readonly property var levels: [[0, "游客"], [1, "用户"], [2, "VIP1"], [3, "VIP2"], [4, "VIP3"], [99, "管理员"]]
    readonly property var sections: [["gallery", "相册"], ["shortvideo", "短视频"], ["text", "文本"], ["audio", "音声"], ["manga", "漫画"], ["games", "游戏"], ["tools", "工具"], ["tasks", "任务"], ["admin", "管理"]]
    function levelIndex(v) { return Math.max(0, levels.findIndex(function (l) { return l[0] === v })) }

    ApiClient { id: api; onErrorChanged: if (error) { page.notice = error; page.noticeError = true } }
    function say(text) { notice = text; noticeError = false }
    function load() {
        api.getJSON("/api/admin/permissions", function (p) { page.permissions = p })
        api.getJSON("/api/admin/subsections", function (list) { page.subsections = list })
        api.getJSON("/api/admin/devices", function (d) { page.devices = d.devices || [] })
        loadUsers()
    }
    function loadUsers() {
        api.getJSON("/api/admin/users?limit=32&offset=" + offset, function (rows, xhr) {
            page.users = rows
            page.moreUsers = xhr.getResponseHeader("X-Has-More") === "true"
        })
    }
    function post(path, body, okText) { api.send("POST", path, body, function () { page.say(okText); page.load() }) }
    function setLevel(id, level) { post("/api/admin/permissions", {section_id: id, required_level: level}, "已保存") }
    Component.onCompleted: load()

    Label { text: "账号与权限"; color: "#e6edf3"; font.pixelSize: 20 }
    Label { visible: page.notice !== ""; text: page.notice; color: page.noticeError ? "#f85149" : "#3fb950"; wrapMode: Text.Wrap; Layout.fillWidth: true }

    // 分区等级
    Label { text: "分区最低等级"; color: "#c9d1d9"; font.pixelSize: 15; font.bold: true }
    Flow {
        Layout.fillWidth: true
        spacing: 10
        Repeater {
            model: page.sections
            delegate: RowLayout {
                required property var modelData
                spacing: 6
                Label { text: modelData[1]; color: "#adbac7"; Layout.preferredWidth: 48 }
                ComboBox {
                    implicitHeight: 30; Layout.preferredWidth: 100; font.pixelSize: 13
                    model: page.levels.map(function (l) { return l[1] })
                    currentIndex: page.levelIndex(page.permissions[modelData[0]] || 0)
                    onActivated: function (i) { page.setLevel(modelData[0], page.levels[i][0]) }
                }
            }
        }
    }

    // 子分区等级
    Label { text: "子分区等级（一级目录）"; color: "#c9d1d9"; font.pixelSize: 15; font.bold: true }
    Label { text: "未单独设置的沿用所属分区；名为 nsfw 的目录默认仅管理员可见。"; color: "#8b949e"; font.pixelSize: 12 }
    Flow {
        Layout.fillWidth: true
        spacing: 10
        Repeater {
            model: page.subsections
            delegate: RowLayout {
                required property var modelData
                spacing: 6
                Label { text: modelData.id + (modelData.explicit ? "" : "（默认）"); color: modelData.explicit ? "#e6edf3" : "#8b949e"; font.pixelSize: 12; Layout.preferredWidth: 190; elide: Text.ElideMiddle }
                ComboBox {
                    implicitHeight: 30; Layout.preferredWidth: 100; font.pixelSize: 13
                    model: page.levels.map(function (l) { return l[1] })
                    currentIndex: page.levelIndex(modelData.required_level)
                    onActivated: function (i) { page.setLevel(modelData.id, page.levels[i][0]) }
                }
            }
        }
    }

    // 账号
    Label { text: "账号"; color: "#c9d1d9"; font.pixelSize: 15; font.bold: true }
    Toggle { text: "显示输入的密码"; checked: page.showPasswords; onToggled: page.showPasswords = checked }
    Label { text: "已设置的密码只保存了加密摘要，无法查看；忘记了就点该账号的「重置密码」设一个新的。"; color: "#8b949e"; font.pixelSize: 12; wrapMode: Text.Wrap; Layout.fillWidth: true }
    RowLayout {
        spacing: 6
        TextField { id: newName; placeholderText: "用户名"; implicitHeight: 30; Layout.preferredWidth: 140; maximumLength: 64 }
        TextField { id: newPassword; placeholderText: "密码"; echoMode: page.showPasswords ? TextInput.Normal : TextInput.Password; implicitHeight: 30; Layout.preferredWidth: 140; maximumLength: 72 }
        ComboBox { id: newLevel; implicitHeight: 30; Layout.preferredWidth: 100; font.pixelSize: 13; model: page.levels.map(function (l) { return l[1] }); currentIndex: 1 }
        Button {
            text: "新建账号"
            enabled: newName.text.trim() !== "" && newPassword.text !== ""
            onClicked: {
                var level = page.levels[newLevel.currentIndex][0]
                page.post("/api/admin/users/create", {username: newName.text.trim(), password: newPassword.text, role: level === 99 ? "admin" : "user", level: level}, "已新建 " + newName.text.trim() + (page.showPasswords ? "，密码：" + newPassword.text : ""))
                newName.text = ""; newPassword.text = ""
            }
        }
    }
    Repeater {
        model: page.users
        delegate: RowLayout {
            required property var modelData
            spacing: 6
            Label { text: modelData.username; color: modelData.status === "active" ? "#e6edf3" : "#6e7681"; Layout.preferredWidth: 140; elide: Text.ElideRight }
            Label { text: modelData.status === "active" ? "正常" : "已停用"; color: "#8b949e"; font.pixelSize: 12; Layout.preferredWidth: 48 }
            ComboBox {
                implicitHeight: 30; Layout.preferredWidth: 100; font.pixelSize: 13
                enabled: modelData.role !== "admin"
                model: page.levels.map(function (l) { return l[1] })
                currentIndex: page.levelIndex(modelData.level)
                onActivated: function (i) { page.post("/api/admin/users", {user_id: modelData.id, level: page.levels[i][0]}, "已改等级，并撤销 " + modelData.username + " 已登录的凭证") }
            }
            Button {
                text: modelData.status === "active" ? "停用" : "恢复"
                enabled: modelData.role !== "admin"   // 管理员不能在这里停用（避免把自己锁在外面）
                onClicked: page.post("/api/admin/users/status", {user_id: modelData.id, status: modelData.status === "active" ? "disabled" : "active"}, "已更新")
            }
            Button { text: "重置密码"; onClicked: page.resetFor = modelData.id }
            TextField { id: resetField; visible: page.resetFor === modelData.id; placeholderText: "新密码"; echoMode: page.showPasswords ? TextInput.Normal : TextInput.Password; implicitHeight: 30; Layout.preferredWidth: 140; maximumLength: 72 }
            Button {
                visible: page.resetFor === modelData.id
                text: "确认"; enabled: resetField.text !== ""
                onClicked: { page.post("/api/admin/users/password", {user_id: modelData.id, password: resetField.text}, modelData.username + " 的密码已改为" + (page.showPasswords ? "：" + resetField.text : "新密码") + "，并撤销全部凭证"); page.resetFor = 0 }
            }
        }
    }
    RowLayout {
        visible: page.offset > 0 || page.moreUsers
        Button { text: "上一页"; enabled: page.offset > 0; onClicked: { page.offset = Math.max(0, page.offset - 32); page.loadUsers() } }
        Button { text: "下一页"; enabled: page.moreUsers; onClicked: { page.offset += 32; page.loadUsers() } }
    }

    // 受信任的浏览器
    Label { text: "受信任的浏览器"; color: "#c9d1d9"; font.pixelSize: 15; font.bold: true }
    Label { visible: page.devices.length === 0; text: "没有。手机或电脑的浏览器登录后，可以在网页端管理页里「信任当前浏览器」。"; color: "#8b949e"; font.pixelSize: 12 }
    Repeater {
        model: page.devices
        delegate: RowLayout {
            required property var modelData
            spacing: 8
            Label { text: modelData.device_name + " · " + modelData.username + " · " + modelData.ip; color: "#e6edf3"; font.pixelSize: 13 }
            Label { text: "到期 " + new Date(modelData.expires_at).toLocaleString(); color: "#8b949e"; font.pixelSize: 12 }
            Button { text: "撤销信任"; onClicked: api.send("DELETE", "/api/admin/devices?id=" + encodeURIComponent(modelData.id), null, function () { page.say("已撤销"); page.load() }) }
        }
    }
}

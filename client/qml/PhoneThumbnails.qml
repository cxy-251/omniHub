import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 主机维护已授权 USB/无线 ADB 手机；密码只用于本次手机服务登录。
ColumnLayout {
    id: panel
    property var worker: null
    property bool busy: false
    property var devices: []
    property string error: ""
    readonly property var review: worker ? worker.phone_review : null
    signal action(string name, var body)
    spacing: 6
    ApiClient { id: api; onErrorChanged: panel.error = error }
    function refreshDevices() { api.getJSON("/api/media/shortvideo/admin/phone-devices", function (list) { panel.devices = list; panel.error = list.length ? "" : "没有已授权设备：先连接无线 ADB 或 USB 调试" }) }
    Label { text: "手机旧封面维护（ADB）"; color: "#e6edf3"; font.pixelSize: 14; topPadding: 8 }
    RowLayout {
        ComboBox { id: device; Layout.fillWidth: true; model: panel.devices; textRole: "label"; valueRole: "id" }
        Button { text: "读取已连接设备"; enabled: !panel.busy; onClicked: panel.refreshDevices() }
        Button { text: "读取回收记录"; enabled: !panel.busy && device.count > 0; onClicked: panel.action("phone-thumbs-history", {device: device.currentValue}) }
    }
    RowLayout {
        TextField { id: user; placeholderText: "手机 omniHub 管理员"; text: "admin"; Layout.fillWidth: true }
        TextField { id: password; placeholderText: "手机 omniHub 密码（不保存）"; echoMode: TextInput.Password; Layout.fillWidth: true }
        SpinBox { id: port; from: 1; to: 65535; value: 8998; editable: true }
        Button { text: "核对旧封面"; enabled: !panel.busy && device.count > 0 && !!password.text; onClicked: { panel.action("phone-thumbs-plan", {device: device.currentValue, user: user.text, password: password.text, port: port.value}); password.text = "" } }
    }
    Label { Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12; text: "仅检查 shortvideo/<平台>/<博主>/.thumbs；逐张确认手机服务有数据库封面，每个目录再读取一张实际图片。缺封面或含其他文件的目录保留。USB／无线均可，批量传视频仍走数据线。" }
    Label { visible: panel.error !== ""; text: panel.error; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true }
    ColumnLayout {
        visible: !!panel.review
        Layout.fillWidth: true
        Label { Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: "#c9d1d9"; font.pixelSize: 12; text: panel.review ? "核对通过 " + panel.review.directories + " 个目录、" + panel.review.files + " 张（" + (panel.review.bytes/1e9).toFixed(2) + " GB），保留 " + panel.review.skipped + " 个目录。\n回收位置：" + panel.review.trash + "\n移入回收目录不会释放手机空间；可在此恢复。" : "" }
        RowLayout {
            Button { text: "回收已核对的旧封面"; enabled: !panel.busy && !!panel.review && panel.review.can_clean && panel.review.remaining > 0; onClicked: panel.action("phone-thumbs-clean", {token: panel.review.token}) }
            Button { text: "恢复本次旧封面"; enabled: !panel.busy && !!panel.review && panel.review.moved; onClicked: panel.action("phone-thumbs-restore", {token: panel.review.token}) }
        }
    }
}

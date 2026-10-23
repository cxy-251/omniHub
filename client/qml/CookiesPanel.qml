import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs

// 账号与 Cookie：下载工具用各平台的登录状态。你在自己的 Chrome 里正常登录，这里按平台把该平台的 Cookie 读出来存好。
// 只在点「从 Chrome 读取」时读，只读该平台的域名；页面不显示 Cookie 的值。
ColumnLayout {
    id: panel
    spacing: 10
    ApiClient { id: api }
    property var info: ({platforms: [], profiles_found: []})
    property string busy: ""            // 正在读取的平台
    property string message: ""
    function load() { api.getJSON("/api/cookies", function (r) { panel.info = r }) }
    function send(action, body) {
        if (action === "read") { busy = body.platform; message = "" }
        api.send("POST", "/api/cookies/" + action, body, function (r) { panel.info = r; panel.busy = "" })
    }
    Connections { target: api; function onErrorChanged() { if (api.error) { panel.busy = ""; panel.message = api.error } } }
    Component.onCompleted: load()

    FolderDialog {
        id: profileDialog
        title: "选择 Chrome 的用户数据目录（里面有 Default 文件夹）"
        onAccepted: panel.send("profile", {path: decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))})
    }

    Label {
        Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
        text: "先在你平时用的 Chrome 里登录各平台，再点「从 Chrome 读取」。Cookie 只存在本机（仅你的账号可读），下载时按平台交给 yt-dlp、gallery-dl。过期了再读一次即可。"
    }
    RowLayout {
        spacing: 6
        Label { text: "Chrome 目录"; color: "#c9d1d9"; font.pixelSize: 12 }
        Label {
            Layout.fillWidth: true; elide: Text.ElideMiddle; font.family: "monospace"; font.pixelSize: 12
            text: panel.info.profile || "没有找到，请指定"
            color: panel.info.profile ? "#e6edf3" : "#f85149"
        }
        Chip { text: "指定…"; onClicked: profileDialog.open() }
        Chip { text: "自动查找"; visible: panel.info.profiles_found.indexOf(panel.info.profile) < 0 && panel.info.profiles_found.length > 0; onClicked: panel.send("profile", {path: ""}) }
    }
    Label { visible: panel.message !== ""; text: panel.message; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true; font.pixelSize: 12 }

    Repeater {
        model: panel.info.platforms
        delegate: Rectangle {
            required property var modelData
            readonly property var saved: modelData.saved || null
            readonly property bool expired: !!(saved && saved.expires && saved.expires * 1000 < Date.now())
            Layout.fillWidth: true
            implicitHeight: row.implicitHeight + 16
            radius: 6; color: "#161b22"; border.color: "#30363d"
            RowLayout {
                id: row
                anchors.left: parent.left; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter; anchors.margins: 10
                spacing: 8
                Label { text: modelData.label; color: "#e6edf3"; font.pixelSize: 14; Layout.preferredWidth: 100 }
                Label {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12
                    color: !saved ? "#8b949e" : expired || !saved.logged_in ? "#e3b341" : "#3fb950"
                    text: !saved ? "未读取（" + modelData.domains.join("、") + "）"
                        : (saved.logged_in ? (expired ? "登录已过期" : "已登录") : "没有登录 Cookie（在 Chrome 里登录后再读）")
                          + " · " + saved.count + " 个 Cookie · 读取于 " + new Date(saved.saved_at).toLocaleString(Qt.locale(), "MM-dd hh:mm")
                          + (saved.expires && !expired ? " · 到期 " + new Date(saved.expires * 1000).toLocaleString(Qt.locale(), "yyyy-MM-dd") : "")
                }
                BusyIndicator { running: panel.busy === modelData.id; visible: running; implicitWidth: 24; implicitHeight: 24 }
                Chip { text: "从 Chrome 读取"; enabled: !panel.busy && !!panel.info.profile; onClicked: panel.send("read", {platform: modelData.id}) }
                Chip { text: "删除"; visible: !!saved && !panel.busy; onClicked: panel.send("clear", {platform: modelData.id}) }
            }
        }
    }
    // Last.fm API key：音乐按重播率挑要用（总播放 ÷ 听众）
    Rectangle {
        Layout.fillWidth: true
        implicitHeight: lastfmRow.implicitHeight + 16
        radius: 6; color: "#161b22"; border.color: "#30363d"
        RowLayout {
            id: lastfmRow
            anchors.left: parent.left; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter; anchors.margins: 10
            spacing: 8
            Label { text: "Last.fm"; color: "#e6edf3"; font.pixelSize: 14; Layout.preferredWidth: 100 }
            Label {
                Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12
                color: panel.info.has_lastfm ? "#3fb950" : "#8b949e"
                text: panel.info.has_lastfm ? "已填 API key：音乐可以按重播率挑" : "填自己的 API key 后，音乐可以按重播率（听过的人平均听几遍）挑。免费申请：last.fm/api/account/create"
                TapHandler { enabled: !panel.info.has_lastfm; onTapped: Qt.openUrlExternally("https://www.last.fm/api/account/create") }
            }
            TextField { id: lastfmKey; implicitHeight: 30; Layout.preferredWidth: 220; placeholderText: "API key"; echoMode: TextInput.Password; font.pixelSize: 12 }
            Chip { text: "保存"; enabled: lastfmKey.text.trim() !== ""; onClicked: { panel.send("lastfm", {path: lastfmKey.text.trim()}); lastfmKey.text = "" } }
            Chip { text: "删除"; visible: !!panel.info.has_lastfm; onClicked: panel.send("lastfm", {path: ""}) }
        }
    }
    Label {
        Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#6e7681"; font.pixelSize: 11
        text: "「已登录」是按各平台常见的会话 Cookie 判断的，不保证一定有效；下载时提示要登录就在 Chrome 里重新登录再读。第一次读取要准备下载环境，可能要等一两分钟。"
    }
}

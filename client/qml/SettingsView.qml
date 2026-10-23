import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 设置页：与网页端管理页对应的本机入口。左栏分类，右栏内容（各自按需加载，切走即销毁）。
//   资源库：增删资源库、主库、分类开关
//   短视频：缩略图开关、扫描、图集入库与清理
//   账号与权限：分区/子分区等级、账号、受信任的浏览器
//   网络：局域网访问开关
Item {
    id: view
    Settings { id: prefs; category: "settings"; property string tab: "libraries" }
    readonly property var tabs: [["libraries", "资源库"], ["shortvideo", "短视频"], ["gallery", "相册"], ["accounts", "账号与权限"], ["network", "网络"]]

    RowLayout {
        anchors.fill: parent
        spacing: 0
        Rectangle {
            Layout.preferredWidth: 150
            Layout.fillHeight: true
            color: "#0d1117"
            Rectangle { anchors.right: parent.right; width: 1; height: parent.height; color: "#30363d" }
            Column {
                anchors.fill: parent; anchors.margins: 8
                spacing: 4
                Repeater {
                    model: view.tabs
                    delegate: ItemDelegate {
                        required property var modelData
                        width: parent.width
                        height: 36
                        highlighted: prefs.tab === modelData[0]
                        contentItem: Label { text: modelData[1]; color: prefs.tab === modelData[0] ? "#58a6ff" : "#c9d1d9"; font.pixelSize: 14; verticalAlignment: Text.AlignVCenter }
                        onClicked: { prefs.tab = modelData[0]; if (modelData[0] === "libraries") backendApi.refreshLibraries() }
                    }
                }
            }
        }
        Loader {
            Layout.fillWidth: true
            Layout.fillHeight: true
            sourceComponent: ({libraries: libraries, shortvideo: shortvideo, gallery: galleryAdmin, accounts: accounts, network: network})[prefs.tab] || libraries
        }
    }

    Component { id: libraries; LibraryView { } }
    Component {
        id: shortvideo
        ScrollView {
            contentWidth: availableWidth
            ShortVideoAdmin { width: parent.width - 32; x: 16; y: 16 }
        }
    }
    Component {
        id: galleryAdmin
        ScrollView {
            contentWidth: availableWidth
            GalleryAdmin { width: parent.width - 32; x: 16; y: 16 }
        }
    }
    Component {
        id: accounts
        ScrollView {
            contentWidth: availableWidth
            AccountsAdmin { width: parent.width - 32; x: 16; y: 16 }
        }
    }
    Component {
        id: network
        Item {
            ApiClient { id: lanApi }
            ColumnLayout {
                x: 16; y: 16
                width: parent.width - 32
                spacing: 10
                Label { text: "网络"; color: "#e6edf3"; font.pixelSize: 20 }
                // 局域网访问：关闭后手机、电脑等其它设备连不上，只有本机能用（立即生效，不用重启）。
                Toggle {
                    id: lanSwitch
                    text: "允许局域网访问"
                    property bool loaded: false
                    enabled: loaded
                    property var urls: []
                    Component.onCompleted: lanApi.getJSON("/api/network/lan", function (r) { lanSwitch.checked = r.enabled; lanSwitch.urls = r.urls || []; lanSwitch.loaded = true })
                    onToggled: lanApi.send("POST", "/api/network/lan", {enabled: checked}, function (r) { lanSwitch.checked = r.enabled })
                }
                Label {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12
                    text: lanApi.error || (lanSwitch.checked ? "手机、电脑连同一个 Wi-Fi，在浏览器里打开下面的网址即可使用网页端。" : "只有这台 Deck 能访问，局域网设备会被拒绝。")
                    color: lanApi.error ? "#f85149" : "#8b949e"
                }
                // 局域网网址，一键复制
                TextEdit { id: copier; visible: false }
                Repeater {
                    model: lanSwitch.checked ? lanSwitch.urls : []
                    delegate: RowLayout {
                        required property string modelData
                        spacing: 8
                        Label { text: modelData; color: "#e6edf3"; font.pixelSize: 14; font.family: "monospace" }
                        Button {
                            id: copyButton
                            property bool done: false
                            text: done ? "已复制" : "复制"
                            implicitHeight: 28
                            onClicked: { copier.text = modelData; copier.selectAll(); copier.copy(); done = true; copyTimer.restart() }
                            Timer { id: copyTimer; interval: 1500; onTriggered: copyButton.done = false }
                        }
                    }
                }
                // 广域网访问：托管 cloudflared 命名隧道（~/.cloudflared/config.yml），外网经域名访问网页端。
                Toggle {
                    id: wanSwitch
                    Layout.topMargin: 12
                    text: "允许广域网访问"
                    property bool loaded: false
                    property var status: ({})
                    enabled: loaded
                    function apply(r) { status = r; checked = r.enabled; loaded = true }
                    Component.onCompleted: lanApi.getJSON("/api/network/wan", apply)
                    onToggled: lanApi.send("POST", "/api/network/wan", {enabled: checked}, apply)
                    // 打开期间每 3 秒刷新连接状态
                    Timer { interval: 3000; repeat: true; running: wanSwitch.checked; onTriggered: lanApi.getJSON("/api/network/wan", wanSwitch.apply) }
                }
                Label {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12
                    readonly property var st: wanSwitch.status
                    text: !wanSwitch.loaded ? "" :
                          !st.binary ? "没有找到 cloudflared，放到 ~/Applications/cloudflared/cloudflared。" :
                          !st.domain ? "没有读到隧道配置里的域名：" + st.config :
                          !wanSwitch.checked ? "关闭中：经隧道来的请求一律拒绝。打开后在外网用 https://" + st.domain + " 访问（需要登录账号）。" :
                          st.connections > 0 ? "已连通（" + st.connections + " 条连接）：https://" + st.domain :
                          st.running ? "正在连接 Cloudflare…" + (st.error ? "（" + st.error + "）" : "") :
                          "未运行" + (st.error ? "：" + st.error : "")
                    color: wanSwitch.checked && st.connections > 0 ? "#3fb950" : (wanSwitch.checked && st.error ? "#f85149" : "#8b949e")
                }
                Label {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
                    text: "下载用的代理与 Cookie 在「下载 → 有声书 → 网络设置」里。"
                }
            }
        }
    }
}

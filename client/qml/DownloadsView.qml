import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtCore

// 下载页：MEGA 与各种脚本任务（有声书、漫画……，由服务端声明，表单自动生成）。
// 右栏是任务卡片：进度、当前步骤、结果，以及「去收听」「打开文件夹」「停止」「日志」。
Item {
    id: view
    signal openAudioAlbum(string album)
    property var kinds: []
    property var presets: []
    property var jobs: []
    property var settings: ({})
    property string message: ""
    property bool messageIsError: false
    readonly property var currentKind: kinds.find(function (k) { return k.id === prefs.tab }) || null

    Settings { id: prefs; category: "downloads"; property string tab: "mega" }
    ApiClient { id: api; onErrorChanged: if (error) view.say(error, true) }

    function say(text, isError) { message = text; messageIsError = !!isError }
    function refresh() {
        api.getJSON("/api/downloads/kinds", function (list) { view.kinds = list })
        api.getJSON("/api/downloads/presets", function (list) { view.presets = list })
        api.getJSON("/api/downloads/settings", function (s) { view.settings = s })
        refreshJobs()
    }
    function refreshJobs() { api.getJSON("/api/downloads/jobs", function (list) { view.jobs = list }) }
    function run(kind, params) {
        say("正在启动…", false)
        api.send("POST", "/api/downloads/run", {kind: kind, params: params}, function () { view.say("已开始，进度见右边", false); view.refreshJobs() })
    }
    Component.onCompleted: refresh()
    Timer { interval: 2000; repeat: true; running: view.jobs.some(function (j) { return j.running }); onTriggered: view.refreshJobs() }

    FileDialog {
        id: cookiesDialog
        title: "选择 cookies.txt（Netscape 格式，可用浏览器扩展导出）"
        onAccepted: {
            var path = decodeURIComponent(selectedFile.toString().replace(/^file:\/\//, ""))
            api.send("POST", "/api/downloads/settings", {ytdlp_proxy: proxyField.text, cookies_file: path}, function (s) { view.settings = s; view.say("Cookie 已导入", false) })
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 10
        spacing: 8

        RowLayout {
            spacing: 4
            Chip { text: "MEGA"; selected: prefs.tab === "mega"; onClicked: prefs.tab = "mega" }
            Repeater {
                model: view.kinds
                delegate: Chip { required property var modelData; text: modelData.title; selected: prefs.tab === modelData.id; onClicked: prefs.tab = modelData.id }
            }
            Item { Layout.fillWidth: true }
            Chip { text: "网络设置"; visible: prefs.tab === "audiobook"; selected: netBox.visible; onClicked: netBox.visible = !netBox.visible }
        }

        // 网络设置（yt-dlp 的代理与 Cookie）
        RowLayout {
            id: netBox
            visible: false
            spacing: 6
            Label { text: "代理"; color: "#c9d1d9"; font.pixelSize: 12 }
            TextField { id: proxyField; implicitHeight: 30; Layout.preferredWidth: 260; placeholderText: "如 http://127.0.0.1:7890（不需要就留空）"; text: view.settings.ytdlp_proxy || "" }
            Chip { text: "保存代理"; onClicked: api.send("POST", "/api/downloads/settings", {ytdlp_proxy: proxyField.text}, function (s) { view.settings = s; view.say("已保存", false) }) }
            Label { text: view.settings.has_cookies ? "Cookie：已导入" : "Cookie：未设置（YouTube 要求登录时需要）"; color: "#8b949e"; font.pixelSize: 12 }
            Chip { text: "导入 cookies.txt…"; onClicked: cookiesDialog.open() }
            Chip { text: "清除 Cookie"; visible: !!view.settings.has_cookies; onClicked: api.send("POST", "/api/downloads/settings", {ytdlp_proxy: proxyField.text, clear_cookies: true}, function (s) { view.settings = s }) }
        }

        Label {
            visible: view.message !== "" && prefs.tab !== "mega"
            text: view.message; color: view.messageIsError ? "#f85149" : "#3fb950"; font.pixelSize: 12
            Layout.fillWidth: true; wrapMode: Text.Wrap
        }

        Loader {
            Layout.fillWidth: true
            Layout.fillHeight: true
            active: prefs.tab === "mega"
            visible: active
            sourceComponent: Component { MegaPanel { } }
        }

        RowLayout {
            visible: prefs.tab !== "mega" && !!view.currentKind
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 12

            ScrollView {
                Layout.fillWidth: true
                Layout.fillHeight: true
                contentWidth: availableWidth
                DownloadForm {
                    width: parent.width
                    kind: view.currentKind || {fields: [], description: ""}
                    presets: view.presets.filter(function (p) { return view.currentKind && p.kind === view.currentKind.id })
                    onRun: function (params) { view.run(kind.id, params) }
                    onRunPreset: function (preset) { view.run(preset.kind, preset.params) }
                    onSavePreset: function (label, params) {
                        api.send("POST", "/api/downloads/presets", {kind: kind.id, label: label, params: params}, function () { view.say("预设已保存", false); view.refresh() })
                    }
                    onDeletePreset: function (preset) {
                        api.send("POST", "/api/downloads/presets/delete", {id: preset.id}, function () { view.refresh() })
                    }
                }
            }

            // 任务卡片
            Rectangle {
                Layout.preferredWidth: 420
                Layout.fillHeight: true
                radius: 6; color: "#161b22"; border.color: "#30363d"
                ColumnLayout {
                    anchors.fill: parent; anchors.margins: 8
                    spacing: 6
                    Label { text: "任务"; color: "#e6edf3"; font.pixelSize: 14 }
                    ListView {
                        id: jobList
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        clip: true
                        spacing: 8
                        model: view.jobs
                        WheelScroll { }
                        ScrollBar.vertical: ScrollBar {}
                        Label { anchors.centerIn: parent; visible: jobList.count === 0; text: "还没有任务"; color: "#6e7681"; font.pixelSize: 12 }
                        delegate: Rectangle {
                            id: card
                            required property var modelData
                            readonly property var job: modelData
                            readonly property bool failed: !job.running && job.exit_code !== 0
                            property bool showLog: false
                            property var log: []
                            width: jobList.width
                            height: cardColumn.implicitHeight + 16
                            radius: 6
                            color: "#0d1117"
                            border.color: job.running ? "#58a6ff" : failed ? "#f85149" : "#3fb950"
                            function loadLog() {
                                api.getJSON("/api/downloads/log?id=" + encodeURIComponent(job.id), function (list) { card.log = list.slice(-200) })
                            }
                            Timer { interval: 2000; repeat: true; running: card.showLog && card.job.running; onTriggered: card.loadLog() }
                            ColumnLayout {
                                id: cardColumn
                                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
                                anchors.margins: 8
                                spacing: 4
                                RowLayout {
                                    Label { Layout.fillWidth: true; elide: Text.ElideMiddle; text: card.job.title; color: "#e6edf3"; font.pixelSize: 13 }
                                    Label {
                                        text: card.job.running ? "进行中" : card.failed ? "失败" : "完成"
                                        color: card.job.running ? "#58a6ff" : card.failed ? "#f85149" : "#3fb950"; font.pixelSize: 12
                                    }
                                }
                                Rectangle {
                                    Layout.fillWidth: true; height: 4; radius: 2; color: "#30363d"
                                    visible: card.job.total > 0
                                    Rectangle { height: parent.height; radius: 2; color: card.failed ? "#f85149" : "#58a6ff"; width: parent.width * Math.min(1, card.job.done / Math.max(1, card.job.total)) }
                                }
                                Label {
                                    Layout.fillWidth: true; elide: Text.ElideRight
                                    text: (card.job.total > 0 ? card.job.done + "/" + card.job.total + " · " : "") + (card.job.step || (card.job.running ? "准备中（首次要安装依赖，约一分钟）…" : ""))
                                    color: "#adbac7"; font.pixelSize: 12
                                    visible: text !== ""
                                }
                                Label {
                                    // 失败时显示最后几行，通常就是原因。
                                    Layout.fillWidth: true; wrapMode: Text.Wrap
                                    visible: card.failed || (card.job.running && card.job.tail.length > 0)
                                    text: card.failed ? card.job.tail.join("\n") : card.job.tail[card.job.tail.length - 1] || ""
                                    color: card.failed ? "#f0883e" : "#6e7681"; font.pixelSize: 11
                                    maximumLineCount: card.failed ? 4 : 1; elide: Text.ElideRight
                                }
                                Label {
                                    Layout.fillWidth: true; elide: Text.ElideMiddle
                                    visible: !!(card.job.result && card.job.result.path)
                                    text: "保存在 " + (card.job.result ? card.job.result.path : "")
                                    color: "#8b949e"; font.pixelSize: 11
                                }
                                Flow {
                                    Layout.fillWidth: true
                                    spacing: 6
                                    Chip { text: "去收听"; visible: !!(card.job.open && card.job.open.type === "audio"); onClicked: view.openAudioAlbum(card.job.open.album) }
                                    Chip { text: "打开文件夹"; visible: !!(card.job.result && card.job.result.path); onClicked: Qt.openUrlExternally("file://" + card.job.result.path) }
                                    Chip { text: "停止"; visible: card.job.running; onClicked: api.send("POST", "/api/downloads/stop", {id: card.job.id}, function () { view.refreshJobs() }) }
                                    Chip { text: card.showLog ? "收起日志" : "日志"; selected: card.showLog; onClicked: { card.showLog = !card.showLog; if (card.showLog) card.loadLog() } }
                                }
                                ScrollView {
                                    visible: card.showLog
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 180
                                    TextArea {
                                        readOnly: true
                                        font.family: "monospace"; font.pixelSize: 11
                                        wrapMode: TextEdit.WrapAnywhere
                                        text: card.log.map(function (e) { return e.message }).join("\n")
                                        color: "#adbac7"
                                        background: Rectangle { color: "#010409" }
                                        onTextChanged: cursorPosition = length
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

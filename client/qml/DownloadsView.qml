import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtCore

// 下载页：MEGA 与各种脚本任务（音乐、YouTube、B 站、18comic 漫画……，由服务端声明，表单自动生成）。
// 任务卡片在右侧的悬浮面板里（可收起）：进度、当前步骤、结果，以及「去收听」「打开文件夹」「停止」「日志」。
Item {
    id: view
    signal openAudioAlbum(string album)
    property var kinds: []
    property var presets: []
    property var jobs: []
    property var settings: ({})
    property string message: ""
    property bool messageIsError: false
    property bool showTasks: false
    readonly property int runningJobs: jobs.filter(function (j) { return j.running }).length
    readonly property var currentKind: kinds.find(function (k) { return k.id === prefs.tab }) || null

    Settings { id: prefs; category: "downloads"; property string tab: "mega"; property string library: "" }
    // 暂存到哪个资源库：各下载方案共用一个选择（选项来自服务端方案里的 library 字段）
    readonly property var libraryField: {
        for (var i = 0; i < kinds.length; i++) {
            var f = kinds[i].scheme && (kinds[i].fields || []).find(function (x) { return x.name === "library" })
            if (f) return f
        }
        return null
    }
    readonly property string stagingLibrary: !libraryField ? "" : (libraryField.options || []).some(function (o) { return o.value === prefs.library }) ? prefs.library : (libraryField.default || "")
    ApiClient { id: api; onErrorChanged: if (error) view.say(error, true) }

    function say(text, isError) { message = text; messageIsError = !!isError }
    function refresh() {
        api.getJSON("/api/downloads/kinds", function (list) {
            view.kinds = list
            if (prefs.tab === "audiobook") prefs.tab = "youtube"   // 有声书已并进 YouTube / B 站的「只要音频」
        })
        api.getJSON("/api/downloads/presets", function (list) { view.presets = list })
        api.getJSON("/api/downloads/settings", function (s) { view.settings = s })
        refreshJobs()
    }
    function refreshJobs() { api.getJSON("/api/downloads/jobs", function (list) { view.jobs = list }) }
    function run(kind, params) {
        say("正在启动…", false)
        api.send("POST", "/api/downloads/run", {kind: kind, params: params}, function () { view.say("已开始，进度在「任务」里", false); view.showTasks = true; view.refreshJobs() })
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
            // 暂存区 · 账号与 Cookie ‖ 各下载方案 ‖ MEGA
            Chip { text: "暂存区"; selected: prefs.tab === "staging"; onClicked: prefs.tab = "staging" }
            Chip { text: "账号与 Cookie"; selected: prefs.tab === "cookies"; onClicked: prefs.tab = "cookies" }
            Divider { Layout.leftMargin: 2; Layout.rightMargin: 2 }
            Repeater {
                model: view.kinds
                delegate: Chip { required property var modelData; text: modelData.title; selected: prefs.tab === modelData.id; onClicked: prefs.tab = modelData.id }
            }
            Divider { Layout.leftMargin: 2; Layout.rightMargin: 2 }
            Chip { text: "MEGA"; selected: prefs.tab === "mega"; onClicked: prefs.tab = "mega" }
            Item { Layout.fillWidth: true }
            Label { visible: stagingBox.visible; text: "暂存到"; color: "#8b949e"; font.pixelSize: 12 }
            ComboBox {
                id: stagingBox
                visible: !!view.libraryField && !!view.currentKind && !!view.currentKind.scheme
                implicitHeight: 28; implicitWidth: 130; font.pixelSize: 12
                textRole: "label"; valueRole: "value"
                model: view.libraryField ? (view.libraryField.options || []) : []
                currentIndex: Math.max(0, model.findIndex(function (o) { return o.value === view.stagingLibrary }))
                onActivated: prefs.library = currentValue
                ToolTip.visible: hovered; ToolTip.text: "下载先放进这个资源库的暂存区；移入时同一块盘上只是改名"
            }
            Chip { text: "网络设置"; visible: !!view.currentKind && (!!view.currentKind.scheme || view.currentKind.id === "btmanga"); selected: netBox.visible; onClicked: netBox.visible = !netBox.visible }
            Chip {
                text: "任务" + (view.runningJobs ? "（" + view.runningJobs + " 个进行中）" : view.jobs.length ? "（" + view.jobs.length + "）" : "")
                selected: view.showTasks
                onClicked: view.showTasks = !view.showTasks
            }
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
            visible: view.message !== "" && prefs.tab !== "mega" && prefs.tab !== "staging" && prefs.tab !== "cookies"
            text: view.message; color: view.messageIsError ? "#f85149" : "#3fb950"; font.pixelSize: 12
            Layout.fillWidth: true; wrapMode: Text.Wrap
        }

        Loader {
            Layout.fillWidth: true
            Layout.fillHeight: true
            active: prefs.tab === "cookies"
            visible: active
            sourceComponent: Component { ScrollView { contentWidth: availableWidth; CookiesPanel { width: parent.width - 8 } } }
        }

        Loader {
            Layout.fillWidth: true
            Layout.fillHeight: true
            active: prefs.tab === "staging"
            visible: active
            sourceComponent: Component { StagingPanel { } }
        }

        Loader {
            Layout.fillWidth: true
            Layout.fillHeight: true
            active: prefs.tab === "mega"
            visible: active
            sourceComponent: Component { MegaPanel { } }
        }

        // 18comic 漫画：搜索、下载列表，有自己的页面（通用表单是给视频图片下载的）
        Loader {
            Layout.fillWidth: true
            Layout.fillHeight: true
            active: prefs.tab === "btmanga" && !!view.currentKind
            visible: active
            sourceComponent: Component { BTMangaPanel { kind: view.currentKind; onRun: function (params) { view.run("btmanga", params) } } }
        }

        Loader {
            Layout.fillWidth: true
            Layout.fillHeight: true
            active: prefs.tab === "manga" && !!view.currentKind
            visible: active
            sourceComponent: Component {
                MangaPanel {
                    kind: view.currentKind
                    onRun: function (params) { view.run("manga", params) }
                }
            }
        }

        RowLayout {
            visible: prefs.tab !== "mega" && prefs.tab !== "staging" && prefs.tab !== "cookies" && prefs.tab !== "manga" && prefs.tab !== "btmanga" && !!view.currentKind
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 12

            ScrollView {
                Layout.fillWidth: true
                Layout.fillHeight: true
                contentWidth: availableWidth
                Column {
                width: parent.width
                SchemePanel {
                    width: parent.width
                    visible: !!(view.currentKind && view.currentKind.scheme)
                    kind: view.currentKind && view.currentKind.scheme ? view.currentKind : {id: "", fields: [], description: "", scheme: {steps: [], destination: ""}}
                    stagingLibrary: view.stagingLibrary
                    onRun: function (params) { view.run(view.currentKind.id, params) }
                    onOpenTab: function (tab) { prefs.tab = tab }
                }
                DownloadForm {
                    visible: !!(view.currentKind && !view.currentKind.scheme)
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
            }


        }
    }

    // 任务：悬浮在右侧、可收起（标签栏右侧的「任务」按钮开关；开始下载时自动弹出）
    Rectangle {
                id: taskPanel
                visible: view.showTasks
                z: 10
                anchors.right: parent.right; anchors.top: parent.top; anchors.bottom: parent.bottom
                anchors.topMargin: 52; anchors.rightMargin: 10; anchors.bottomMargin: 10
                width: Math.min(400, view.width * 0.42)
                radius: 6; color: "#161b22"; border.color: "#30363d"
                ColumnLayout {
                    anchors.fill: parent; anchors.margins: 8
                    spacing: 6
                    RowLayout {
                        Label { text: "任务"; color: "#e6edf3"; font.pixelSize: 14; Layout.fillWidth: true }
                        IconButton { glyph: "close"; size: 28; onClicked: view.showTasks = false; ToolTip.text: "收起" }
                    }
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
                                    Chip { text: "去暂存区"; visible: ["music", "youtube", "bilibili", "x", "other"].indexOf(card.job.kind) >= 0 && !card.job.running && !(card.job.result && card.job.result.plan); onClicked: prefs.tab = "staging" }
                                    Chip { text: "打开文件夹"; visible: !!(card.job.result && card.job.result.path); onClicked: Qt.openUrlExternally("file://" + card.job.result.path) }
                                    Chip { text: "查看下载分卷"; visible: card.job.kind === "btmanga" && !!(card.job.result && card.job.result.work); onClicked: Qt.openUrlExternally("file://" + card.job.result.work) }
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

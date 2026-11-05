import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import QtCore

// Desktop-owned device operations; leaving this page does not cancel a server job.
ScrollView {
    id: view
    objectName: "deviceManager"
    clip: true
    contentWidth: availableWidth
    property var devices: []
    property var libraries: []
    property var books: []
    property var selectedBooks: ({})
    property var job: ({running: false, logs: []})
    property string connectionError: ""
    property bool loadingDevices: false
    property bool loadingBooks: false
    property bool advanced: false
    property bool showLogs: false
    property string setupCommand: ""
    property var buildTools: ({})
    readonly property bool busy: !!job.running
    readonly property var device: deviceBox.currentIndex >= 0 ? devices[deviceBox.currentIndex] : null
    readonly property bool wired: !!device && device.state === "device" && !!device.usb
    readonly property var probe: job.probe && device && job.probe.device === device.serial ? job.probe : null
    readonly property var plan: job.plan || null
    readonly property var filteredBooks: books.filter(function (b) { return b.rel.toLowerCase().indexOf(bookSearch.text.toLowerCase()) >= 0 })
    Settings {
        id: prefs
        category: "deviceOperations"
        property string serial: ""
        property string libraryID: ""
        property string projectDir: ""
        property string targetRoot: ""
        property string keyFile: ""
        property string sshUser: ""
        property string installDir: "omniHub"
        property int sshPort: 8022
        property int serverPort: 8998
        property int reserveGiB: 10
    }
    ApiClient { id: api }
    ApiClient { id: pollApi }
    function size(n) { n = Number(n || 0); return n >= 1073741824 ? (n / 1073741824).toFixed(2) + " GiB" : n >= 1048576 ? (n / 1048576).toFixed(1) + " MiB" : (n / 1024).toFixed(0) + " KiB" }
    function refreshStatus() { pollApi.getJSON("/api/devices/status", function (s) { job = s }) }
    function refreshDevices() {
        loadingDevices = true
        api.send("GET", "/api/devices/list", null, function (s) {
            devices = s.devices || []; libraries = (s.libraries || []).filter(function (l) { return l.is_online })
            connectionError = s.error || (!s.adb ? "未找到 adb，请安装 Android platform-tools。" : "")
            if (!prefs.projectDir) prefs.projectDir = s.default_project || ""
            buildTools = s.build_tools || {}
            if (!prefs.targetRoot) prefs.targetRoot = s.default_root
            if (!prefs.keyFile) prefs.keyFile = s.default_key
            var index = devices.findIndex(function (d) { return d.serial === prefs.serial })
            if (index < 0) index = devices.findIndex(function (d) { return d.usb && d.state === "device" })
            if (index < 0 && devices.length) index = 0
            deviceBox.currentIndex = index; loadingDevices = false
            var libraryIndex = libraries.findIndex(function (l) { return l.id === prefs.libraryID })
            if (libraryIndex < 0) libraryIndex = libraries.findIndex(function (l) { return l.id === s.default_manga_library })
            libraryBox.currentIndex = libraryIndex >= 0 ? libraryIndex : libraries.length ? 0 : -1
            refreshBooks()
        }, function (xhr) { connectionError = xhr.responseText; loadingDevices = false })
    }
    function refreshBooks() {
        if (libraryBox.currentIndex < 0 || libraryBox.currentIndex >= libraries.length) return
        var id = libraries[libraryBox.currentIndex].id
        loadingBooks = true
        api.send("GET", "/api/devices/catalog?library_id=" + encodeURIComponent(id), null, function (s) {
            if (libraryBox.currentIndex >= 0 && libraries[libraryBox.currentIndex].id === id) { books = s || []; selectedBooks = ({}) }
            loadingBooks = false
        }, function (xhr) { books = []; loadingBooks = false; api.error = xhr.responseText })
    }
    function input() {
        return {device: device ? device.serial : "", target_root: prefs.targetRoot, ssh_user: prefs.sshUser,
            key_file: prefs.keyFile, install_dir: prefs.installDir, ssh_port: prefs.sshPort, server_port: prefs.serverPort, project_dir: prefs.projectDir}
    }
    function act(action, body) {
        api.send("POST", "/api/devices/" + action, body || {}, function (s) { if (s.running !== undefined) job = s; refreshStatus() })
    }
    function prepare() {
        var body = input()
        body.library_id = libraries[libraryBox.currentIndex].id
        body.all = allBooks.checked
        body.selection = Object.keys(selectedBooks).filter(function (key) { return selectedBooks[key] })
        body.reserve_gib = prefs.reserveGiB
        body.remove_source = removeSource.checked
        act("manga/plan", body)
    }
    Component.onCompleted: { refreshDevices(); refreshStatus() }
    Timer { interval: 1000; repeat: true; running: view.visible; onTriggered: view.refreshStatus() }
    FolderDialog {
        id: projectDialog
        title: "选择 Deck 上的 omniHub 项目目录"
        onAccepted: prefs.projectDir = decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))
    }
    FileDialog {
        id: keyDialog
        title: "选择连接 Termux 的 SSH 私钥"
        onAccepted: prefs.keyFile = decodeURIComponent(selectedFile.toString().replace(/^file:\/\//, ""))
    }
    ColumnLayout {
        width: view.availableWidth - 32
        x: 16; y: 16
        spacing: 12
        RowLayout {
            Layout.fillWidth: true
            ComboBox { id: deviceBox; objectName: "deviceSelector"; Layout.fillWidth: true; model: view.devices; textRole: "model"; enabled: !view.busy
                onActivated: prefs.serial = view.device ? view.device.serial : ""
            }
            Button { text: view.loadingDevices ? "检测中…" : "刷新设备"; enabled: !view.loadingDevices && !view.busy; onClicked: view.refreshDevices() }
            Button { text: "检查设备 / 版本"; enabled: !!view.device && view.device.state === "device" && !view.busy; onClicked: view.act("probe", view.input()) }
        }
        Label {
            Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: view.wired ? "#3fb950" : "#d29922"; font.pixelSize: 12
            text: !view.device ? "连接手机并开启 USB 调试或无线调试；首次连接需在手机授权。"
                : view.device.state !== "device" ? view.device.state + "：请在手机允许 USB 调试，或重新连接。"
                : (view.wired ? "USB 已连接，可迁移漫画和更新程序" : "无线 ADB：可更新程序；漫画迁移必须插 USB 数据线") + " · " + view.device.serial
        }
        Label { visible: view.connectionError !== ""; text: view.connectionError; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true }
        RowLayout {
            Layout.fillWidth: true
            Label { text: "手机资源库"; color: "#c9d1d9" }
            TextField { Layout.fillWidth: true; text: prefs.targetRoot; enabled: !view.busy; onTextEdited: prefs.targetRoot = text; placeholderText: "手机上已添加到 omniHub 的资源库目录" }
        }
        Label {
            visible: !!view.probe; Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
            text: view.probe ? (view.probe.root_error || "手机可用空间：" + view.size(view.probe.free)) : ""
        }
        Label { visible: api.error !== ""; text: api.error; color: "#f85149"; Layout.fillWidth: true; wrapMode: Text.Wrap }
        Label { visible: !!view.job.error; text: view.job.error || ""; color: "#f85149"; Layout.fillWidth: true; wrapMode: Text.Wrap }
        Label { visible: !!view.job.message; text: view.job.message || ""; color: "#c9d1d9"; Layout.fillWidth: true; wrapMode: Text.Wrap }
        Label {
            visible: !!view.job.progress && view.job.progress.phase !== ""; Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: "#8b949e"; font.pixelSize: 12
            text: view.job.progress ? view.job.progress.phase + " · " + view.job.progress.done + "/" + view.job.progress.total + "\n" + view.job.progress.file + "\n" + view.size(view.job.progress.bytes) + " / " + view.size(view.job.progress.total_bytes) : ""
        }
        ProgressBar { Layout.fillWidth: true; visible: view.busy; indeterminate: !view.job.progress || !view.job.progress.total_bytes; from: 0; to: view.job.progress && view.job.progress.total_bytes ? view.job.progress.total_bytes : 1; value: view.job.progress ? view.job.progress.bytes : 0 }
        Button { text: view.job.kind === "deploy" ? "停止任务（手机切换阶段仅停止等待）" : "暂停任务（保留漫画断点）"; visible: view.busy; onClicked: view.act("pause", {}) }
        Button { text: view.showLogs ? "收起过程日志" : "过程日志"; visible: (view.job.logs || []).length > 0; onClicked: view.showLogs = !view.showLogs }
        ScrollView {
            visible: view.showLogs; Layout.fillWidth: true; Layout.preferredHeight: 160
            TextArea { readOnly: true; selectByMouse: true; wrapMode: TextEdit.WrapAnywhere; text: (view.job.logs || []).join("\n"); font.pixelSize: 12; color: "#adbac7" }
        }
        GroupBox {
            title: "普通漫画迁移（USB）"; Layout.fillWidth: true
            ColumnLayout {
                anchors.fill: parent; spacing: 8
                RowLayout {
                    Layout.fillWidth: true
                    Label { text: "本机来源库"; color: "#c9d1d9" }
                    ComboBox { id: libraryBox; objectName: "sourceLibrary"; Layout.fillWidth: true; model: view.libraries; textRole: "name"; enabled: !view.busy; onActivated: { prefs.libraryID = view.libraries[currentIndex].id; view.refreshBooks() } }
                    Button { text: "刷新漫画"; enabled: !view.busy && !view.loadingBooks; onClicked: view.refreshBooks() }
                }
                CheckBox { id: allBooks; text: "全部普通漫画（" + view.books.length + " 本）"; checked: true; enabled: !view.busy }
                ColumnLayout {
                    visible: !allBooks.checked; Layout.fillWidth: true
                    TextField { id: bookSearch; Layout.fillWidth: true; placeholderText: "搜索漫画，勾选要迁移的书" }
                    ListView {
                        id: bookList; Layout.fillWidth: true; Layout.preferredHeight: 210; clip: true; model: view.filteredBooks
                        ScrollBar.vertical: ScrollBar { }
                        delegate: CheckDelegate {
                            required property var modelData
                            width: bookList.width
                            text: modelData.rel + " · " + view.size(modelData.size)
                            checked: !!view.selectedBooks[modelData.rel]; enabled: !view.busy
                            onToggled: { var next = Object.assign({}, view.selectedBooks); next[modelData.rel] = checked; view.selectedBooks = next }
                            ToolTip.visible: hovered; ToolTip.text: modelData.rel
                        }
                    }
                }
                RowLayout {
                    Label { text: "手机至少预留"; color: "#c9d1d9" }
                    SpinBox { from: 1; to: 1024; value: prefs.reserveGiB; editable: true; enabled: !view.busy; onValueModified: prefs.reserveGiB = value }
                    Label { text: "GiB"; color: "#c9d1d9" }
                }
                CheckBox { id: removeSource; text: "迁移完成后，将已校验的本机副本移入系统回收站"; checked: false; enabled: !view.busy }
                Label { text: "仅处理普通漫画 standard 目录中的 CBZ。默认保留本机；同名不同内容不会覆盖。断线或暂停后保留文件内部断点。"; color: "#8b949e"; font.pixelSize: 12; wrapMode: Text.Wrap; Layout.fillWidth: true }
                Button { text: "检查迁移内容与空间"; objectName: "prepareManga"; enabled: view.wired && !view.busy && libraryBox.currentIndex >= 0 && view.books.length > 0; onClicked: view.prepare() }
                Label {
                    visible: !!view.plan; Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: "#c9d1d9"; font.pixelSize: 12
                    text: view.plan ? "当前计划：" + view.plan.source + " → " + view.plan.target + "\n" + view.plan.done + "/" + view.plan.count + " 本完成 · " + view.size(view.plan.total) + "，待传 " + view.size(view.plan.needed) + "，预留 " + view.size(view.plan.reserve)
                         + "\n" + (view.plan.remove_source ? "已选择迁移：校验完成后回收本机副本" : "复制：保留本机副本") : ""
                }
                PrimaryButton { text: view.plan && view.plan.done === view.plan.count ? "本次迁移已完成" : view.plan && view.plan.remove_source ? "开始 / 继续迁移并回收本机" : "开始 / 继续复制到手机"; objectName: "startManga"; visible: !!view.plan; enabled: view.wired && !view.busy && view.plan.done < view.plan.count; onClicked: { var body = view.input(); body.plan_id = view.plan.id; view.act("manga/start", body) } }
            }
        }
        GroupBox {
            title: "更新 / 部署手机 omniHub（支持无线）"; Layout.fillWidth: true
            ColumnLayout {
                anchors.fill: parent; spacing: 8
                Label { Layout.fillWidth: true; wrapMode: Text.WrapAnywhere; color: "#c9d1d9"; text: !view.probe ? "先点击“检查设备 / 版本”。" : view.probe.ssh_ready ? (view.probe.installed ? "手机版本：" + view.probe.version + (view.probe.running ? " · 运行中" : " · 已停止") : "Termux 已就绪，可以首次部署") : "Termux SSH 未就绪：" + view.probe.ssh_error }
                RowLayout {
                    Layout.fillWidth: true
                    TextField { id: projectPath; objectName: "projectPath"; Layout.fillWidth: true; text: prefs.projectDir; placeholderText: "Deck 上的 omniHub 项目目录"; enabled: !view.busy; onTextEdited: prefs.projectDir = text }
                    Button { text: "选择项目"; enabled: !view.busy; onClicked: projectDialog.open() }
                }
                Label { visible: !view.buildTools.go || !view.buildTools.pnpm || !view.buildTools.node; Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#d29922"; text: "本机构建需要 Go、pnpm 和 Node，请安装缺少的工具后刷新设备。" }
                ColumnLayout {
                    visible: !!view.probe && view.probe.ssh_ready && !view.probe.installed; Layout.fillWidth: true
                    TextField { id: adminName; Layout.fillWidth: true; placeholderText: "首次部署：管理员名称"; text: "admin" }
                    TextField { id: adminPassword; Layout.fillWidth: true; placeholderText: "首次部署：至少 8 位密码"; echoMode: TextInput.Password }
                }
                Label { text: "从本机当前代码构建服务端及 Web（包括未提交改动），通过 ADB 传到手机 Termux 并重启服务。手机使用浏览器访问。保留账号、配置和资源库；启动失败尝试恢复旧程序。"; color: "#8b949e"; font.pixelSize: 12; wrapMode: Text.Wrap; Layout.fillWidth: true }
                PrimaryButton {
                    objectName: "deployPhone"; text: view.probe && view.probe.installed ? "构建本机代码并更新手机服务端" : "构建并部署手机服务端"
                    enabled: !view.busy && !!view.probe && view.probe.ssh_ready && prefs.projectDir !== "" && view.buildTools.go && view.buildTools.pnpm && view.buildTools.node && (view.probe.installed || (adminName.text.trim() !== "" && adminPassword.text.length >= 8))
                    onClicked: { var body = view.input(); body.admin_name = adminName.text; body.admin_password = adminPassword.text; view.act("deploy", body); adminPassword.text = "" }
                }
            }
        }
        Button { text: view.advanced ? "收起连接设置" : "连接设置 / 首次配置帮助"; onClicked: view.advanced = !view.advanced }
        ColumnLayout {
            visible: view.advanced; Layout.fillWidth: true; spacing: 8
            Label { text: "Termux SSH 用户留空可自动识别。没有 SSH 时，在手机 Termux 执行下方准备命令。"; wrapMode: Text.Wrap; Layout.fillWidth: true; color: "#8b949e" }
            TextField { Layout.fillWidth: true; text: prefs.sshUser; placeholderText: "SSH 用户（自动识别）"; enabled: !view.busy; onTextEdited: prefs.sshUser = text }
            RowLayout {
                Layout.fillWidth: true
                TextField { Layout.fillWidth: true; text: prefs.keyFile; placeholderText: "SSH 私钥路径"; enabled: !view.busy; onTextEdited: prefs.keyFile = text }
                Button { text: "选择私钥"; enabled: !view.busy; onClicked: keyDialog.open() }
            }
            TextField { Layout.fillWidth: true; text: prefs.installDir; placeholderText: "安装目录（相对 Termux HOME）"; enabled: !view.busy; onTextEdited: prefs.installDir = text }
            RowLayout {
                Label { text: "SSH 端口"; color: "#c9d1d9" }
                SpinBox { from: 1; to: 65535; value: prefs.sshPort; editable: true; onValueModified: prefs.sshPort = value; enabled: !view.busy }
                Label { text: "服务端口"; color: "#c9d1d9" }
                SpinBox { from: 1; to: 65535; value: prefs.serverPort; editable: true; onValueModified: prefs.serverPort = value; enabled: !view.busy }
            }
            Button { text: "生成手机准备命令"; onClicked: api.getJSON("/api/devices/setup-command?key_file=" + encodeURIComponent(prefs.keyFile), function (s) { view.setupCommand = s.command }) }
            TextArea { id: setupText; visible: view.setupCommand !== ""; Layout.fillWidth: true; text: view.setupCommand; readOnly: true; selectByMouse: true; wrapMode: TextEdit.WrapAnywhere; font.pixelSize: 12 }
            Button { visible: view.setupCommand !== ""; text: "复制准备命令"; onClicked: { setupText.selectAll(); setupText.copy(); setupText.deselect() } }
        }
        Item { Layout.preferredHeight: 16 }
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Window
import QtQuick.Layouts
import QtCore

DarkWindow {
    id: mainWindow
    width: 1280
    height: 800
    visible: true
    title: "omniHub Native Client"
    onClosing: {
        if (typeof nativeResume !== "undefined") nativeResume.flush()
        if (textLoader.item) textLoader.item.flush()
        audio.save()
    }
    // 其它页面要求音声页打开并播放的专辑（逻辑路径），音声页创建时读取一次。
    property string audioRequest: ""
    // 音声播放引擎：全局一份，切换页面不中断。
    AudioEngine { id: audio }
    // 记住上次打开的页面。
    Settings { id: uiPrefs; category: "main"; property int page: 4 }

    function selectPage(index) {
        if (stack.currentIndex === index) return
        // Capture before Loader destroys the item; hidden pages must not
        // retain decoders, GPU buffers or request objects.
        switchStarted = Date.now()
        stack.currentIndex = index
        uiPrefs.page = index
        switchCreated = Date.now()
    }

    // Settings 在组件完成时才读出上次的页码；在那之前不激活任何页面，避免先按默认值建出一个页面再销毁。
    property bool pagesReady: false
    // 开发验证：OMNIHUB_DEV_SWITCH=<毫秒> 在几个页面之间轮流切换，打印「建页耗时」和「到下一帧画完」的耗时。
    property double switchStarted: 0
    property double switchCreated: 0
    onFrameSwapped: if (switchStarted > 0) { console.log("switch-timing page", stack.currentIndex, "create", switchCreated - switchStarted, "ms frame", Date.now() - switchStarted, "ms"); switchStarted = 0 }
    Timer {
        property var order: [4, 5, 1, 3, 6, 7]
        property int step: 0
        interval: typeof devSwitch !== "undefined" && devSwitch > 0 ? devSwitch : 3000; repeat: true
        running: typeof devSwitch !== "undefined" && devSwitch > 0 && mainWindow.serviceReady
        onTriggered: { mainWindow.selectPage(order[step % order.length]); step++ }
    }
    // 依赖本机服务的页面等服务连上（地址与凭证已知）后再创建；否则页面创建时发的请求拿不到地址、直接放弃。
    // 只置位一次：之后服务短暂断开不销毁已打开的页面（页面自己显示请求失败）。
    property bool serviceReady: false
    function checkService() { if (!serviceReady && backendApi.connected && backendApi.stateReady) serviceReady = true }
    Component.onCompleted: { pagesReady = true; backendApi.refresh() }
    Connections {
        target: backendApi
        function onStatusChanged() {
            mainWindow.checkService()
            if (backendApi.stateReady && backendApi.connected && !backendApi.hasAdmin) setupDialog.open()
            if (backendApi.hasAdmin) setupDialog.close()
        }
        function onLibrariesChanged() {
            mainWindow.checkService()
            if (backendApi.stateReady && backendApi.connected && !backendApi.hasAdmin) setupDialog.open()
            // A standalone local file must keep playing when the service is
            // absent/offline; recovery navigation cannot destroy its decoder.
            // 只在资源库请求完成且确认状态离线时跳转；请求进行中 stateReady 尚未更新。不写入记住的页面。
            if (!backendApi.libraryBusy && backendApi.connected && !backendApi.stateReady) stack.currentIndex = 2
        }
    }
    Dialog {
        id: setupDialog
        anchors.centerIn: parent
        modal: true
        closePolicy: Popup.NoAutoClose
        title: "创建管理员账号"
        Column {
            spacing: 12
            TextField { id: adminName; placeholderText: "管理员用户名"; maximumLength: 64 }
            TextField { id: adminPassword; placeholderText: "管理员密码"; echoMode: TextInput.Password; maximumLength: 72 }
            Label { text: backendApi.error; color: "#f85149"; wrapMode: Text.Wrap; width: 320 }
            Button { text: "完成设置"; enabled: adminName.text.length > 0 && adminPassword.text.length > 0; onClicked: backendApi.setupAdmin(adminName.text, adminPassword.text) }
        }
    }

    // 多联放映独立窗口：独立于页面切换，关闭窗口即释放解码器。
    Loader {
        id: matrixWindow
        active: false
        sourceComponent: Component { MatrixWindow { audioEngine: audio; onClosing: Qt.callLater(function () { matrixWindow.active = false }) } }
    }

    Column {
        anchors.fill: parent

        // 顶栏导航（按常用度排序）
        Rectangle {
            width: parent.width
            height: 40
            color: "#161b22"
            Row {
                anchors.verticalCenter: parent.verticalCenter
                x: 8
                spacing: 4
                Repeater {
                    model: [{page: 4, label: "短视频"}, {page: 5, label: "相册"},
                            {page: 1, label: "游戏"}, {page: 3, label: "小说"}, {page: 6, label: "音声"}, {page: 7, label: "下载"}, {page: 2, label: "资源库"}]
                    delegate: Chip {
                        required property var modelData
                        text: modelData.label
                        selected: stack.currentIndex === modelData.page
                        onClicked: {
                            mainWindow.selectPage(modelData.page)
                            if (modelData.page === 2) backendApi.refreshLibraries()
                        }
                    }
                }
            }
            Rectangle { anchors.bottom: parent.bottom; width: parent.width; height: 1; color: "#30363d" }
        }

        // 视窗内容切换（服务未连上时，依赖服务的页面先显示提示）
        Label {
            visible: mainWindow.pagesReady && !mainWindow.serviceReady && stack.currentIndex !== 2
            width: parent.width; height: visible ? 40 : 0
            horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
            text: backendApi.error ? backendApi.error + "（可在「资源库」页检查）" : "正在连接 omniHub 服务…"
            color: "#8b949e"
        }
        StackLayout {
            id: stack
            width: parent.width
            height: parent.height - 40 - (audioBar.visible ? audioBar.height : 0)
            currentIndex: Math.max(1, uiPrefs.page)

            // 页 0 原为内嵌播放器。播放器改为系统「打开方式」调用的独立外部窗口（规格 9.2/9.3），这里留空占位以保持页码。
            Item { }
            Loader {
                active: mainWindow.pagesReady && mainWindow.serviceReady && stack.currentIndex === 1
                sourceComponent: Component { GamesView { } }
            }
            Loader {
                active: mainWindow.pagesReady && stack.currentIndex === 2
                sourceComponent: Component { LibraryView { } }
            }
            Loader { id: textLoader; active: mainWindow.pagesReady && mainWindow.serviceReady && stack.currentIndex === 3; sourceComponent: Component { NovelView { } } }
            Loader {
                active: mainWindow.pagesReady && mainWindow.serviceReady && stack.currentIndex === 4
                sourceComponent: Component {
                    ShortVideoView {
                        audioEngine: audio
                        matrixWindowOpen: matrixWindow.active
                        onMatrixWindowRequested: {
                            if (matrixWindow.item) { matrixWindow.item.raise(); matrixWindow.item.requestActivate() }
                            else matrixWindow.active = true
                        }
                    }
                }
            }
            Loader { active: mainWindow.pagesReady && mainWindow.serviceReady && stack.currentIndex === 5; sourceComponent: Component { GalleryView { } } }
            Loader { active: mainWindow.pagesReady && mainWindow.serviceReady && stack.currentIndex === 6; sourceComponent: Component { AudioView { engine: audio; request: mainWindow.audioRequest } } }
                        Loader {
                active: mainWindow.pagesReady && mainWindow.serviceReady && stack.currentIndex === 7
                sourceComponent: Component {
                    DownloadsView {
                        // 「去收听」：切到音声页打开该专辑并开始播放。
                        onOpenAudioAlbum: function (album) {
                            mainWindow.audioRequest = album
                            mainWindow.selectPage(6)
                            Qt.callLater(function () { mainWindow.audioRequest = "" })
                        }
                    }
                }
            }
        }
        AudioBar {
            id: audioBar
            width: parent.width
            engine: audio
            visible: audio.active
            onOpenRequested: mainWindow.selectPage(6)
        }
    }
}

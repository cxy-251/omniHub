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
    }
    // 记住上次打开的页面（游戏让出后恢复时以 restoredUI 为准）。
    Settings { id: uiPrefs; category: "main"; property int page: 4 }
    property var lifecycle: typeof nativeLifecycle === "undefined" ? null : nativeLifecycle
    property var restoredUI: lifecycle ? lifecycle.restored : ({})

    function yieldForGame(sessionId) {
        if (!lifecycle) return
        lifecycle.yieldToGame(sessionId, {page: stack.currentIndex})
    }

    function selectPage(index) {
        if (stack.currentIndex === index) return
        // Capture before Loader destroys the item; hidden pages must not
        // retain decoders, GPU buffers or request objects.
        stack.currentIndex = index
        uiPrefs.page = index
    }

    // Settings 在组件完成时才读出上次的页码；在那之前不激活任何页面，避免先按默认值建出一个页面再销毁。
    property bool pagesReady: false
    Component.onCompleted: { pagesReady = true; backendApi.refresh() }
    Connections {
        target: backendApi
        function onStatusChanged() {
            if (backendApi.stateReady && backendApi.connected && !backendApi.hasAdmin) setupDialog.open()
            if (backendApi.hasAdmin) setupDialog.close()
        }
        function onLibrariesChanged() {
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

    Column {
        anchors.fill: parent
        Label {
            width: parent.width
            visible: !!mainWindow.lifecycle && mainWindow.lifecycle.error.length > 0
            text: mainWindow.lifecycle ? mainWindow.lifecycle.error : ""
            textFormat: Text.PlainText
            color: "#f85149"
            wrapMode: Text.Wrap
        }

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
                            {page: 1, label: "游戏"}, {page: 3, label: "小说"}, {page: 2, label: "资源库"}]
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

        // 视窗内容切换
        StackLayout {
            id: stack
            width: parent.width
            height: parent.height - 40
            currentIndex: Math.max(1, mainWindow.restoredUI.page !== undefined ? mainWindow.restoredUI.page : uiPrefs.page)

            // 页 0 原为内嵌播放器。播放器改为系统「打开方式」调用的独立外部窗口（规格 9.2/9.3），这里留空占位以保持页码。
            Item { }
            Loader {
                active: mainWindow.pagesReady && stack.currentIndex === 1
                sourceComponent: Component { GamesView { onGameStarted: function(sessionId) { mainWindow.yieldForGame(sessionId) } } }
            }
            Loader {
                active: mainWindow.pagesReady && stack.currentIndex === 2
                sourceComponent: Component { LibraryView { } }
            }
            Loader { id: textLoader; active: mainWindow.pagesReady && stack.currentIndex === 3; sourceComponent: Component { NovelView { } } }
            Loader { active: mainWindow.pagesReady && stack.currentIndex === 4; sourceComponent: Component { ShortVideoView { } } }
            Loader { active: mainWindow.pagesReady && stack.currentIndex === 5; sourceComponent: Component { GalleryView { } } }
        }
    }
}

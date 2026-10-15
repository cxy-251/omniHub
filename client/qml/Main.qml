import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Window 2.15
import QtQuick.Layouts 1.15
import QtCore

Window {
    id: mainWindow
    width: 1280
    height: 800
    visible: true
    title: "omniHub Native Client"
    color: "#0d1117"
    onClosing: {
        if (typeof nativeResume !== "undefined") nativeResume.flush()
        if (textLoader.item) textLoader.item.flush()
    }
    // 记住上次打开的页面（游戏让出后恢复时以 restoredUI 为准）。
    Settings { id: uiPrefs; category: "main"; property int page: 4 }
    property var lifecycle: typeof nativeLifecycle === "undefined" ? null : nativeLifecycle
    property var restoredUI: lifecycle ? lifecycle.restored : ({})
    property string savedMediaSource: restoredUI.source || initialExternalFile
    property real savedMediaPosition: restoredUI.position || 0
    property bool savedMediaPaused: !!restoredUI.paused
    property var savedPlaylistEntries: restoredUI.entries || []
    property int savedPlaylistIndex: restoredUI.index !== undefined ? restoredUI.index : -1

    function yieldForGame(sessionId) {
        if (!lifecycle) return
        if (playerLoader.item) {
            savedMediaSource = playerLoader.item.source
            savedMediaPosition = playerLoader.item.position
            savedMediaPaused = playerLoader.item.pauseRequested
            savedPlaylistEntries = playerLoader.item.playlistEntries.slice()
            savedPlaylistIndex = playerLoader.item.playlistIndex
        }
        lifecycle.yieldToGame(sessionId, {source: savedMediaSource, position: savedMediaPosition, paused: savedMediaPaused, page: stack.currentIndex, entries: savedPlaylistEntries, index: savedPlaylistIndex})
    }

    function selectPage(index) {
        if (stack.currentIndex === index) return
        // Capture before Loader destroys the item; hidden pages must not
        // retain decoders, GPU buffers or request objects.
        if (playerLoader.item) {
            savedMediaSource = playerLoader.item.source
            savedMediaPosition = playerLoader.item.position
            savedMediaPaused = playerLoader.item.pauseRequested
            savedPlaylistEntries = playerLoader.item.playlistEntries.slice()
            savedPlaylistIndex = playerLoader.item.playlistIndex
        }
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
            if (!backendApi.libraryBusy && backendApi.connected && !backendApi.stateReady && !(playerLoader.item && playerLoader.item.source.length > 0)) stack.currentIndex = 2
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

        // 顶栏导航
        Rectangle {
            width: parent.width
            height: 48
            color: "#161b22"
            border.color: "#30363d"

            Row {
                anchors.centerIn: parent
                spacing: 20

                Button {
                    text: "通用播放器"
                    highlighted: stack.currentIndex === 0
                    onClicked: mainWindow.selectPage(0)
                }
                Button {
                    text: "独立游戏大厅"
                    highlighted: stack.currentIndex === 1
                    onClicked: mainWindow.selectPage(1)
                }
                Button {
                    text: "资源库管理"
                    highlighted: stack.currentIndex === 2
                    onClicked: { mainWindow.selectPage(2); backendApi.refreshLibraries() }
                }
                Button { text: "文本阅读"; highlighted: stack.currentIndex === 3; onClicked: mainWindow.selectPage(3) }
                Button { text: "短视频"; highlighted: stack.currentIndex === 4; onClicked: mainWindow.selectPage(4) }
                Button { text: "相册"; highlighted: stack.currentIndex === 5; onClicked: mainWindow.selectPage(5) }
            }
        }

        // 视窗内容切换
        StackLayout {
            id: stack
            width: parent.width
            height: parent.height - 48
            currentIndex: mainWindow.restoredUI.page !== undefined ? mainWindow.restoredUI.page : (initialExternalFile !== "" ? 0 : uiPrefs.page)

            Loader {
                id: playerLoader
                active: mainWindow.pagesReady && stack.currentIndex === 0
                sourceComponent: Component { PlayerView { } }
                onLoaded: {
                    item.restorePlayback(mainWindow.savedMediaSource, mainWindow.savedMediaPosition, mainWindow.savedMediaPaused, mainWindow.savedPlaylistEntries, mainWindow.savedPlaylistIndex)
                    if (typeof mprisService !== "undefined") mprisService.bind(item)
                    if (typeof nativeResume !== "undefined") nativeResume.bind(item, mainWindow.savedMediaPosition === 0)
                }
            }

            Loader {
                active: mainWindow.pagesReady && stack.currentIndex === 1
                sourceComponent: Component { GameLauncherView { onGameStarted: function(sessionId) { mainWindow.yieldForGame(sessionId) } } }
            }
            Loader {
                active: mainWindow.pagesReady && stack.currentIndex === 2
                sourceComponent: Component { LibraryView { } }
            }
            Loader { id: textLoader; active: mainWindow.pagesReady && stack.currentIndex === 3; sourceComponent: Component { TextLibraryView { } } }
            Loader { active: mainWindow.pagesReady && stack.currentIndex === 4; sourceComponent: Component { ShortVideoView { } } }
            Loader { active: mainWindow.pagesReady && stack.currentIndex === 5; sourceComponent: Component { GalleryView { } } }
        }
    }
}

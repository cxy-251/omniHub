import QtQuick

// 多联放映的独立窗口：不随主窗口切换页面而销毁；F 或「全屏」切换全屏，Esc 退出全屏，关闭窗口即结束放映。
DarkWindow {
    id: window
    width: 1280; height: 760
    visible: true
    title: "omniHub 多联放映"
    property var audioEngine: null
    MatrixView {
        id: matrixView
        anchors.fill: parent
        detached: true
        audioEngine: window.audioEngine
        fullscreen: window.visibility === Window.FullScreen
        onExitRequested: window.close()
        onFullscreenToggled: window.visibility = window.visibility === Window.FullScreen ? Window.Windowed : Window.FullScreen
        Component.onCompleted: forceActiveFocus()
    }
    onClosing: matrixView.savePositions()
}

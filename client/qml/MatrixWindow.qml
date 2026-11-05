import QtQuick

// 多联放映的独立窗口：不随主窗口切换页面而销毁；F 或「全屏」切换全屏，Esc 退出全屏，关闭窗口即结束放映。
DarkWindow {
    id: window
    width: 1280; height: 760
    visible: true
    property string domain: "shortvideo"
    title: domain === "gallery" ? "omniHub 相册轮播" : "omniHub 多联放映"
    property var audioEngine: null
    signal audioOpenRequested()
    MatrixView {
        id: matrixView
        anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
        anchors.bottom: audioBar.visible ? audioBar.top : parent.bottom
        detached: true
        domain: window.domain
        audioEngine: window.audioEngine
        fullscreen: window.visibility === Window.FullScreen
        onExitRequested: window.close()
        onFullscreenToggled: window.visibility = window.visibility === Window.FullScreen ? Window.Windowed : Window.FullScreen
        Component.onCompleted: forceActiveFocus()
    }
    Loader {
        id: audioBar
        anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
        height: item ? item.implicitHeight : 0
        active: !!window.audioEngine
        visible: active && window.audioEngine.active
        sourceComponent: AudioBar {
            engine: window.audioEngine
            onOpenRequested: window.audioOpenRequested()
        }
    }
    onClosing: matrixView.savePositions()
}

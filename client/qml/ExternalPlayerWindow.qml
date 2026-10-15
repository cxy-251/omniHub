import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

DarkWindow {
    id: externalWindow
    width: 960; height: 600
    visible: true
    title: "omniHub 外部播放器"
    onClosing: nativeResume.flush()
    function openFiles(files) {
        if (player.openFiles(files)) {
            show()
            raise()
            requestActivate()
        }
    }
    PlayerView { id: player; anchors.fill: parent }
    Component.onCompleted: {
        nativeResume.bind(player)
        if (typeof mprisService !== "undefined") mprisService.bind(player)
        openFiles(initialExternalFiles)
    }
}

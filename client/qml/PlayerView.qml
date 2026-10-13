import QtQuick 2.15
import QtQuick.Controls 2.15
import omniHub.Media 1.0

Item {
    id: playerView
    property alias source: mpv.source

    MpvItem {
        id: mpv
        anchors.fill: parent
    }

    TouchControls {
        anchors.fill: parent
        playerItem: mpv
        onShowOsd: function(txt) {
            osdText.text = txt
            osdBox.visible = true
            osdTimer.restart()
        }
    }

    Rectangle {
        id: osdBox
        visible: false
        anchors.centerIn: parent
        width: 160
        height: 50
        color: "#aa000000"
        radius: 8

        Text {
            id: osdText
            anchors.centerIn: parent
            color: "#ffffff"
            font.pixelSize: 16
        }

        Timer {
            id: osdTimer
            interval: 1200
            onTriggered: osdBox.visible = false
        }
    }

    // 底部浮动控制栏
    Rectangle {
        anchors.bottom: parent.bottom
        anchors.left: parent.left
        anchors.right: parent.right
        height: 48
        color: "#cc161b22"

        Row {
            anchors.centerIn: parent
            spacing: 16

            Button {
                text: mpv.playing ? "暂停" : "播放"
                onClicked: mpv.playing ? mpv.pause() : mpv.play()
            }

            Slider {
                width: 400
                from: 0
                to: Math.max(1, mpv.duration)
                value: mpv.position
                onMoved: mpv.position = value
            }

            Text {
                color: "#e6edf3"
                anchors.verticalCenter: parent.verticalCenter
                text: Math.round(mpv.position) + "s / " + Math.round(mpv.duration) + "s"
            }
        }
    }
}

import QtQuick
import QtQuick.Controls

// Toggle：统一的开关。开 = 蓝色轨道、圆钮在右；关 = 灰色轨道、圆钮在左（默认 Fusion 开关在深色主题里分不清开关）。
Switch {
    id: control
    font.pixelSize: 13
    indicator: Rectangle {
        implicitWidth: 38; implicitHeight: 20
        x: control.leftPadding
        y: parent.height / 2 - height / 2
        radius: 10
        color: control.checked ? "#1f6feb" : "#30363d"
        border.color: control.checked ? "#58a6ff" : "#484f58"
        opacity: control.enabled ? 1 : 0.5
        Rectangle {
            x: control.checked ? parent.width - width - 2 : 2
            anchors.verticalCenter: parent.verticalCenter
            width: 16; height: 16; radius: 8
            color: control.checked ? "#ffffff" : "#8b949e"
            Behavior on x { NumberAnimation { duration: 120 } }
        }
    }
    contentItem: Label {
        text: control.text; font: control.font
        color: control.enabled ? "#c9d1d9" : "#6e7681"
        verticalAlignment: Text.AlignVCenter
        leftPadding: control.indicator.width + 8
    }
}

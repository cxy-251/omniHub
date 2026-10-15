import QtQuick
import QtQuick.Controls

// SeekBar：细进度条，悬停或拖动时轨道加粗并显示圆点。
Slider {
    id: bar
    hoverEnabled: true
    implicitHeight: 20
    readonly property bool active: hovered || pressed
    background: Rectangle {
        x: bar.leftPadding
        y: bar.topPadding + bar.availableHeight / 2 - height / 2
        width: bar.availableWidth
        height: bar.active ? 6 : 3
        radius: height / 2
        color: "#55ffffff"
        Rectangle {
            width: bar.visualPosition * parent.width
            height: parent.height
            radius: parent.radius
            color: "#58a6ff"
        }
    }
    handle: Rectangle {
        x: bar.leftPadding + bar.visualPosition * (bar.availableWidth - width)
        y: bar.topPadding + bar.availableHeight / 2 - height / 2
        width: 14; height: 14; radius: 7
        color: "#ffffff"
        visible: bar.active
    }
}

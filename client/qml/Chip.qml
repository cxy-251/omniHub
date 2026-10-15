import QtQuick
import QtQuick.Controls

// Chip：紧凑的可选标签按钮（筛选条、模式切换、导航共用）。
Button {
    id: chip
    property bool selected: false
    font.pixelSize: 13
    padding: 4
    leftPadding: 10
    rightPadding: 10
    implicitHeight: 28
    contentItem: Label {
        text: chip.text; font: chip.font
        color: chip.selected ? "#58a6ff" : (chip.hovered ? "#e6edf3" : "#adbac7")
        horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
    }
    background: Rectangle {
        radius: 14
        color: chip.selected ? "#1f2a3a" : (chip.down ? "#21262d" : "#161b22")
        border.color: chip.selected ? "#58a6ff" : "#30363d"
    }
}

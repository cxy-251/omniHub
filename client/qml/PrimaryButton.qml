import QtQuick
import QtQuick.Controls

// PrimaryButton：主要操作按钮（开始对局、开始下载、登录……）。蓝底白字；不可用时变灰。
// Fusion 的 highlighted 按钮在深色配色里画成黑色，看不出是主按钮，所以单独画。
Button {
    id: control
    font.pixelSize: 14
    implicitHeight: 34
    leftPadding: 18; rightPadding: 18
    contentItem: Label {
        text: control.text; font: control.font
        color: control.enabled ? "#ffffff" : "#8b949e"
        horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
    }
    background: Rectangle {
        radius: 6
        color: !control.enabled ? "#21262d" : control.down ? "#1158c7" : control.hovered ? "#2f81f7" : "#1f6feb"
        border.color: control.enabled ? "#388bfd" : "#30363d"
    }
}

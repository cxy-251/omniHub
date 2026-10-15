import QtQuick
import QtQuick.Controls

// IconButton：圆形悬停底色的图标按钮。glyph 为 client/icons 下的图标名。
AbstractButton {
    id: button
    property string glyph: ""
    property int size: 36
    implicitWidth: size
    implicitHeight: size
    hoverEnabled: true
    opacity: enabled ? 1 : 0.35
    background: Rectangle {
        radius: width / 2
        color: button.down ? "#40ffffff" : (button.hovered || button.checked ? "#26ffffff" : "transparent")
    }
    contentItem: Image {
        source: button.glyph ? "qrc:/icons/" + button.glyph + ".svg" : ""
        sourceSize: Qt.size(Math.round(button.size * 0.62), Math.round(button.size * 0.62))
        fillMode: Image.Pad
        horizontalAlignment: Image.AlignHCenter
        verticalAlignment: Image.AlignVCenter
    }
    ToolTip.visible: hovered && ToolTip.text.length > 0
    ToolTip.delay: 600
}

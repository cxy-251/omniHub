import QtQuick

// WheelScroll：放在 Flickable / GridView / ListView 里，让鼠标滚轮按固定距离直接滚动。
// Qt 默认把滚轮当成一次「甩动」，每格只挪一点还带减速，桌面上感觉阻力很大。
// 触控板提供像素增量时按像素滚动；触屏拖动不受影响。
WheelHandler {
    id: handler
    // 在 Flickable 里声明的对象会被放进它的 contentItem，所以沿父级往上找第一个可滚动的视图。
    property Item flick: {
        var p = handler.parent
        while (p && !(p.contentY !== undefined && typeof p.flick === "function")) p = p.parent
        return p
    }
    property real step: 120          // 每格滚动的像素
    property bool horizontal: false
    acceptedDevices: PointerDevice.Mouse | PointerDevice.TouchPad
    target: null
    onWheel: function (event) {
        if (!flick) return
        var delta = horizontal ? (event.pixelDelta.x || event.pixelDelta.y) : event.pixelDelta.y
        if (delta === 0) {
            var angle = horizontal ? (event.angleDelta.x || event.angleDelta.y) : event.angleDelta.y
            delta = angle / 120 * step
        }
        if (horizontal) {
            var maxX = Math.max(0, flick.contentWidth - flick.width) + flick.originX
            flick.contentX = Math.max(flick.originX, Math.min(maxX, flick.contentX - delta))
        } else {
            var maxY = Math.max(0, flick.contentHeight - flick.height) + flick.originY
            flick.contentY = Math.max(flick.originY, Math.min(maxY, flick.contentY - delta))
        }
        event.accepted = true
    }
}

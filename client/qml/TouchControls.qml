import QtQuick 2.15

Item {
    id: root
    property var playerItem: null

    signal showOsd(string text)

    MultiPointTouchArea {
        anchors.fill: parent
        mouseEnabled: true

        property real startX: 0
        property real startY: 0
        property bool isDragging: false

        onPressed: function(touchPoints) {
            if (touchPoints.length > 0) {
                startX = touchPoints[0].x
                startY = touchPoints[0].y
                isDragging = true
            }
        }

        onUpdated: function(touchPoints) {
            if (!isDragging || touchPoints.length === 0 || !playerItem) return
            var p = touchPoints[0]
            var dx = p.x - startX
            var dy = p.y - startY

            // Left adjusts decoded video colors; right adjusts player volume.
            if (Math.abs(dy) > Math.abs(dx) && Math.abs(dy) > 20) {
                if (startX < root.width / 2) {
                    playerItem.videoBrightness = Math.max(-100, Math.min(100, playerItem.videoBrightness - dy * 0.2))
                    root.showOsd("画面亮度: " + Math.round(playerItem.videoBrightness))
                } else {
                    var newVol = Math.max(0, Math.min(100, playerItem.volume - (dy * 0.2)))
                    playerItem.volume = newVol
                    root.showOsd("音量: " + Math.round(newVol) + "%")
                }
                startY = p.y
            }
            // 水平滑动：快进快退
            else if (Math.abs(dx) > 30) {
                var seekSec = dx > 0 ? 5 : -5
                playerItem.seekRelative(seekSec)
                root.showOsd((seekSec > 0 ? "快进 +" : "快退 ") + Math.abs(seekSec) + "s")
                startX = p.x
            }
        }

        onReleased: function() {
            isDragging = false
        }
    }
    TapHandler {
        property bool boosting: false
        property real previousRate: 1
        onLongPressed: {
            if (!root.playerItem) return
            previousRate = root.playerItem.playbackRate
            boosting = true
            root.playerItem.playbackRate = Math.max(2, previousRate)
            root.showOsd(root.playerItem.playbackRate + "×")
        }
        onPressedChanged: {
            if (!pressed && boosting) {
                if (root.playerItem) root.playerItem.playbackRate = previousRate
                boosting = false
            }
        }
    }
}

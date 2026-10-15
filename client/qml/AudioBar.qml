import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 底部迷你播放条：音声播放时在所有页面显示。点标题回到音声页。
Rectangle {
    id: bar
    required property var engine
    signal openRequested()
    height: 52
    color: "#161b22"
    Rectangle { anchors.top: parent.top; width: parent.width; height: 1; color: "#30363d" }

    readonly property var mpv: engine.player

    RowLayout {
        anchors.fill: parent
        anchors.leftMargin: 8; anchors.rightMargin: 8
        spacing: 4

        // 标题区：点击打开音声页
        Item {
            Layout.preferredWidth: 240
            Layout.fillHeight: true
            Column {
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width
                Label {
                    width: parent.width; elide: Text.ElideRight
                    text: bar.engine.track ? bar.engine.track.title : ""
                    color: "#e6edf3"; font.pixelSize: 13
                }
                Label {
                    width: parent.width; elide: Text.ElideRight
                    text: bar.engine.error || (bar.engine.albumTitle + "（" + (bar.engine.position + 1) + "/" + bar.engine.total + "）")
                    color: bar.engine.error ? "#f85149" : "#8b949e"; font.pixelSize: 11
                }
            }
            TapHandler { onTapped: bar.openRequested() }
            HoverHandler { cursorShape: Qt.PointingHandCursor }
        }

        IconButton { glyph: "prev"; size: 32; onClicked: bar.engine.previousMedia(); ToolTip.text: "上一集" }
        IconButton { glyph: "replay_10"; size: 32; onClicked: bar.engine.seekBy(-bar.engine.skipBack); ToolTip.text: "后退 10 秒" }
        IconButton { glyph: bar.mpv.playing ? "pause" : "play"; size: 40; onClicked: bar.engine.togglePlay() }
        IconButton { glyph: "forward_30"; size: 32; onClicked: bar.engine.seekBy(bar.engine.skipForward); ToolTip.text: "前进 30 秒" }
        IconButton { glyph: "next"; size: 32; enabled: bar.engine.hasNext; onClicked: bar.engine.nextMedia(); ToolTip.text: "下一集" }

        Label { text: bar.engine.formatTime(bar.mpv.position); color: "#adbac7"; font.pixelSize: 12; font.family: "monospace" }
        SeekBar {
            Layout.fillWidth: true
            from: 0; to: Math.max(1, bar.mpv.duration); value: bar.mpv.position
            enabled: bar.mpv.duration > 0
            onMoved: bar.mpv.position = value
        }
        Label { text: bar.engine.formatTime(bar.mpv.duration); color: "#adbac7"; font.pixelSize: 12; font.family: "monospace" }

        Chip { text: bar.engine.rate + "×"; onClicked: bar.engine.cycleRate(); ToolTip.visible: hovered; ToolTip.text: "倍速" }
        IconButton {
            size: 32
            glyph: ({list: "repeat", single: "repeat_one", random: "shuffle"})[bar.engine.mode]
            onClicked: bar.engine.cycleMode()
            ToolTip.text: ({list: "列表循环", single: "单集循环", random: "随机"})[bar.engine.mode]
        }
        IconButton {
            size: 32; glyph: "bedtime"; checked: bar.engine.sleepMinutes > 0
            onClicked: bar.engine.cycleSleep()
            ToolTip.text: bar.engine.sleepMinutes > 0 ? bar.engine.sleepMinutes + " 分钟后暂停（再点切换）" : "定时关闭"
        }
        Label { visible: bar.engine.sleepMinutes > 0; text: bar.engine.sleepMinutes + "′"; color: "#58a6ff"; font.pixelSize: 12 }
        IconButton { glyph: "close"; size: 28; onClicked: bar.engine.stop(); ToolTip.text: "停止并收起" }
    }
}

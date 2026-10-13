import QtQuick 2.15
import QtQuick.Controls 2.15
import omniHub.Games 1.0

Item {
    id: gameLauncherView

    GameLauncher {
        id: launcher
        Component.onCompleted: fetchGames()
    }

    ListView {
        anchors.fill: parent
        anchors.margins: 20
        spacing: 12
        model: launcher.games

        delegate: Rectangle {
            width: parent.width
            height: 70
            color: "#161b22"
            radius: 8
            border.color: "#30363d"

            Row {
                anchors.fill: parent
                anchors.margins: 12
                spacing: 16

                Column {
                    width: parent.width - 150
                    Text {
                        text: modelData.name || modelData.id
                        color: "#e6edf3"
                        font.bold: true
                        font.pixelSize: 15
                    }
                    Text {
                        text: "分类: " + (modelData.category || "PC") + " | 游玩时长: " + Math.round((modelData.play_time_sec || 0)/60) + " 分钟"
                        color: "#8b949e"
                        font.pixelSize: 12
                    }
                }

                Button {
                    text: "启动游戏"
                    anchors.verticalCenter: parent.verticalCenter
                    onClicked: launcher.launchGame(modelData.id, modelData.exe, modelData.args || "")
                }
            }
        }
    }
}

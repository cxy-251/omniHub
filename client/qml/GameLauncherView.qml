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
                    text: (modelData.name.indexOf("StarCraft") !== -1) ? "对战配置…" : "启动游戏"
                    anchors.verticalCenter: parent.verticalCenter
                    onClicked: {
                        if (modelData.name.indexOf("StarCraft") !== -1) {
                            sc2Dialog.open()
                        } else {
                            launcher.launchGame(modelData.id, modelData.exe, modelData.args || "")
                        }
                    }
                }
            }
        }
    }

    // 星际争霸 2 定制对战开局弹窗 (第 10.1 节)
    Dialog {
        id: sc2Dialog
        anchors.centerIn: parent
        width: 400
        height: 320
        title: "星际争霸 2 离线对战配置"
        standardButtons: Dialog.Ok | Dialog.Cancel

        Column {
            anchors.fill: parent
            spacing: 12

            Text { text: "AI 种族:"; color: "#e6edf3" }
            ComboBox {
                id: raceCombo
                width: parent.width
                model: ["Random", "Terran", "Zerg", "Protoss"]
            }

            Text { text: "AI 难度:"; color: "#e6edf3" }
            ComboBox {
                id: diffCombo
                width: parent.width
                model: ["VeryEasy", "Easy", "Medium", "Hard", "VeryHard", "CheatInsane"]
                currentIndex: 2
            }
        }

        onAccepted: {
            var args = "--race " + raceCombo.currentText + " --difficulty " + diffCombo.currentText;
            launcher.launchGame("sc2", "wine_games/StarCraft II/Support64/SC2Switcher_x64.exe", args);
        }
    }
}

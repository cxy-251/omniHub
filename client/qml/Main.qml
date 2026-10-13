import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Window 2.15

Window {
    id: mainWindow
    width: 1280
    height: 800
    visible: true
    title: "omniHub Native Client"
    color: "#0d1117"

    Column {
        anchors.fill: parent

        // 顶栏导航
        Rectangle {
            width: parent.width
            height: 48
            color: "#161b22"
            border.color: "#30363d"

            Row {
                anchors.centerIn: parent
                spacing: 20

                Button {
                    text: "通用播放器"
                    highlighted: stack.currentIndex === 0
                    onClicked: stack.currentIndex = 0
                }
                Button {
                    text: "独立游戏大厅"
                    highlighted: stack.currentIndex === 1
                    onClicked: stack.currentIndex = 1
                }
            }
        }

        // 视窗内容切换
        StackLayout {
            id: stack
            width: parent.width
            height: parent.height - 48
            currentIndex: initialExternalFile !== "" ? 0 : 1

            PlayerView {
                id: player
                source: initialExternalFile
            }

            GameLauncherView {
                id: gameLauncher
            }
        }
    }
}

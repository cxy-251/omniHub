import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Window 2.15
import QtQuick.Layouts 1.15

Window {
    id: mainWindow
    width: 1280
    height: 800
    visible: true
    title: "omniHub Native Client"
    color: "#0d1117"

    Component.onCompleted: backendApi.refresh()
    Connections {
        target: backendApi
        function onStatusChanged() {
            if (backendApi.connected && !backendApi.hasAdmin) setupDialog.open()
            if (backendApi.hasAdmin) setupDialog.close()
        }
    }
    Dialog {
        id: setupDialog
        anchors.centerIn: parent
        modal: true
        closePolicy: Popup.NoAutoClose
        title: "创建管理员账号"
        Column {
            spacing: 12
            TextField { id: adminName; placeholderText: "管理员用户名"; maximumLength: 64 }
            TextField { id: adminPassword; placeholderText: "管理员密码"; echoMode: TextInput.Password; maximumLength: 72 }
            Label { text: backendApi.error; color: "#f85149"; wrapMode: Text.Wrap; width: 320 }
            Button { text: "完成设置"; enabled: adminName.text.length > 0 && adminPassword.text.length > 0; onClicked: backendApi.setupAdmin(adminName.text, adminPassword.text) }
        }
    }

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

import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import omniHub.Games 1.0

Item {
    GameLauncher {
        id: launcher
        Component.onCompleted: {
            if (backendApi.hasAdmin) { fetchGames(); refreshSession() }
        }
    }
    Connections {
        target: backendApi
        function onStatusChanged() {
            if (backendApi.hasAdmin) { launcher.fetchGames(); launcher.refreshSession() }
        }
    }
    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 20
        spacing: 12
        Label {
            Layout.fillWidth: true
            visible: launcher.error.length > 0
            text: launcher.error
            textFormat: Text.PlainText
            wrapMode: Text.Wrap
            color: "#f85149"
        }
        RowLayout {
            Layout.fillWidth: true
            Label {
                Layout.fillWidth: true
                text: launcher.gameRunning ? "游戏运行中：" + (launcher.session.key || "") : "游戏大厅"
                textFormat: Text.PlainText
                color: "#e6edf3"
            }
            Button { text: "停止游戏"; visible: launcher.gameRunning; enabled: !launcher.busy; onClicked: launcher.stopGame() }
            Button { text: "刷新"; enabled: !launcher.loading; onClicked: { launcher.fetchGames(launcher.offset); launcher.refreshSession() } }
        }
        Label {
            Layout.fillWidth: true
            visible: !!launcher.session.warning || !!launcher.session.exit_error
            text: launcher.session.warning || launcher.session.exit_error || ""
            textFormat: Text.PlainText
            wrapMode: Text.Wrap
            color: "#d29922"
        }
        ListView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: 12
            model: launcher.games
            delegate: Rectangle {
                width: ListView.view.width
                height: 80
                color: "#161b22"
                radius: 8
                border.color: "#30363d"
                RowLayout {
                    anchors.fill: parent
                    anchors.margins: 12
                    spacing: 16
                    ColumnLayout {
                        Layout.fillWidth: true
                        Label {
                            Layout.fillWidth: true
                            text: modelData.name || modelData.id
                            textFormat: Text.PlainText
                            elide: Text.ElideRight
                            color: "#e6edf3"
                            font.bold: true
                        }
                        Label {
                            Layout.fillWidth: true
                            text: (modelData.library_name || "") + " · " + modelData.category + " · " + Math.round((modelData.play_time_sec || 0) / 60) + " 分钟"
                            textFormat: Text.PlainText
                            elide: Text.ElideRight
                            color: "#8b949e"
                        }
                    }
                    Button {
                        text: modelData.read_only ? "只读库" : "启动游戏"
                        enabled: !modelData.read_only && !launcher.busy && !launcher.gameRunning
                        onClicked: launcher.launchGame(modelData.library_id, modelData.id)
                    }
                }
            }
        }
        RowLayout {
            Layout.alignment: Qt.AlignHCenter
            Button { text: "上一页"; enabled: !launcher.loading && launcher.offset > 0; onClicked: launcher.fetchGames(Math.max(0, launcher.offset - 24)) }
            Label { text: launcher.loading ? "加载中…" : "第 " + (Math.floor(launcher.offset / 24) + 1) + " 页"; color: "#8b949e" }
            Button { text: "下一页"; enabled: !launcher.loading && launcher.hasMore; onClicked: launcher.fetchGames(launcher.offset + 24) }
        }
    }
}

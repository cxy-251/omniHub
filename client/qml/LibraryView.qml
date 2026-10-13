import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

ScrollView {
    id: libraryView
    clip: true
    Connections {
        target: backendApi
        function onCategoryDirectoriesChanged() { categoryInput.text = backendApi.categoryDirectories.join("\n") }
    }
    ColumnLayout {
        width: libraryView.availableWidth
        spacing: 14
        Label { text: "资源库与全局状态"; font.pixelSize: 24; color: "#c9d1d9"; Layout.margins: 16 }
        Label {
            text: backendApi.stateReady ? "账号服务可用" : "全局状态离线：请重新接入原库，然后重新检测。不会创建替代账号库。"
            color: backendApi.stateReady ? "#3fb950" : "#f85149"
            wrapMode: Text.Wrap; Layout.fillWidth: true; Layout.leftMargin: 16; Layout.rightMargin: 16
        }
        Label { text: backendApi.stateDirectory; color: "#8b949e"; wrapMode: Text.WrapAnywhere; Layout.fillWidth: true; Layout.leftMargin: 16; Layout.rightMargin: 16 }
        Label { text: backendApi.libraryError || backendApi.error; visible: text.length > 0; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true; Layout.leftMargin: 16; Layout.rightMargin: 16 }
        Button { text: "重新检测与恢复"; enabled: !backendApi.libraryBusy; onClicked: backendApi.recoverLibraries(); Layout.leftMargin: 16 }
        Label { text: "配置切换会暂停当前播放；请先停止资源任务。新增库不会自动迁移账号。"; color: "#8b949e"; wrapMode: Text.Wrap; Layout.fillWidth: true; Layout.leftMargin: 16; Layout.rightMargin: 16 }
        RowLayout {
            Layout.fillWidth: true; Layout.margins: 16
            TextField { id: libraryPath; placeholderText: "服务端物理资源库的绝对路径"; Layout.fillWidth: true; maximumLength: 4096 }
            CheckBox { id: libraryReadOnly; text: "只读" }
            Button { text: "添加"; enabled: !backendApi.libraryBusy && libraryPath.text.length > 0; onClicked: backendApi.addLibrary(libraryPath.text, libraryReadOnly.checked) }
        }
        Repeater {
            model: backendApi.libraries
            delegate: Frame {
                required property var modelData
                Layout.fillWidth: true; Layout.leftMargin: 16; Layout.rightMargin: 16
                ColumnLayout {
                    anchors.fill: parent
                    Label { text: modelData.name + (modelData.is_online ? " · 在线" : " · 离线") + (modelData.is_primary ? " · 主库" : "") + (modelData.read_only ? " · 只读" : ""); color: "#c9d1d9" }
                    Label { text: modelData.root_path; color: "#8b949e"; wrapMode: Text.WrapAnywhere; Layout.fillWidth: true }
                    RowLayout {
                        Button { text: "设为主库"; enabled: !backendApi.libraryBusy && backendApi.stateReady && modelData.is_online && !modelData.read_only; onClicked: backendApi.previewPrimary(modelData.id) }
                        Button { text: "移除挂载"; enabled: !backendApi.libraryBusy; onClicked: { removeDialog.libraryId = modelData.id; removeDialog.libraryName = modelData.name; removeDialog.open() } }
                    }
                }
            }
        }
        Frame {
            visible: backendApi.migrationPreview.confirmation_token !== undefined
            Layout.fillWidth: true; Layout.margins: 16
            ColumnLayout {
                anchors.fill: parent
                Label { text: "确认迁移全局数据库"; font.bold: true; color: "#c9d1d9" }
                Label { text: backendApi.migrationPreview.message || ""; wrapMode: Text.Wrap; Layout.fillWidth: true; color: "#c9d1d9" }
                Label { text: "从：" + (backendApi.migrationPreview.source || "") + "\n到：" + (backendApi.migrationPreview.destination || ""); wrapMode: Text.WrapAnywhere; Layout.fillWidth: true; color: "#8b949e" }
                RowLayout {
                    Button { text: "确认迁移"; enabled: !backendApi.libraryBusy; onClicked: backendApi.confirmPrimary() }
                    Button { text: "取消"; enabled: !backendApi.libraryBusy; onClicked: backendApi.cancelMigration() }
                }
            }
        }
        BusyIndicator { running: backendApi.libraryBusy; visible: running; Layout.alignment: Qt.AlignHCenter }
        Frame {
            Layout.fillWidth: true; Layout.margins: 16
            ColumnLayout {
                anchors.fill: parent
                Label { text: "启用的分类目录"; font.bold: true; color: "#c9d1d9" }
                Label { text: "每行一个分类，如 media_library/audio。新增目录仅在在线可写库中补建；移除分类会停用访问并保留文件。空清单停用全部分类。"; color: "#8b949e"; wrapMode: Text.Wrap; Layout.fillWidth: true }
                TextArea {
                    id: categoryInput
                    Layout.fillWidth: true
                    Layout.preferredHeight: 220
                    wrapMode: TextEdit.NoWrap
                    selectByMouse: true
                    Component.onCompleted: text = backendApi.categoryDirectories.join("\n")
                }
                Button {
                    text: "保存分类配置"
                    enabled: !backendApi.libraryBusy
                    onClicked: backendApi.saveCategories(categoryInput.text.split("\n").map(function (dir) { return dir.trim() }).filter(function (dir) { return dir.length > 0 }))
                }
            }
        }
    }
    Dialog {
        id: removeDialog
        property string libraryId
        property string libraryName
        anchors.centerIn: parent
        title: "移除 " + libraryName + " 的挂载？"
        modal: true
        standardButtons: Dialog.Ok | Dialog.Cancel
        Label { width: 360; text: "文件保留原位。全局状态仍位于原目录；拔出其所在介质会使账号服务离线。"; wrapMode: Text.Wrap }
        onAccepted: backendApi.removeLibrary(libraryId)
    }
}

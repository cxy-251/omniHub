import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs

ScrollView {
    id: libraryView
    clip: true
    // 全部已知分类 = 启用 + 停用，按名称排序，用于开关列表。
    readonly property var allCategories: backendApi.categoryDirectories.concat(backendApi.disabledCategories).filter(function (d, i, a) { return a.indexOf(d) === i }).sort()
    FolderDialog {
        id: folderDialog
        title: "选择资源库根目录"
        onAccepted: libraryPath.text = decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))
    }
    ColumnLayout {
        width: libraryView.availableWidth
        spacing: 14
        Label { text: "资源库与全局状态"; font.pixelSize: 24; color: "#c9d1d9"; Layout.margins: 16 }
        // 局域网访问：关闭后手机、电脑等其它设备连不上，只有本机能用（立即生效，不用重启）。
        RowLayout {
            Layout.leftMargin: 16; Layout.rightMargin: 16
            spacing: 8
            ApiClient { id: lanApi }
            Toggle {
                id: lanSwitch
                text: "允许局域网访问"
                property bool loaded: false
                Component.onCompleted: lanApi.getJSON("/api/network/lan", function (r) { lanSwitch.checked = r.enabled; lanSwitch.loaded = true })
                onToggled: lanApi.send("POST", "/api/network/lan", {enabled: checked}, function (r) { lanSwitch.checked = r.enabled })
                enabled: loaded
            }
            Label {
                text: lanApi.error || (lanSwitch.checked ? "手机、电脑可以通过局域网访问（omnihub.local）" : "只有这台 Deck 能访问，局域网设备会被拒绝")
                color: lanApi.error ? "#f85149" : "#8b949e"; font.pixelSize: 12
            }
        }
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
            TextField { id: libraryPath; placeholderText: "点「浏览」选择资源库根目录"; Layout.fillWidth: true; maximumLength: 4096 }
            Button { text: "浏览…"; onClicked: folderDialog.open() }
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
                Label { text: "分类目录"; font.bold: true; color: "#c9d1d9" }
                Label { text: "关闭即停用：停止扫描和展示，文件保持原样，随时可重新开启。"; color: "#8b949e"; wrapMode: Text.Wrap; Layout.fillWidth: true }
                GridLayout {
                    columns: 2
                    Layout.fillWidth: true
                    Repeater {
                        model: libraryView.allCategories
                        delegate: Toggle {
                            required property string modelData
                            text: modelData
                            enabled: !backendApi.libraryBusy
                            checked: backendApi.categoryDirectories.indexOf(modelData) >= 0
                            onToggled: {
                                var active = backendApi.categoryDirectories.filter(function (d) { return d !== modelData })
                                if (checked) active.push(modelData)
                                backendApi.saveCategories(active)
                            }
                        }
                    }
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

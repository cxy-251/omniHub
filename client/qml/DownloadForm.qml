import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 通用下载表单：按服务端声明的字段生成（lines 多行 / text / toggle / choice），默认值预先填好；
// 不常用的项收在「更多选项」里。上方是这类任务的预设（一键运行），下方是「开始」与「存为预设」。
ColumnLayout {
    id: form
    required property var kind          // /api/downloads/kinds 的一项
    property var presets: []            // 本类预设
    property var values: ({})
    property bool showAdvanced: false
    signal run(var params)
    signal runPreset(var preset)
    signal savePreset(string label, var params)
    signal deletePreset(var preset)
    spacing: 8

    function reset() {
        var v = {}
        kind.fields.forEach(function (f) { v[f.name] = f.default !== undefined ? f.default : (f.type === "toggle" ? false : "") })
        values = v
    }
    function set(name, value) { var v = Object.assign({}, values); v[name] = value; values = v }
    readonly property bool ready: kind.fields.every(function (f) { return !f.required || String(values[f.name] || "").trim() !== "" }) && !(kind.missing && kind.missing.length)
    onKindChanged: reset()
    Component.onCompleted: reset()

    Label { text: form.kind.description; color: "#8b949e"; font.pixelSize: 12; wrapMode: Text.Wrap; Layout.fillWidth: true }
    Label {
        visible: !!(form.kind.missing && form.kind.missing.length)
        text: "本机缺少：" + (form.kind.missing || []).join("、") + "，装好后才能用"
        color: "#f85149"; font.pixelSize: 12
    }

    // 预设：一键运行
    Flow {
        Layout.fillWidth: true
        spacing: 6
        visible: form.presets.length > 0
        Repeater {
            model: form.presets
            delegate: Rectangle {
                required property var modelData
                height: 30; radius: 15
                width: presetRow.implicitWidth + 16
                color: "#161b22"; border.color: "#30363d"
                Row {
                    id: presetRow
                    anchors.centerIn: parent
                    spacing: 6
                    Label { text: "▶ " + modelData.label; color: "#e6edf3"; font.pixelSize: 12; anchors.verticalCenter: parent.verticalCenter
                        TapHandler { onTapped: form.runPreset(modelData) }
                        HoverHandler { cursorShape: Qt.PointingHandCursor }
                    }
                    Label { text: "✕"; color: "#6e7681"; font.pixelSize: 11; anchors.verticalCenter: parent.verticalCenter
                        TapHandler { onTapped: form.deletePreset(modelData) }
                        HoverHandler { cursorShape: Qt.PointingHandCursor }
                    }
                }
            }
        }
    }

    Repeater {
        model: form.kind.fields
        delegate: ColumnLayout {
            id: fieldBox
            required property var modelData
            Layout.fillWidth: true
            visible: !modelData.advanced || form.showAdvanced
            spacing: 2
            Label {
                visible: modelData.type !== "toggle"
                text: modelData.label + (modelData.required ? "" : "（可选）")
                color: "#c9d1d9"; font.pixelSize: 12
            }
            // 按字段类型只显示对应的那个控件。
            ScrollView {
                visible: fieldBox.modelData.type === "lines"
                Layout.fillWidth: true
                implicitHeight: 96
                TextArea {
                    placeholderText: fieldBox.modelData.placeholder || ""
                    text: form.values[fieldBox.modelData.name] || ""
                    wrapMode: TextEdit.WrapAnywhere
                    font.pixelSize: 13
                    onTextChanged: if (fieldBox.modelData.type === "lines" && text !== (form.values[fieldBox.modelData.name] || "")) form.set(fieldBox.modelData.name, text)
                    background: Rectangle { color: "#0d1117"; border.color: parent.activeFocus ? "#58a6ff" : "#30363d"; radius: 4 }
                }
            }
            TextField {
                visible: fieldBox.modelData.type === "text"
                Layout.fillWidth: true
                implicitHeight: 30
                placeholderText: fieldBox.modelData.placeholder || ""
                text: form.values[fieldBox.modelData.name] !== undefined ? String(form.values[fieldBox.modelData.name]) : ""
                font.pixelSize: 13
                onTextEdited: form.set(fieldBox.modelData.name, text)
            }
            CheckBox {
                visible: fieldBox.modelData.type === "toggle"
                text: fieldBox.modelData.label
                checked: !!form.values[fieldBox.modelData.name]
                onToggled: form.set(fieldBox.modelData.name, checked)
            }
            ComboBox {
                visible: fieldBox.modelData.type === "choice"
                Layout.preferredWidth: 280
                implicitHeight: 30
                font.pixelSize: 13
                textRole: "label"; valueRole: "value"
                model: fieldBox.modelData.options || []
                currentIndex: Math.max(0, indexOfValue(form.values[fieldBox.modelData.name]))
                onActivated: form.set(fieldBox.modelData.name, currentValue)
            }
            Label { visible: !!modelData.help; text: modelData.help || ""; color: "#6e7681"; font.pixelSize: 11 }
        }
    }

    RowLayout {
        spacing: 6
        Chip {
            visible: form.kind.fields.some(function (f) { return f.advanced })
            text: form.showAdvanced ? "收起更多选项" : "更多选项"
            onClicked: form.showAdvanced = !form.showAdvanced
        }
        Item { Layout.fillWidth: true }
        TextField { id: presetName; implicitHeight: 30; placeholderText: "预设名称"; Layout.preferredWidth: 160; visible: savePresetChip.armed }
        Chip {
            id: savePresetChip
            property bool armed: false
            text: armed ? "保存" : "存为预设"
            enabled: form.ready && (!armed || presetName.text.trim() !== "")
            onClicked: {
                if (!armed) { armed = true; presetName.forceActiveFocus(); return }
                form.savePreset(presetName.text.trim(), form.values)
                presetName.text = ""; armed = false
            }
        }
        Button { text: "开始"; highlighted: true; enabled: form.ready; onClicked: form.run(form.values) }
    }
}

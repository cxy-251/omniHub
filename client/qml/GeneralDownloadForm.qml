import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 通用下载的表单：一行流程说明 → 链接 → 一行选项 → 每个链接会怎样（平台、工具、登录、去向）→ 后果说明与「开始」。
// 预览由服务端按域名算（/api/downloads/analyze，不联网），与实际下载用同一张平台表。
ColumnLayout {
    id: form
    required property var kind
    property var presets: []
    property var values: ({urls: "", mode: "", max_height: "", items: "", library: ""})
    property var analysis: ({items: []})
    signal run(var params)
    signal runPreset(var preset)
    signal savePreset(string label, var params)
    signal deletePreset(var preset)
    signal openTab(string tab)
    spacing: 8

    ApiClient { id: api }
    function field(name) { return form.kind.fields.find(function (f) { return f.name === name }) || {options: []} }
    function set(name, value) { var v = Object.assign({}, values); v[name] = value; values = v; analyzeTimer.restart() }
    readonly property var validItems: analysis.items.filter(function (i) { return i.valid })
    readonly property bool ready: validItems.length > 0 && validItems.length === analysis.items.length && !(kind.missing && kind.missing.length)
    // 任务的字段（选项与默认值）从服务端来，可能晚于本表单创建：到了再填默认值。
    function applyDefaults() {
        var v = Object.assign({}, values)
        ;["mode", "max_height", "library"].forEach(function (n) { var f = field(n); if (f.default !== undefined && !v[n]) v[n] = f.default })
        values = v
        analyzeTimer.restart()
    }
    onKindChanged: applyDefaults()
    Component.onCompleted: applyDefaults()
    function optionIndex(name) {
        var opts = field(name).options || []
        return Math.max(0, opts.findIndex(function (o) { return o.value === form.values[name] }))
    }
    Timer { id: analyzeTimer; interval: 300; onTriggered: api.send("POST", "/api/downloads/analyze", form.values, function (r) { form.analysis = r }) }

    // 流程：一行说清楚会发生什么
    Flow {
        Layout.fillWidth: true
        spacing: 4
        Repeater {
            model: ["① 粘贴链接", "② 下载到暂存区", "③ 在「暂存区」预览、删掉不要的", "④ 移入资源库"]
            delegate: Row {
                required property string modelData
                required property int index
                spacing: 4
                Rectangle {
                    height: 24; width: stepLabel.implicitWidth + 16; radius: 12
                    color: index === 1 ? "#1f6feb33" : "#161b22"; border.color: index === 1 ? "#58a6ff" : "#30363d"
                    Label { id: stepLabel; anchors.centerIn: parent; text: modelData; color: "#c9d1d9"; font.pixelSize: 12 }
                    TapHandler { enabled: index >= 2; onTapped: form.openTab("staging") }
                }
                Label { visible: index < 3; text: "→"; color: "#6e7681"; anchors.verticalCenter: parent.verticalCenter }
            }
        }
    }
    Label {
        visible: !!(form.kind.missing && form.kind.missing.length)
        text: "本机缺少：" + (form.kind.missing || []).join("、") + "，装好后才能用"
        color: "#f85149"; font.pixelSize: 12
    }

    // 预设：一键填好并开始
    Flow {
        Layout.fillWidth: true
        spacing: 6
        visible: form.presets.length > 0
        Repeater {
            model: form.presets
            delegate: Chip {
                required property var modelData
                text: "▶ " + modelData.label
                onClicked: form.runPreset(modelData)
                ToolTip.text: "右键删除这个预设"
                TapHandler { acceptedButtons: Qt.RightButton; onTapped: form.deletePreset(modelData) }
            }
        }
    }

    ScrollView {
        Layout.fillWidth: true
        implicitHeight: 110
        TextArea {
            placeholderText: "粘贴链接，一行一个：视频、播放列表、频道、合集、用户主页……\n例如 https://www.youtube.com/watch?v=…   https://www.bilibili.com/video/BV…   https://x.com/<用户>/media"
            text: form.values.urls
            wrapMode: TextEdit.WrapAnywhere
            font.pixelSize: 13
            onTextChanged: if (text !== form.values.urls) form.set("urls", text)
            background: Rectangle { color: "#0d1117"; border.color: parent.activeFocus ? "#58a6ff" : "#30363d"; radius: 4 }
        }
    }

    // 选项：一行
    RowLayout {
        Layout.fillWidth: true
        spacing: 6
        Chip { text: "视频"; selected: form.values.mode === "video"; onClicked: form.set("mode", "video") }
        Chip { text: "只要音频（音乐）"; selected: form.values.mode === "audio"; onClicked: form.set("mode", "audio") }
        Rectangle { width: 1; height: 20; color: "#30363d" }
        Label { visible: form.values.mode === "video"; text: "最高"; color: "#8b949e"; font.pixelSize: 12 }
        ComboBox {
            visible: form.values.mode === "video"
            implicitHeight: 30; Layout.preferredWidth: 100; font.pixelSize: 12
            textRole: "label"; valueRole: "value"
            model: form.field("max_height").options
            currentIndex: form.optionIndex("max_height")
            onActivated: form.set("max_height", currentValue)
        }
        TextField {
            implicitHeight: 30; Layout.preferredWidth: 150; font.pixelSize: 12
            placeholderText: "只下序号，如 1-20"
            text: form.values.items
            onTextEdited: form.set("items", text)
            ToolTip.visible: hovered; ToolTip.text: "播放列表、合集、用户页时只下这些序号；留空全下"
        }
        Item { Layout.fillWidth: true }
        Label { text: "暂存到"; color: "#8b949e"; font.pixelSize: 12 }
        ComboBox {
            implicitHeight: 30; Layout.preferredWidth: 130; font.pixelSize: 12
            textRole: "label"; valueRole: "value"
            model: form.field("library").options
            currentIndex: form.optionIndex("library")
            onActivated: form.set("library", currentValue)
        }
    }

    // 每个链接会怎样
    Repeater {
        model: form.analysis.items
        delegate: RowLayout {
            required property var modelData
            Layout.fillWidth: true
            spacing: 6
            Label {
                text: modelData.valid ? modelData.platform : "✗"
                color: modelData.valid ? "#e6edf3" : "#f85149"; font.pixelSize: 12; font.bold: true
                Layout.preferredWidth: 90; elide: Text.ElideRight
            }
            Label { visible: modelData.valid; text: modelData.tool; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 64 }
            Label {
                visible: modelData.valid
                Layout.preferredWidth: 120
                font.pixelSize: 12
                text: ({logged_in: "✓ 已登录", no_login: "⚠ Cookie 里没有登录", none: "⚠ 没读过 Cookie", unknown_site: "不带登录"})[modelData.cookie] || ""
                color: modelData.cookie === "logged_in" ? "#3fb950" : modelData.cookie === "unknown_site" ? "#8b949e" : "#e3b341"
                TapHandler { enabled: modelData.cookie === "none" || modelData.cookie === "no_login"; onTapped: form.openTab("cookies") }
                HoverHandler { cursorShape: (modelData.cookie === "none" || modelData.cookie === "no_login") ? Qt.PointingHandCursor : Qt.ArrowCursor }
            }
            Label { Layout.fillWidth: true; text: modelData.target; color: "#c9d1d9"; font.pixelSize: 12; elide: Text.ElideRight }
        }
    }

    // 后果与开始
    RowLayout {
        Layout.fillWidth: true
        spacing: 8
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
            text: form.analysis.items.length === 0 ? "粘贴链接后，这里会列出每个链接用什么工具、带不带登录、下到哪里。"
                : (form.validItems.length < form.analysis.items.length ? "有不是网址的行，删掉后才能开始。"
                   : form.validItems.length + " 个链接逐个下载到「" + (form.analysis.library ? form.analysis.library.name : "") + "」的暂存区（新批次），不动资源库里已有的内容。")
                  + (form.analysis.items.some(function (i) { return i.cookie === "none" || i.cookie === "no_login" }) ? "没登录的平台可能只能下公开内容，点黄色提示去读 Cookie。" : "")
        }
        Chip {
            id: savePresetChip
            property bool armed: false
            visible: form.ready
            text: armed ? "保存预设" : "存为预设"
            enabled: !armed || presetName.text.trim() !== ""
            onClicked: {
                if (!armed) { armed = true; presetName.forceActiveFocus(); return }
                form.savePreset(presetName.text.trim(), form.values)
                presetName.text = ""; armed = false
            }
        }
        TextField { id: presetName; visible: savePresetChip.armed; implicitHeight: 30; Layout.preferredWidth: 140; placeholderText: "预设名称" }
        PrimaryButton { text: "开始下载到暂存区"; enabled: form.ready; onClicked: form.run(form.values) }
    }
}

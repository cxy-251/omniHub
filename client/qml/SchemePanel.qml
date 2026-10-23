import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 下载方案页（音乐、YouTube、B 站、X、其他链接）：按服务端给的方案说明生成。
//   去向 → 链接（一行一个）→ 选项 →（列表类方案）怎么挑 → 开始。设好就不用管，进度在「任务」里。
// 列表类方案每个链接只读一次列表页，音乐按 Last.fm 重播率、视频按播放量排名取前 N 个，不逐个打开视频页。
ColumnLayout {
    id: panel
    required property var kind
    signal run(var params)
    signal openTab(string tab)
    spacing: 8

    ApiClient { id: api }
    readonly property var scheme: Object.assign({steps: [], destination: "", input: "", placeholder: "", ranked: false}, kind.scheme || {})
    readonly property var rankFields: ["rank", "top"]
    property var values: ({})
    property string input: ""
    property bool showHelp: false
    property var analysis: ({items: []})

    function field(name) { return kind.fields.find(function (f) { return f.name === name }) || null }
    function set(name, value) { var v = Object.assign({}, values); v[name] = value; values = v }
    function optionLabel(name) { var f = field(name); var o = f ? f.options.find(function (x) { return x.value === values[name] }) : null; return o ? o.label : "" }
    function reset() {
        var v = {}
        kind.fields.forEach(function (f) { if (f.default !== undefined) v[f.name] = f.default })
        values = v; input = ""; analysis = {items: []}
    }
    onKindChanged: reset()
    Component.onCompleted: reset()
    readonly property var lines: input.split("\n").map(function (s) { return s.trim() }).filter(function (s) { return s !== "" })
    readonly property bool ready: lines.length > 0 && !(kind.missing && kind.missing.length)
    function start() {
        var p = Object.assign({}, values)
        p.mode = "direct"
        p.urls = lines.join("\n")
        panel.run(p)
    }
    Timer { id: analyzeTimer; interval: 300; onTriggered: if (panel.kind.id === "other") api.send("POST", "/api/downloads/analyze", {urls: panel.input, mode: "video", library: panel.values.library || ""}, function (r) { panel.analysis = r }) }

    // 去向 + 说明（默认收起）
    RowLayout {
        Layout.fillWidth: true
        spacing: 6
        Label { text: "放到"; color: "#8b949e"; font.pixelSize: 12 }
        Label { text: panel.scheme.destination; color: "#3fb950"; font.pixelSize: 14; font.bold: true }
        Label { text: "（先进暂存区，确认后再移入）"; color: "#6e7681"; font.pixelSize: 12 }
        Item { Layout.fillWidth: true }
        Chip { text: panel.showHelp ? "收起说明" : "怎么用？"; selected: panel.showHelp; onClicked: panel.showHelp = !panel.showHelp }
    }
    Label {
        visible: panel.showHelp
        Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#adbac7"
        text: panel.kind.description + "\n步骤：" + panel.scheme.steps.join(" → ")
    }

    // 链接（一行一个）
    ScrollView {
        Layout.fillWidth: true
        implicitHeight: 110
        TextArea {
            placeholderText: panel.scheme.input + "\n" + panel.scheme.placeholder
            text: panel.input
            wrapMode: TextEdit.WrapAnywhere; font.pixelSize: 13
            onTextChanged: if (text !== panel.input) { panel.input = text; analyzeTimer.restart() }
            background: Rectangle { color: "#0d1117"; border.color: parent.activeFocus ? "#58a6ff" : "#30363d"; radius: 4 }
        }
    }

    // 选项（放不下自动换行）：怎么挑的两项放在最前面
    Flow {
        Layout.fillWidth: true
        spacing: 8
        Repeater {
            model: panel.kind.fields.filter(function (f) { return f.name !== "urls" && (f.name !== "top" || panel.values.rank !== "none") })
                                    .sort(function (a, b) { return (panel.rankFields.indexOf(b.name) >= 0) - (panel.rankFields.indexOf(a.name) >= 0) })
            delegate: Row {
                required property var modelData
                spacing: 4
                Label { text: modelData.label; color: "#8b949e"; font.pixelSize: 12; anchors.verticalCenter: parent.verticalCenter }
                ComboBox {
                    visible: modelData.type === "choice"
                    implicitHeight: 30; width: modelData.name === "rank" ? 150 : 120; font.pixelSize: 12
                    textRole: "label"; valueRole: "value"
                    model: modelData.options || []
                    currentIndex: Math.max(0, (modelData.options || []).findIndex(function (o) { return o.value === panel.values[modelData.name] }))
                    onActivated: panel.set(modelData.name, currentValue)
                }
                TextField {
                    visible: modelData.type === "text"
                    implicitHeight: 30; width: 200; font.pixelSize: 12
                    placeholderText: modelData.help || ""
                    text: panel.values[modelData.name] || ""
                    onTextEdited: panel.set(modelData.name, text)
                }
            }
        }
    }

    // 其他链接：每个链接认成什么平台
    Repeater {
        model: panel.kind.id === "other" ? panel.analysis.items : []
        delegate: Label {
            required property var modelData
            Layout.fillWidth: true; elide: Text.ElideRight; font.pixelSize: 12
            color: modelData.valid ? "#c9d1d9" : "#f85149"
            text: modelData.valid ? modelData.platform + " · " + modelData.target : "✗ " + modelData.url + "：" + modelData.target
        }
    }

    // 会发生什么 + 开始
    RowLayout {
        Layout.fillWidth: true
        spacing: 8
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
            text: !panel.lines.length ? "先在上面粘贴链接。"
                : panel.lines.length + " 个链接"
                  + (!panel.scheme.ranked ? "" : panel.values.rank === "none" ? "，每个全部下载"
                     : "，每个读一次列表页、按" + panel.optionLabel("rank") + "取" + panel.optionLabel("top") + "（排名表写进任务日志）"
                       + (panel.kind.id === "music" ? "；完整专辑整张下" : ""))
                  + "，排成一个任务在后台下完，下过的自动跳过。"
                  + (panel.kind.id === "music" ? "每首的 Last.fm 信息（风格、专辑、听众、重播率）写进文件，播放时显示。" : "")
        }
        PrimaryButton { text: panel.lines.length ? "开始下载 " + panel.lines.length + " 个链接" : "开始下载"; enabled: panel.ready; onClicked: panel.start() }
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 下载方案页（音乐、YouTube、B 站、X、其他链接）：按服务端给的方案说明生成。
//   一行流程 + 去向 → 输入 →（有列表的方案）读取列表、勾选 → 选项 → 后果说明与「下载到暂存区」。
// 列表只读元数据（/api/downloads/probe），已下载名单里有的会标出，默认不勾选。
ColumnLayout {
    id: panel
    required property var kind
    signal run(var params)
    signal openTab(string tab)
    spacing: 8

    ApiClient { id: api }
    readonly property var scheme: Object.assign({steps: [], destination: "", input: "", placeholder: "", probe: false, single: false}, kind.scheme || {})
    property var values: ({})
    property string input: ""
    property var probe: null            // {title, uploader, entries}
    property bool probing: false
    property string probeError: ""
    property var picked: ({})           // 条目序号 → true
    property string sortBy: "order"     // order | views | likes | ratio | date
    property var analysis: ({items: []})

    function field(name) { return kind.fields.find(function (f) { return f.name === name }) || null }
    function set(name, value) { var v = Object.assign({}, values); v[name] = value; values = v }
    function reset() {
        var v = {}
        kind.fields.forEach(function (f) { if (f.default !== undefined) v[f.name] = f.default })
        values = v; input = ""; probe = null; probeError = ""; picked = ({}); analysis = {items: []}
    }
    onKindChanged: reset()
    Component.onCompleted: reset()

    function readList() {
        probing = true; probeError = ""; probe = null; picked = ({})
        api.send("POST", "/api/downloads/probe", {url: input.trim(), library: values.library || "", scheme: kind.id}, function (r) {
            panel.probing = false
            panel.probe = r
            // 短列表默认全选（没下过的）；长列表（如整个频道）默认不选，由你挑
            var m = {}
            if (r.entries.length <= 50) r.entries.forEach(function (e, i) { if (!e.downloaded) m[i] = true })
            panel.picked = m
            if (panel.field("album") && !panel.values.album) panel.set("album", r.title)
        })
    }
    Connections { target: api; function onErrorChanged() { if (api.error && panel.probing) { panel.probing = false; panel.probeError = api.error } } }

    // 排序后的条目（保留原序号 i）
    readonly property var rows: {
        if (!probe) return []
        var list = probe.entries.map(function (e, i) { return {e: e, i: i} })
        var ratio = function (e) { return e.views ? (e.likes || 0) / e.views : 0 }
        var keys = {views: function (x) { return x.e.views || 0 }, likes: function (x) { return x.e.likes || 0 },
                    ratio: function (x) { return ratio(x.e) }, date: function (x) { return x.e.date || "" }}
        if (keys[sortBy]) list.sort(function (a, b) { var ka = keys[sortBy](a), kb = keys[sortBy](b); return ka < kb ? 1 : ka > kb ? -1 : a.i - b.i })
        return list
    }
    readonly property int pickedCount: Object.keys(picked).length
    function pickTop(n) { var m = {}; rows.slice(0, n).forEach(function (x) { if (!x.e.downloaded) m[x.i] = true }); picked = m }
    function pickAll(on, onlyNew) { var m = {}; if (on) probe.entries.forEach(function (e, i) { if (!onlyNew || !e.downloaded) m[i] = true }); picked = m }
    readonly property var lines: input.split("\n").map(function (s) { return s.trim() }).filter(function (s) { return s !== "" })
    readonly property bool ready: scheme.probe ? pickedCount > 0 : lines.length > 0
    function start() {
        var p = Object.assign({}, values)
        if (scheme.probe) {
            p.urls = Object.keys(picked).map(Number).sort(function (a, b) { return a - b }).map(function (i) { return probe.entries[i].url }).join("\n")
            // 批次名：音乐用歌单名，视频用频道名
            p.title = kind.id === "music" ? (probe.title || probe.uploader || "") : (probe.uploader || probe.title || "")
        } else p.urls = lines.join("\n")
        panel.run(p)
    }
    function duration(s) {
        if (!s) return ""
        s = Math.round(s)
        var h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60
        return (h ? h + ":" + (m < 10 ? "0" : "") : "") + m + ":" + (sec < 10 ? "0" : "") + sec
    }
    function count(n) { return n === null || n === undefined ? "" : n >= 1e8 ? (n / 1e8).toFixed(1) + "亿" : n >= 1e4 ? (n / 1e4).toFixed(1) + "万" : String(n) }
    Timer { id: analyzeTimer; interval: 300; onTriggered: if (panel.kind.id === "other") api.send("POST", "/api/downloads/analyze", {urls: panel.input, mode: "video", library: panel.values.library || ""}, function (r) { panel.analysis = r }) }

    // 流程与去向
    Flow {
        Layout.fillWidth: true
        spacing: 4
        Repeater {
            model: panel.scheme.steps
            delegate: Row {
                required property string modelData
                required property int index
                spacing: 4
                Label { text: (index + 1) + " " + modelData; color: "#c9d1d9"; font.pixelSize: 12 }
                Label { visible: index < panel.scheme.steps.length - 1; text: "→"; color: "#6e7681"; font.pixelSize: 12 }
            }
        }
    }
    RowLayout {
        spacing: 6
        Label { text: "放到"; color: "#8b949e"; font.pixelSize: 12 }
        Label { text: panel.scheme.destination; color: "#3fb950"; font.pixelSize: 13; font.bold: true }
        Label { text: "（先进暂存区，确认后移入）"; color: "#6e7681"; font.pixelSize: 12; TapHandler { onTapped: panel.openTab("staging") } }
    }
    Label { Layout.fillWidth: true; wrapMode: Text.Wrap; text: panel.kind.description; color: "#8b949e"; font.pixelSize: 12 }

    // 输入
    RowLayout {
        visible: panel.scheme.single
        Layout.fillWidth: true
        spacing: 6
        TextField {
            Layout.fillWidth: true; implicitHeight: 32; font.pixelSize: 13
            placeholderText: panel.scheme.placeholder
            text: panel.input
            onTextEdited: panel.input = text
            onAccepted: if (text.trim()) panel.readList()
        }
        BusyIndicator { running: panel.probing; visible: running; implicitWidth: 26; implicitHeight: 26 }
        PrimaryButton { text: "读取列表"; enabled: panel.input.trim() !== "" && !panel.probing; onClicked: panel.readList() }
    }
    ScrollView {
        visible: !panel.scheme.single
        Layout.fillWidth: true
        implicitHeight: 90
        TextArea {
            placeholderText: panel.scheme.placeholder
            text: panel.input
            wrapMode: TextEdit.WrapAnywhere; font.pixelSize: 13
            onTextChanged: if (text !== panel.input) { panel.input = text; analyzeTimer.restart() }
            background: Rectangle { color: "#0d1117"; border.color: parent.activeFocus ? "#58a6ff" : "#30363d"; radius: 4 }
        }
    }
    Label { visible: panel.probeError !== ""; text: panel.probeError; color: "#f85149"; font.pixelSize: 12; wrapMode: Text.Wrap; Layout.fillWidth: true }

    // 选项（一行）
    RowLayout {
        Layout.fillWidth: true
        spacing: 6
        Repeater {
            model: panel.kind.fields.filter(function (f) { return f.name !== "urls" && f.name !== "library" })
            delegate: RowLayout {
                required property var modelData
                spacing: 4
                Label { text: modelData.label; color: "#8b949e"; font.pixelSize: 12 }
                ComboBox {
                    visible: modelData.type === "choice"
                    implicitHeight: 30; Layout.preferredWidth: 130; font.pixelSize: 12
                    textRole: "label"; valueRole: "value"
                    model: modelData.options || []
                    currentIndex: Math.max(0, (modelData.options || []).findIndex(function (o) { return o.value === panel.values[modelData.name] }))
                    onActivated: panel.set(modelData.name, currentValue)
                }
                TextField {
                    visible: modelData.type === "text"
                    implicitHeight: 30; Layout.preferredWidth: 220; font.pixelSize: 12
                    placeholderText: modelData.help || ""
                    text: panel.values[modelData.name] || ""
                    onTextEdited: panel.set(modelData.name, text)
                }
            }
        }
        Item { Layout.fillWidth: true }
        Label { text: "暂存到"; color: "#8b949e"; font.pixelSize: 12 }
        ComboBox {
            implicitHeight: 30; Layout.preferredWidth: 120; font.pixelSize: 12
            textRole: "label"; valueRole: "value"
            model: panel.field("library") ? panel.field("library").options : []
            currentIndex: Math.max(0, (panel.field("library") ? panel.field("library").options : []).findIndex(function (o) { return o.value === panel.values.library }))
            onActivated: panel.set("library", currentValue)
        }
    }

    // 列表
    RowLayout {
        visible: !!panel.probe
        Layout.fillWidth: true
        spacing: 4
        Label {
            text: panel.probe ? (panel.probe.title || panel.probe.uploader) + " · " + panel.probe.entries.length + " 条"
                  + (panel.probe.entries.some(function (e) { return e.downloaded }) ? "（已下载 " + panel.probe.entries.filter(function (e) { return e.downloaded }).length + "）" : "") : ""
            color: "#e6edf3"; font.pixelSize: 13; font.bold: true; elide: Text.ElideRight; Layout.maximumWidth: 360
        }
        Divider {}
        Chip { text: "全选未下载"; onClicked: panel.pickAll(true, true) }
        Chip { text: "全不选"; onClicked: panel.pickAll(false) }
        Chip { text: "前 10"; onClicked: panel.pickTop(10) }
        Chip { text: "前 50"; onClicked: panel.pickTop(50) }
        Divider {}
        Label { text: "排序"; color: "#8b949e"; font.pixelSize: 12 }
        Repeater {
            model: [["order", "原顺序"], ["date", "最新"], ["views", "播放量"], ["likes", "点赞"], ["ratio", "点赞率"]]
            delegate: Chip { required property var modelData; text: modelData[1]; selected: panel.sortBy === modelData[0]; onClicked: panel.sortBy = modelData[0] }
        }
    }
    ListView {
        visible: !!panel.probe
        Layout.fillWidth: true
        Layout.preferredHeight: Math.min(contentHeight, 420)
        clip: true
        model: panel.rows
        WheelScroll { }
        ScrollBar.vertical: ScrollBar { }
        delegate: RowLayout {
            required property var modelData
            width: ListView.view.width
            height: 30
            spacing: 6
            CheckBox {
                checked: !!panel.picked[modelData.i]
                onToggled: { var m = Object.assign({}, panel.picked); if (checked) m[modelData.i] = true; else delete m[modelData.i]; panel.picked = m }
            }
            Label { text: modelData.i + 1; color: "#6e7681"; font.pixelSize: 11; Layout.preferredWidth: 30 }
            Label { text: modelData.e.title; color: modelData.e.downloaded ? "#6e7681" : "#e6edf3"; font.pixelSize: 12; elide: Text.ElideRight; Layout.fillWidth: true }
            Label { visible: modelData.e.downloaded; text: "已下载"; color: "#3fb950"; font.pixelSize: 11 }
            Label { text: panel.duration(modelData.e.duration); color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 50; horizontalAlignment: Text.AlignRight }
            Label { text: modelData.e.date ? modelData.e.date.slice(0, 4) + "-" + modelData.e.date.slice(4, 6) + "-" + modelData.e.date.slice(6) : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 72 }
            Label { text: modelData.e.views ? "▶ " + panel.count(modelData.e.views) : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 64 }
            Label { text: modelData.e.likes ? "♥ " + panel.count(modelData.e.likes) + (modelData.e.views ? " · " + (modelData.e.likes / modelData.e.views * 100).toFixed(1) + "%" : "") : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 110 }
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

    // 后果与开始
    RowLayout {
        Layout.fillWidth: true
        spacing: 8
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
            text: !panel.ready ? (panel.scheme.probe ? "先读取列表，再勾选要下的。" : "先填上面的内容。")
                : "将下载 " + (panel.scheme.probe ? panel.pickedCount + " 个" : panel.lines.length + " 项") + "到暂存区（新批次），资源库里已有的内容不会被改动；下过的自动跳过。"
                  + "下完在「暂存区」检查，确认后「移入资源库」，就会出现在 " + panel.scheme.destination + "。"
        }
        PrimaryButton { text: "下载到暂存区"; enabled: panel.ready && !(panel.kind.missing && panel.kind.missing.length); onClicked: panel.start() }
    }
}

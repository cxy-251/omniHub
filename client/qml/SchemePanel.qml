import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 下载方案页（音乐、YouTube、B 站、X、其他链接）：按服务端给的方案说明生成。
//   一行流程 + 去向 → 链接（一行一个）→ 两种下法：
//     直接下载：每个链接（频道、歌单、合集）取最新 N 个，排成一个任务按顺序下完，设好就不用管；
//     读取列表挑选：逐个读出列表（只读元数据，/api/downloads/probe），按列表分组显示，勾选后一起下。
// 每个方案有「已下载」名单：下过的标出、跳过，所以同一批链接以后再下就只下新的。
ColumnLayout {
    id: panel
    required property var kind
    signal run(var params)
    signal openTab(string tab)
    spacing: 8

    ApiClient { id: api }
    readonly property var scheme: Object.assign({steps: [], destination: "", input: "", placeholder: "", probe: false}, kind.scheme || {})
    property var values: ({})
    property string input: ""
    property var groups: []             // 读出的列表：[{url, title, uploader, entries, error}]
    property int probingIndex: -1       // 正在读第几个链接
    property var picked: ({})           // "组号:条目号" → true
    property string sortBy: "order"     // order | date | views | likes | ratio
    property var analysis: ({items: []})

    function field(name) { return kind.fields.find(function (f) { return f.name === name }) || null }
    function set(name, value) { var v = Object.assign({}, values); v[name] = value; values = v }
    function reset() {
        var v = {}
        kind.fields.forEach(function (f) { if (f.default !== undefined) v[f.name] = f.default })
        values = v; input = ""; groups = []; picked = ({}); probingIndex = -1; analysis = {items: []}
    }
    onKindChanged: reset()
    Component.onCompleted: reset()
    readonly property var lines: input.split("\n").map(function (s) { return s.trim() }).filter(function (s) { return s !== "" })

    // 逐个读列表（一个读完再读下一个，避免同时打开太多请求被网站限制）
    function readAll() {
        groups = []; picked = ({})
        readNext(0)
    }
    function readNext(i) {
        if (i >= lines.length) { probingIndex = -1; return }
        probingIndex = i
        var url = lines[i]
        api.error = ""   // 连续两个链接报同样的错时，第二次也要触发 onErrorChanged
        api.send("POST", "/api/downloads/probe", {url: url, library: values.library || "", scheme: kind.id}, function (r) {
            var g = panel.groups.slice()
            g.push({url: url, title: r.title, uploader: r.uploader, entries: r.entries, error: ""})
            panel.groups = g
            // 短列表默认全选没下过的；长列表（整个频道）默认不选
            if (r.entries.length <= 50) {
                var m = Object.assign({}, panel.picked)
                r.entries.forEach(function (e, k) { if (!e.downloaded) m[(g.length - 1) + ":" + k] = true })
                panel.picked = m
            }
            if (panel.field("album") && g.length === 1 && !panel.values.album) panel.set("album", r.title)
            panel.readNext(i + 1)
        })
    }
    Connections {
        target: api
        function onErrorChanged() {
            if (!api.error || panel.probingIndex < 0) return
            var g = panel.groups.slice()
            g.push({url: panel.lines[panel.probingIndex], title: "", uploader: "", entries: [], error: api.error})
            panel.groups = g
            panel.readNext(panel.probingIndex + 1)
        }
    }

    // 显示用的行：每组一个标题行 + 该组条目（组内排序）
    readonly property var rows: {
        var out = []
        var ratio = function (e) { return e.views ? (e.likes || 0) / e.views : 0 }
        var keys = {views: function (x) { return x.e.views || 0 }, likes: function (x) { return x.e.likes || 0 },
                    ratio: function (x) { return ratio(x.e) }, date: function (x) { return x.e.date || "" }}
        groups.forEach(function (grp, g) {
            out.push({header: true, g: g, grp: grp})
            var list = grp.entries.map(function (e, i) { return {e: e, i: i, g: g} })
            if (keys[sortBy]) list.sort(function (a, b) { var ka = keys[sortBy](a), kb = keys[sortBy](b); return ka < kb ? 1 : ka > kb ? -1 : a.i - b.i })
            out = out.concat(list)
        })
        return out
    }
    readonly property int pickedCount: Object.keys(picked).length
    readonly property int entryCount: groups.reduce(function (n, grp) { return n + grp.entries.length }, 0)
    function pickEach(n) {   // 每个列表取（当前排序下）前 n 个没下过的；n<=0 为全部
        var m = {}
        groups.forEach(function (grp, g) {
            var taken = 0
            rows.forEach(function (x) { if (!x.header && x.g === g && !x.e.downloaded && (n <= 0 || taken < n)) { m[g + ":" + x.i] = true; taken++ } })
        })
        picked = m
    }
    function toggle(key, on) { var m = Object.assign({}, picked); if (on) m[key] = true; else delete m[key]; picked = m }

    readonly property bool canPick: pickedCount > 0
    readonly property bool canDirect: lines.length > 0
    function startPicked() {
        var p = Object.assign({}, values)
        p.mode = "picked"
        p.urls = Object.keys(picked).map(function (k) { var a = k.split(":"); return panel.groups[+a[0]].entries[+a[1]].url }).join("\n")
        p.title = groups.length === 1 ? (kind.id === "music" ? groups[0].title : (groups[0].uploader || groups[0].title)) : ""
        if (groups.length !== 1) delete p.album
        panel.run(p)
    }
    function startDirect() {
        var p = Object.assign({}, values)
        p.mode = "direct"
        p.urls = lines.join("\n")
        delete p.album
        panel.run(p)
    }
    function duration(s) {
        if (!s) return ""
        s = Math.round(s)
        var h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60
        return (h ? h + ":" + (m < 10 ? "0" : "") : "") + m + ":" + (sec < 10 ? "0" : "") + sec
    }
    function count(n) { return n === null || n === undefined ? "" : n >= 1e8 ? (n / 1e8).toFixed(1) + "亿" : n >= 1e4 ? (n / 1e4).toFixed(1) + "万" : String(n) }
    function perListLabel() { var f = field("per_list"); var o = f ? f.options.find(function (x) { return x.value === values.per_list }) : null; return o ? o.label : "" }
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

    // 链接（一行一个）
    ScrollView {
        Layout.fillWidth: true
        implicitHeight: 96
        TextArea {
            placeholderText: panel.scheme.placeholder
            text: panel.input
            wrapMode: TextEdit.WrapAnywhere; font.pixelSize: 13
            onTextChanged: if (text !== panel.input) { panel.input = text; analyzeTimer.restart() }
            background: Rectangle { color: "#0d1117"; border.color: parent.activeFocus ? "#58a6ff" : "#30363d"; radius: 4 }
        }
    }

    // 选项（一行）
    RowLayout {
        Layout.fillWidth: true
        spacing: 6
        Repeater {
            model: panel.kind.fields.filter(function (f) { return ["urls", "library", "per_list"].indexOf(f.name) < 0 && !(f.name === "album" && panel.groups.length !== 1) })
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

    // 两种下法
    RowLayout {
        visible: panel.scheme.probe
        Layout.fillWidth: true
        spacing: 6
        Label { text: "直接下载：每个链接取"; color: "#c9d1d9"; font.pixelSize: 12 }
        ComboBox {
            implicitHeight: 30; Layout.preferredWidth: 120; font.pixelSize: 12
            textRole: "label"; valueRole: "value"
            model: panel.field("per_list") ? panel.field("per_list").options : []
            currentIndex: Math.max(0, (panel.field("per_list") ? panel.field("per_list").options : []).findIndex(function (o) { return o.value === panel.values.per_list }))
            onActivated: panel.set("per_list", currentValue)
        }
        PrimaryButton { text: "直接下载 " + panel.lines.length + " 个链接"; enabled: panel.canDirect && panel.probingIndex < 0; onClicked: panel.startDirect() }
        Divider { Layout.leftMargin: 4; Layout.rightMargin: 4 }
        Chip { text: panel.probingIndex >= 0 ? "正在读第 " + (panel.probingIndex + 1) + "/" + panel.lines.length + " 个…" : "读取列表挑选"; enabled: panel.canDirect && panel.probingIndex < 0; onClicked: panel.readAll() }
        BusyIndicator { running: panel.probingIndex >= 0; visible: running; implicitWidth: 24; implicitHeight: 24 }
        Item { Layout.fillWidth: true }
    }

    // 列表（按链接分组）
    RowLayout {
        visible: panel.groups.length > 0
        Layout.fillWidth: true
        spacing: 4
        Label { text: panel.groups.length + " 个列表，共 " + panel.entryCount + " 条"; color: "#e6edf3"; font.pixelSize: 13; font.bold: true }
        Divider {}
        Chip { text: "全选未下载"; onClicked: panel.pickEach(0) }
        Chip { text: "全不选"; onClicked: panel.picked = ({}) }
        Chip { text: "每个列表前 10"; onClicked: panel.pickEach(10) }
        Chip { text: "每个列表前 50"; onClicked: panel.pickEach(50) }
        Divider {}
        Label { text: "排序"; color: "#8b949e"; font.pixelSize: 12 }
        Repeater {
            model: [["order", "原顺序"], ["date", "最新"], ["views", "播放量"], ["likes", "点赞"], ["ratio", "点赞率"]]
            delegate: Chip { required property var modelData; text: modelData[1]; selected: panel.sortBy === modelData[0]; onClicked: panel.sortBy = modelData[0] }
        }
    }
    ListView {
        visible: panel.groups.length > 0
        Layout.fillWidth: true
        Layout.preferredHeight: Math.min(contentHeight, 440)
        clip: true
        model: panel.rows
        WheelScroll { }
        ScrollBar.vertical: ScrollBar { }
        delegate: Item {
            required property var modelData
            width: ListView.view.width
            height: modelData.header ? 30 : 28
            Label {
                visible: !!modelData.header
                anchors.fill: parent; verticalAlignment: Text.AlignVCenter
                elide: Text.ElideRight; font.pixelSize: 13; font.bold: true
                color: modelData.header && modelData.grp.error ? "#f85149" : "#58a6ff"
                text: !modelData.header ? "" : modelData.grp.error ? "✗ " + modelData.grp.url + "：" + modelData.grp.error
                    : (modelData.grp.title || modelData.grp.uploader) + "  ·  " + modelData.grp.entries.length + " 条"
                      + (modelData.grp.entries.some(function (e) { return e.downloaded }) ? "（已下载 " + modelData.grp.entries.filter(function (e) { return e.downloaded }).length + "）" : "")
            }
            RowLayout {
                visible: !modelData.header
                anchors.fill: parent
                spacing: 6
                CheckBox {
                    checked: !modelData.header && !!panel.picked[modelData.g + ":" + modelData.i]
                    onToggled: panel.toggle(modelData.g + ":" + modelData.i, checked)
                }
                Label { text: modelData.header ? "" : modelData.i + 1; color: "#6e7681"; font.pixelSize: 11; Layout.preferredWidth: 30 }
                Label { text: modelData.header ? "" : modelData.e.title; color: !modelData.header && modelData.e.downloaded ? "#6e7681" : "#e6edf3"; font.pixelSize: 12; elide: Text.ElideRight; Layout.fillWidth: true }
                Label { visible: !modelData.header && modelData.e.downloaded; text: "已下载"; color: "#3fb950"; font.pixelSize: 11 }
                Label { text: modelData.header ? "" : panel.duration(modelData.e.duration); color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 50; horizontalAlignment: Text.AlignRight }
                Label { text: !modelData.header && modelData.e.date ? modelData.e.date.slice(0, 4) + "-" + modelData.e.date.slice(4, 6) + "-" + modelData.e.date.slice(6) : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 72 }
                Label { text: !modelData.header && modelData.e.views ? "▶ " + panel.count(modelData.e.views) : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 64 }
                Label { text: !modelData.header && modelData.e.likes ? "♥ " + panel.count(modelData.e.likes) + (modelData.e.views ? " · " + (modelData.e.likes / modelData.e.views * 100).toFixed(1) + "%" : "") : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 110 }
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

    // 后果与开始
    RowLayout {
        Layout.fillWidth: true
        spacing: 8
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
            text: panel.scheme.probe
                ? (panel.groups.length ? "已勾选 " + panel.pickedCount + " 条。" : "")
                  + "直接下载：" + panel.lines.length + " 个链接各取" + panel.perListLabel() + "，排成一个任务按顺序下完，可以不用管；下过的自动跳过，同一批链接以后再下就只下新的。"
                  + "都先进暂存区，确认后「移入资源库」才会出现在 " + panel.scheme.destination + "。"
                : (panel.lines.length ? panel.lines.length + " 项下载到暂存区（新批次），下过的自动跳过；确认后「移入资源库」才会出现在 " + panel.scheme.destination + "。" : "先填上面的内容。")
        }
        PrimaryButton {
            visible: panel.scheme.probe
            text: "下载勾选的 " + panel.pickedCount + " 条"
            enabled: panel.canPick && !(panel.kind.missing && panel.kind.missing.length)
            onClicked: panel.startPicked()
        }
        PrimaryButton {
            visible: !panel.scheme.probe
            text: "下载到暂存区"
            enabled: panel.canDirect && !(panel.kind.missing && panel.kind.missing.length)
            onClicked: panel.startDirect()
        }
    }
}

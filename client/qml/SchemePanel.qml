import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// 下载方案页（音乐、YouTube、B 站、X、其他链接）：按服务端给的方案说明生成。
//   一行流程 + 去向 → 链接（一行一个）→ 两种下法：
//     直接下载：每个链接（频道、歌单、合集）提取单个视频、逐个读数据，按重播率/点赞率/播放量取前 N 个，排成一个任务下完，设好就不用管；
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
    // 补充数据（读完列表后逐个读：播放、点赞、日期、艺人、专辑；音乐再加 Last.fm 重播率与标签），按链接存
    property var info: ({})
    property var detailQueue: []
    property bool detailing: false
    property int detailDone: 0
    property int detailTotal: 0
    property string detailError: ""

    function field(name) { return kind.fields.find(function (f) { return f.name === name }) || null }
    function set(name, value) { var v = Object.assign({}, values); v[name] = value; values = v }
    function reset() {
        var v = {}
        kind.fields.forEach(function (f) { if (f.default !== undefined) v[f.name] = f.default })
        values = v; input = ""; groups = []; picked = ({}); probingIndex = -1; analysis = {items: []}
        info = ({}); detailQueue = []; detailing = false; detailError = ""
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
        if (i >= lines.length) { probingIndex = -1; startDetails(); return }
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
            if (api.error && panel.detailing) { panel.detailing = false; panel.detailError = api.error; return }
            if (!api.error || panel.probingIndex < 0) return
            var g = panel.groups.slice()
            g.push({url: panel.lines[panel.probingIndex], title: "", uploader: "", entries: [], error: api.error})
            panel.groups = g
            panel.readNext(panel.probingIndex + 1)
        }
    }

    // 补充数据：每次 8 个，一批读完再发下一批；可以中途停止
    function startDetails() {
        var q = []
        groups.forEach(function (grp) { grp.entries.forEach(function (e) { if (!panel.info[e.url]) q.push(e.url) }) })
        detailQueue = q; detailDone = 0; detailTotal = q.length; detailError = ""
        detailing = q.length > 0
        if (detailing) nextDetails()
    }
    function nextDetails() {
        if (!detailing || detailQueue.length === 0) { detailing = false; return }
        var batch = detailQueue.slice(0, 8)
        detailQueue = detailQueue.slice(8)
        api.error = ""
        api.send("POST", "/api/downloads/details", {urls: batch, scheme: kind.id}, function (r) {
            var m = Object.assign({}, panel.info)
            r.forEach(function (d) { m[d.url] = d })
            panel.info = m
            panel.detailDone += batch.length
            panel.nextDetails()
        })
    }
    // 条目 + 补充的数据
    function full(e) { var d = info[e.url]; return d ? Object.assign({}, e, d, {title: e.title || d.title}) : e }

    // 显示用的行：每组一个标题行 + 该组条目（组内排序）
    readonly property var rows: {
        var out = []
        var ratio = function (e) { return e.views ? (e.likes || 0) / e.views : -1 }
        var keys = {views: function (x) { return x.e.views || -1 }, likes: function (x) { return x.e.likes || -1 },
                    ratio: function (x) { return ratio(x.e) }, date: function (x) { return x.e.date || "" },
                    replay: function (x) { return x.e.replay || -1 }}
        groups.forEach(function (grp, g) {
            out.push({header: true, g: g, grp: grp})
            var list = grp.entries.map(function (e, i) { return {e: panel.full(e), i: i, g: g} })
            if (keys[sortBy]) list.sort(function (a, b) { var ka = keys[sortBy](a), kb = keys[sortBy](b); return ka < kb ? 1 : ka > kb ? -1 : a.i - b.i })
            // 按专辑：同一张专辑的排在一起（专辑名先取 YouTube 的，没有再用 Last.fm 的），没有专辑的放最后
            if (sortBy === "album") {
                var alb = function (x) { return x.e.album || x.e.lastfm_album || "\uffff" }
                list.sort(function (a, b) { var ka = alb(a), kb = alb(b); return ka < kb ? -1 : ka > kb ? 1 : a.i - b.i })
            }
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
    function optionLabel(name) { var f = field(name); var o = f ? f.options.find(function (x) { return x.value === values[name] }) : null; return o ? o.label : "" }
    readonly property var rankFields: ["rank", "top", "pool"]
    Timer { id: analyzeTimer; interval: 300; onTriggered: if (panel.kind.id === "other") api.send("POST", "/api/downloads/analyze", {urls: panel.input, mode: "video", library: panel.values.library || ""}, function (r) { panel.analysis = r }) }

    property bool showHelp: false

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
        implicitHeight: 96
        TextArea {
            placeholderText: panel.scheme.input + "\n" + panel.scheme.placeholder
            text: panel.input
            wrapMode: TextEdit.WrapAnywhere; font.pixelSize: 13
            onTextChanged: if (text !== panel.input) { panel.input = text; analyzeTimer.restart() }
            background: Rectangle { color: "#0d1117"; border.color: parent.activeFocus ? "#58a6ff" : "#30363d"; radius: 4 }
        }
    }

    // 通用选项（放不下自动换行）
    Flow {
        Layout.fillWidth: true
        spacing: 8
        Repeater {
            model: panel.kind.fields.filter(function (f) { return ["urls"].indexOf(f.name) < 0 && panel.rankFields.indexOf(f.name) < 0 && !(f.name === "album" && panel.groups.length !== 1) })
            delegate: Row {
                required property var modelData
                spacing: 4
                Label { text: modelData.label; color: "#8b949e"; font.pixelSize: 12; anchors.verticalCenter: parent.verticalCenter }
                ComboBox {
                    visible: modelData.type === "choice"
                    implicitHeight: 30; width: modelData.name === "library" ? 120 : 130; font.pixelSize: 12
                    textRole: "label"; valueRole: "value"
                    model: modelData.options || []
                    currentIndex: Math.max(0, (modelData.options || []).findIndex(function (o) { return o.value === panel.values[modelData.name] }))
                    onActivated: panel.set(modelData.name, currentValue)
                }
                TextField {
                    visible: modelData.type === "text"
                    implicitHeight: 30; width: 220; font.pixelSize: 12
                    placeholderText: modelData.help || ""
                    text: panel.values[modelData.name] || ""
                    onTextEdited: panel.set(modelData.name, text)
                }
            }
        }
    }

    // 两种下法：两张卡片
    Flow {
        visible: panel.scheme.probe
        Layout.fillWidth: true
        spacing: 8
        readonly property real cardWidth: width >= 640 ? (width - spacing) / 2 : width
        // 自动挑选
        Rectangle {
            width: parent.cardWidth
            height: autoCol.implicitHeight + 20
            radius: 6; color: "#161b22"; border.color: "#1f6feb"
            ColumnLayout {
                id: autoCol
                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 10
                spacing: 6
                Label { text: "自动挑选，设好就不用管"; color: "#e6edf3"; font.pixelSize: 14; font.bold: true }
                Label {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
                    text: panel.values.rank === "none" ? "每个链接全部下载。"
                        : "每个链接：提取单个视频 → 逐个读数据 → 按" + panel.optionLabel("rank") + "取" + panel.optionLabel("top") + "，排名表写进任务日志。"
                          + (panel.kind.id === "music" ? "完整专辑整张下，不排名。" : "")
                }
                Flow {
                    Layout.fillWidth: true
                    spacing: 6
                    Repeater {
                        model: panel.kind.fields.filter(function (f) { return panel.rankFields.indexOf(f.name) >= 0 && (f.name === "rank" || panel.values.rank !== "none") })
                        delegate: Row {
                            required property var modelData
                            spacing: 4
                            Label { text: modelData.label; color: "#8b949e"; font.pixelSize: 12; anchors.verticalCenter: parent.verticalCenter }
                            ComboBox {
                                implicitHeight: 30; width: modelData.name === "rank" ? 150 : 110; font.pixelSize: 12
                                textRole: "label"; valueRole: "value"
                                model: modelData.options || []
                                currentIndex: Math.max(0, (modelData.options || []).findIndex(function (o) { return o.value === panel.values[modelData.name] }))
                                onActivated: panel.set(modelData.name, currentValue)
                                ToolTip.visible: hovered && !!modelData.help; ToolTip.text: modelData.help || ""
                            }
                        }
                    }
                }
                PrimaryButton {
                    text: panel.lines.length ? "开始：自动挑选并下载 " + panel.lines.length + " 个链接" : "先在上面粘贴链接"
                    enabled: panel.canDirect && panel.probingIndex < 0 && !(panel.kind.missing && panel.kind.missing.length)
                    onClicked: panel.startDirect()
                }
            }
        }
        // 自己挑
        Rectangle {
            width: parent.cardWidth
            height: pickCol.implicitHeight + 20
            radius: 6; color: "#161b22"; border.color: "#30363d"
            ColumnLayout {
                id: pickCol
                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 10
                spacing: 6
                Label { text: "先看列表，自己勾选"; color: "#e6edf3"; font.pixelSize: 14; font.bold: true }
                Label {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; color: "#8b949e"; font.pixelSize: 12
                    text: "逐个读出每个链接里有哪些（只读信息，不下载），在下面按列表分组显示，勾好后一起下。已下载过的会标出来。"
                }
                RowLayout {
                    spacing: 6
                    Button {
                        text: panel.probingIndex >= 0 ? "正在读第 " + (panel.probingIndex + 1) + "/" + panel.lines.length + " 个…" : "读取列表"
                        enabled: panel.canDirect && panel.probingIndex < 0
                        onClicked: panel.readAll()
                    }
                    BusyIndicator { running: panel.probingIndex >= 0; visible: running; implicitWidth: 24; implicitHeight: 24 }
                }
            }
        }
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
            model: (panel.kind.id === "music" ? [["replay", "重播率"]] : []).concat([["ratio", "点赞率"], ["views", "播放量"], ["date", "最新"], ["album", "按专辑"], ["order", "原顺序"]])
            delegate: Chip { required property var modelData; text: modelData[1]; selected: panel.sortBy === modelData[0]; onClicked: panel.sortBy = modelData[0] }
        }
    }
    // 补充数据的进度
    RowLayout {
        visible: panel.groups.length > 0 && (panel.detailing || panel.detailError !== "" || panel.detailDone < panel.detailTotal)
        Layout.fillWidth: true
        spacing: 6
        BusyIndicator { running: panel.detailing; visible: running; implicitWidth: 22; implicitHeight: 22 }
        ProgressBar { Layout.preferredWidth: 200; from: 0; to: Math.max(1, panel.detailTotal); value: panel.detailDone }
        Label {
            Layout.fillWidth: true; elide: Text.ElideRight; font.pixelSize: 12
            color: panel.detailError ? "#f85149" : "#8b949e"
            text: panel.detailError ? "补充数据停了：" + panel.detailError
                : "正在补充数据 " + panel.detailDone + "/" + panel.detailTotal + "（播放、点赞、专辑" + (panel.kind.id === "music" ? "、Last.fm 重播率与风格" : "") + "；每首约 3 秒，可以边看边勾）"
        }
        Chip { text: panel.detailing ? "停止" : "继续"; onClicked: { if (panel.detailing) panel.detailing = false; else { panel.detailing = true; panel.detailError = ""; panel.nextDetails() } } }
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
            height: modelData.header ? 30 : 44
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
                // 两行：歌名（+ 风格标签）／艺人 · 专辑 · 年份
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 0
                    RowLayout {
                        Layout.fillWidth: true
                        spacing: 6
                        Label { text: modelData.header ? "" : modelData.e.title; color: !modelData.header && modelData.e.downloaded ? "#6e7681" : "#e6edf3"; font.pixelSize: 12; elide: Text.ElideRight; Layout.fillWidth: true }
                        Label { visible: !modelData.header && !!modelData.e.tags && modelData.e.tags.length > 0; text: !modelData.header && modelData.e.tags ? modelData.e.tags.slice(0, 3).join(" · ") : ""; color: "#a371f7"; font.pixelSize: 10 }
                        Label { visible: !modelData.header && modelData.e.downloaded; text: "已下载"; color: "#3fb950"; font.pixelSize: 11 }
                    }
                    Label {
                        Layout.fillWidth: true; elide: Text.ElideRight; font.pixelSize: 11; color: "#8b949e"
                        text: modelData.header ? "" : [modelData.e.artist || modelData.e.uploader || "", (modelData.e.album || modelData.e.lastfm_album) ? "专辑《" + (modelData.e.album || modelData.e.lastfm_album) + "》" : "", modelData.e.year || ""].filter(function (x) { return x }).join(" · ")
                    }
                }
                Label { text: modelData.header ? "" : panel.duration(modelData.e.duration); color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 50; horizontalAlignment: Text.AlignRight }

                Label { text: !modelData.header && modelData.e.views ? "▶ " + panel.count(modelData.e.views) : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 64 }
                Label { text: !modelData.header && modelData.e.likes && modelData.e.views ? "♥ " + (modelData.e.likes / modelData.e.views * 100).toFixed(1) + "%" : ""; color: "#8b949e"; font.pixelSize: 11; Layout.preferredWidth: 56 }
                Label {
                    visible: panel.kind.id === "music"
                    text: !modelData.header && modelData.e.replay ? "重播 " + modelData.e.replay.toFixed(1) : (!modelData.header && panel.info[modelData.e.url] ? "—" : "")
                    color: !modelData.header && modelData.e.replay >= 10 ? "#3fb950" : "#c9d1d9"; font.pixelSize: 11; Layout.preferredWidth: 64
                    ToolTip.visible: replayHover.hovered && !modelData.header && !!modelData.e.listeners
                    ToolTip.text: !modelData.header && modelData.e.listeners ? "Last.fm：" + (modelData.e.lastfm_name || "") + "\n听众 " + panel.count(modelData.e.listeners) + "，播放 " + panel.count(modelData.e.playcount) + "\n听过的人平均听 " + modelData.e.replay.toFixed(1) + " 遍" : ""
                    HoverHandler { id: replayHover }
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

    // 下载：勾选的（读取列表后）／没有列表的方案直接下
    RowLayout {
        Layout.fillWidth: true
        spacing: 8
        visible: panel.groups.length > 0 || !panel.scheme.probe
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
            text: panel.scheme.probe ? "已勾选 " + panel.pickedCount + " 条，下到暂存区（下过的自动跳过）。"
                : (panel.lines.length ? panel.lines.length + " 项下到暂存区（新批次），下过的自动跳过。" : "先在上面填内容。")
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

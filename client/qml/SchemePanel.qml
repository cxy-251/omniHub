import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 下载方案页（音乐、YouTube、B 站、X、其他链接）：按服务端给的方案说明生成。
//   去向 → 链接（一行一个）→ 选项 →（列表类方案）怎么挑 → 开始。设好就不用管，进度在「任务」里。
// 视频方案可按播放量取每个链接前 N 个（只读一次列表页）；音乐全部下载不筛选，数据写进标签。
// 音乐先「读取列表」：服务端按下载时同样的规则（换纯音轨版、查专辑、挑 MV）列出会下什么、不下什么，
// 去掉不要的勾，再按这份列表下载。
ColumnLayout {
    id: panel
    required property var kind
    property string stagingLibrary: ""   // 暂存到哪个资源库：下载页顶部统一选
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
        plan = null; planJob = null; planToken = ""; excluded = {}
        if (kind.scheme && kind.scheme.listable) restoreList()   // 不用 listable：换方案时这个绑定可能还没更新
    }
    onKindChanged: reset()
    Component.onCompleted: reset()
    readonly property var lines: input.split("\n").map(function (s) { return s.trim() }).filter(function (s) { return s !== "" })
    readonly property bool ready: lines.length > 0 && !(kind.missing && kind.missing.length)
    function start() {
        var p = Object.assign({}, values)
        p.mode = "direct"
        p.urls = lines.join("\n")
        if (stagingLibrary) p.library = stagingLibrary
        panel.run(p)
    }

    // ---- 读取列表（音乐、YouTube 等）：先看会下什么、去掉不要的勾，再按列表下 ----
    readonly property bool isMusic: kind.id === "music"
    readonly property bool listable: !!scheme.listable
    property string planToken: ""
    property var planJob: null     // 读取列表的任务（/api/downloads/jobs 里的一项）
    property var plan: null        // 读出来的列表：songs、mvs、videos、skipped、done、other
    property var excluded: ({})    // 去掉勾的视频 ID
    readonly property bool listing: !!planJob && planJob.running
    readonly property bool listFailed: !!planJob && !planJob.running && planJob.exit_code !== 0
    // 读取在服务端后台跑：切到别的标签页、关掉下载页再回来，按存下的令牌和任务接着看（每个方案一份）
    Settings { id: listPrefs; category: "downloadLists"; property string state: "{}" }
    function saved() { try { return JSON.parse(listPrefs.state)[kind.id] || {} } catch (e) { return {} } }
    function save(fields) {
        var all = {}
        try { all = JSON.parse(listPrefs.state) } catch (e) {}
        all[kind.id] = Object.assign(all[kind.id] || {}, fields)
        listPrefs.state = JSON.stringify(all)
    }
    function clearPlan() { planToken = ""; planJob = null; plan = null; excluded = {}; save({token: "", job: "", excluded: {}, picked: false}) }
    function readList() {
        clearPlan()
        planToken = Date.now().toString(36) + Math.random().toString(36).slice(2, 10)
        var params = Object.assign({}, values, {step: "list", plan_token: planToken, urls: lines.join("\n"), library: stagingLibrary || values.library || ""})
        api.send("POST", "/api/downloads/run", {kind: kind.id, params: params}, listStarted)
    }
    function listStarted(job) { planJob = job; save({token: planToken, job: job.id}) }
    onInputChanged: if (kind.scheme && kind.scheme.listable) save({input: input})
    function restoreList() {
        var st = saved()
        input = st.input || ""
        if (!st.token) return
        planToken = st.token
        excluded = st.excluded || {}
        planJob = {id: st.job, running: true, done: 0, total: 0, step: "", tail: []}
        pollList()
    }
    function planLoaded(r) {
        // 视频方案：排名在前 N 之后的默认不勾（只在第一次取到列表时；之后按用户改过的）
        if (!saved().picked) {
            var m = Object.assign({}, excluded)
            ;(r.videos || []).forEach(function (v) { if (!v.picked) m[v.id] = true })
            excluded = m
            save({picked: true})
        }
        plan = r
    }
    function pollList() {
        api.getJSON("/api/downloads/jobs", function (list) {
            var job = list.find(function (j) { return panel.planJob && j.id === panel.planJob.id })
            if (job) panel.planJob = job
            if (!job || (!job.running && job.exit_code === 0))   // 任务不在了（服务端重启过）就直接取列表文件，取不到就作废
                api.send("GET", "/api/downloads/plan?token=" + panel.planToken, null, panel.planLoaded,
                         function () { panel.clearPlan() })
        })
    }
    Timer { interval: 1500; repeat: true; running: panel.listing; onTriggered: panel.pollList() }
    function toggle(id, on) { var m = Object.assign({}, excluded); if (on) delete m[id]; else m[id] = true; excluded = m }
    onExcludedChanged: if (planToken) save({excluded: excluded})
    function setAll(type, on) {
        var m = Object.assign({}, excluded)
        rows.forEach(function (r) { if (r.type === type) { if (on) delete m[r.id]; else m[r.id] = true } })
        excluded = m
    }
    function count(type) { return rows.filter(function (r) { return r.type === type && !panel.excluded[r.id] }).length }
    function views(n) { return n === null || n === undefined ? "播放量不明" : n >= 1e8 ? (n / 1e8).toFixed(1) + " 亿次" : n >= 1e4 ? Math.round(n / 1e4) + " 万次" : n + " 次" }
    // 列表的每一行：分组标题、要下的歌、MV、视频、其他网站链接、不下的、已下载过的
    readonly property var rows: {
        if (!plan) return []
        var out = []
        var who = function (x) { return (x.artists && x.artists.length ? x.artists.join(" / ") + " - " : "") + (x.title || x.id) }
        var songs = (plan.songs || []).slice().sort(function (a, b) { return a.path.localeCompare(b.path, undefined, {numeric: true}) })
        if (songs.length) out.push({type: "head", group: "song", text: "要下的歌 " + songs.length + " 首（放到 音声/音乐/ 下的位置）"})
        songs.forEach(function (x) { out.push({type: "song", id: x.id, text: x.path, note: x.source === "纯音轨" ? "" : x.source}) })
        var mvs = (plan.mvs || []).filter(function (m) { return !m.done })
        if (mvs.length) out.push({type: "head", group: "mv", text: "MV " + mvs.length + " 个（下成视频，放到 短视频/MV/<歌手>/）"})
        mvs.forEach(function (m) { out.push({type: "mv", id: m.id, text: m.title, note: m.artist}) })
        var videos = plan.videos || []
        if (videos.length) out.push({type: "head", group: "video", text: "候选视频 " + videos.length + " 个（按播放量排，前 " + (values.top || "N") + " 个默认勾上；放到 " + scheme.destination + "）"})
        videos.forEach(function (v) { out.push({type: "video", id: v.id, text: v.title, note: views(v.views) + " · " + v.channel}) })
        if ((plan.other || []).length) out.push({type: "head", text: "其他网站的链接 " + plan.other.length + " 个（不查纯音轨版，按原视频下音频）"})
        ;(plan.other || []).forEach(function (u) { out.push({type: "other", text: u}) })
        if (plan.off_topic) out.push({type: "head", text: "标题里没有要的团名 " + plan.off_topic + " 个（频道里别的艺人，不列出）"})
        if (plan.skipped.length) out.push({type: "head", text: "不下 " + plan.skipped.length + " 个"})
        plan.skipped.forEach(function (x) { out.push({type: "skip", text: who(x), note: x.reason || ""}) })
        var done = (plan.done || []).map(who).concat(mvs.length < (plan.mvs || []).length ? plan.mvs.filter(function (m) { return m.done }).map(function (m) { return "MV：" + m.title }) : [])
        var doneCount = done.length + (plan.done_count || 0)
        if (doneCount) out.push({type: "head", text: "已下载过 " + doneCount + " 个（跳过）"})
        done.forEach(function (t) { out.push({type: "done", text: t}) })
        return out
    }
    function pickedText() {
        var parts = []
        if (count("song")) parts.push("歌 " + count("song") + " 首")
        if (count("mv")) parts.push("MV " + count("mv") + " 个")
        if (count("video")) parts.push("视频 " + count("video") + " 个")
        return parts.join("、") || "没有勾选"
    }
    function startPlan() {
        var p = Object.assign({}, values, {plan: planToken, urls: lines.join("\n"), library: stagingLibrary || values.library || "", exclude: Object.keys(excluded).join(",")})
        panel.run(p)
        clearPlan()
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
            onTextChanged: if (text !== panel.input) { panel.input = text; analyzeTimer.restart(); if (panel.kind.id === "music") panel.clearPlan() }
            background: Rectangle { color: "#0d1117"; border.color: parent.activeFocus ? "#58a6ff" : "#30363d"; radius: 4 }
        }
    }

    // 选项（放不下自动换行）：怎么挑的两项放在最前面
    Flow {
        Layout.fillWidth: true
        spacing: 8
        Repeater {
            model: panel.kind.fields.filter(function (f) { return f.name !== "urls" && f.name !== "library" && (f.name !== "top" || panel.values.rank !== "none") })
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

    // 读取列表的进度与结果
    Label {
        visible: panel.listable && !!panel.planJob && !panel.plan
        Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12
        color: panel.listFailed ? "#f85149" : "#58a6ff"
        text: !panel.planJob ? "" : panel.listFailed ? "读取失败：" + (panel.planJob.tail || []).join(" ") + "（完整日志在「任务」里）"
            : panel.listing ? "正在读取列表（只查不下载；" + (panel.isMusic ? "歌手页要逐张读专辑，几十张要两三分钟" : "大频道的列表页要读一两分钟，没有播放量的还要逐个补") + "，可以先去别的页面，回来接着看）\n"
                              + (panel.planJob.step || "准备中（第一次要安装依赖）…")
                              + ((panel.planJob.tail || []).length ? "\n最近：" + panel.planJob.tail[panel.planJob.tail.length - 1] : "")
            : "读取完成，正在取列表…"
    }
    Rectangle {
        visible: panel.listable && !!panel.plan
        Layout.fillWidth: true
        implicitHeight: Math.min(460, planList.contentHeight + 12)
        color: "#0d1117"; border.color: "#30363d"; radius: 4
        ListView {
            id: planList
            anchors.fill: parent; anchors.margins: 6
            clip: true
            model: panel.rows
            WheelScroll { }
            ScrollBar.vertical: ScrollBar {}
            delegate: RowLayout {
                required property var modelData
                readonly property bool pickable: modelData.type === "song" || modelData.type === "mv" || modelData.type === "video"
                width: planList.width - 12
                spacing: 6
                CheckBox {
                    visible: parent.pickable
                    checked: parent.pickable && !panel.excluded[modelData.id]
                    onToggled: panel.toggle(modelData.id, checked)
                    padding: 0; implicitHeight: 24
                }
                Label {
                    Layout.fillWidth: true; elide: Text.ElideMiddle
                    topPadding: modelData.type === "head" ? 8 : 2; bottomPadding: 2
                    text: modelData.text
                    font.pixelSize: modelData.type === "head" ? 13 : 12; font.bold: modelData.type === "head"
                    color: modelData.type === "head" ? "#e6edf3" : modelData.type === "skip" || modelData.type === "done" ? "#6e7681"
                         : panel.excluded[modelData.id] ? "#6e7681" : "#c9d1d9"
                }
                Label {
                    visible: !!modelData.note
                    Layout.maximumWidth: 420; elide: Text.ElideRight
                    text: modelData.note || ""
                    font.pixelSize: 11; color: modelData.type === "skip" ? "#d29922" : "#8b949e"
                }
                Chip { visible: modelData.type === "head" && !!modelData.group; text: "全选"; onClicked: panel.setAll(modelData.group, true) }
                Chip { visible: modelData.type === "head" && !!modelData.group; text: "全不选"; onClicked: panel.setAll(modelData.group, false) }
            }
        }
    }

    // 会发生什么 + 开始
    RowLayout {
        Layout.fillWidth: true
        spacing: 8
        Label {
            Layout.fillWidth: true; wrapMode: Text.Wrap; font.pixelSize: 12; color: "#8b949e"
            text: !panel.lines.length ? "先在上面粘贴链接。"
                : panel.listable && panel.plan ? "按上面的列表一个一个下：" + panel.pickedText()
                  + ((panel.plan.other || []).length ? "，其他网站链接 " + panel.plan.other.length + " 个" : "") + "。去掉勾的不下；改了链接或选项要重新读取。"
                : panel.listable ? panel.lines.length + " 个链接。先「读取列表」看会下哪些、哪些不下，再开始下载；也可以不看直接下。"
                : panel.lines.length + " 个链接"
                  + (!panel.scheme.ranked ? "" : panel.values.rank === "none" ? "，每个全部下载"
                     : "，每个读一次列表页、按" + panel.optionLabel("rank") + "取" + panel.optionLabel("top") + "（排名表写进任务日志）"
                       + (panel.kind.id === "music" ? "；完整专辑整张下" : ""))
                  + "，排成一个任务在后台下完，下过的自动跳过。"
                  + (panel.kind.id === "music" ? "全部下载不筛选；每首的播放数、点赞、发布日期、下载日期和 Last.fm 信息（风格、听众、重播率）写进文件，播放时显示。" : "")
        }
        Chip { visible: panel.listable && !panel.plan; text: "不看列表，直接下载"; enabled: panel.ready && !panel.listing; onClicked: panel.start() }
        Chip { visible: panel.listable && !!panel.plan; text: "重新读取"; enabled: panel.ready; onClicked: panel.readList() }
        PrimaryButton {
            visible: panel.listable
            text: panel.plan ? "开始下载（" + panel.pickedText() + "）" : panel.listing ? "读取中…" : "读取列表"
            enabled: panel.ready && !panel.listing && (!panel.plan || panel.count("song") + panel.count("mv") + panel.count("video") + (panel.plan.other || []).length > 0)
            onClicked: panel.plan ? panel.startPlan() : panel.readList()
        }
        PrimaryButton { visible: !panel.listable; text: panel.lines.length ? "开始下载 " + panel.lines.length + " 个链接" : "开始下载"; enabled: panel.ready; onClicked: panel.start() }
    }
}

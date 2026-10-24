import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtCore

// 下载方案页（音乐、YouTube、B 站、X、其他链接）：按服务端给的方案说明生成。
//   去向 → 链接（一行一个）→（YouTube）模式 → 读取列表 → 勾选 → 开始。进度在「任务」里。
// 列表类方案先「读取列表」：服务端按下载时同样的规则列出会下什么、不下什么（音乐：换纯音轨版、查专辑、挑 MV；
// YouTube：按模式挑 MV 并按艺人分组 / 有声书按集 / 普通视频按播放量），去掉不要的勾，再按这份列表一个一个下。
ColumnLayout {
    id: panel
    required property var kind
    property string stagingLibrary: ""   // 暂存到哪个资源库：下载页顶部统一选
    signal run(var params)
    signal openTab(string tab)
    spacing: 8

    ApiClient { id: api }
    readonly property var scheme: Object.assign({steps: [], destination: "", input: "", placeholder: ""}, kind.scheme || {})
    property var values: ({})
    property string input: ""
    property bool showHelp: false
    property var analysis: ({items: []})

    function field(name) { return kind.fields.find(function (f) { return f.name === name }) || null }
    function set(name, value) {
        var v = Object.assign({}, values); v[name] = value; values = v
        if (name === "mode") { save({mode: value}); if (plan || planJob) clearPlan() }   // 换了模式，读出的列表就不对了
    }
    function optionLabel(name) { var f = field(name); var o = f ? f.options.find(function (x) { return x.value === values[name] }) : null; return o ? o.label : "" }
    function reset() {
        var v = {}
        kind.fields.forEach(function (f) { if (f.default !== undefined) v[f.name] = f.default })
        values = v; input = ""; analysis = {items: []}
        plan = null; planJob = null; planToken = ""; excluded = {}
        if (kind.scheme && kind.scheme.listable) {   // 不用 listable：换方案时这个绑定可能还没更新
            var mode = saved().mode   // 上次用的模式
            if (mode && field("mode")) { var v2 = Object.assign({}, values); v2.mode = mode; values = v2 }
            restoreList()
        }
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
    property string bookName: ""   // 有声书模式的书名（读出列表时填播放列表名，可以改）
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
    function clearPlan() { planToken = ""; planJob = null; plan = null; excluded = {}; save({token: "", job: "", excluded: {}, picked: ""}) }
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
        if (saved().picked !== planToken) {
            var m = Object.assign({}, excluded)
            ;(r.videos || []).forEach(function (v) { if (!v.picked) m[v.id] = true })
            excluded = m
            save({picked: planToken})
        }
        plan = r
        bookName = r.book || ""
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
    function setAll(group, on) {
        var m = Object.assign({}, excluded)
        var ids = group.indexOf("a:") === 0
            ? (plan.videos || []).filter(function (v) { return "a:" + v.group === group }).map(function (v) { return v.id })
            : group === "video" ? (plan.videos || []).map(function (v) { return v.id })
            : rows.filter(function (r) { return r.grp === group }).map(function (r) { return r.id })
        ids.forEach(function (id) { if (on) delete m[id]; else m[id] = true })
        excluded = m
    }
    function count(type) {
        if (type === "video")   // 按全部视频算（MV 模式收起的组不在 rows 里）
            return plan ? (plan.videos || []).filter(function (v) { return !panel.excluded[v.id] }).length : 0
        return rows.filter(function (r) { return r.type === type && !panel.excluded[r.id] }).length
    }
    property var expanded: ({})   // MV 模式展开了哪些艺人
    function expand(group) { var m = Object.assign({}, expanded); if (m[group]) delete m[group]; else m[group] = true; expanded = m }
    function views(n) { return n === null || n === undefined ? "播放量不明" : n >= 1e8 ? (n / 1e8).toFixed(1) + " 亿次" : n >= 1e4 ? Math.round(n / 1e4) + " 万次" : n + " 次" }
    // 列表的每一行：分组标题、要下的歌、MV、视频、其他网站链接、不下的、已下载过的
    readonly property var rows: {
        if (!plan) return []
        var out = []
        var who = function (x) { return (x.artists && x.artists.length ? x.artists.join(" / ") + " - " : "") + (x.title || x.id) }
        var songs = (plan.songs || []).slice().sort(function (a, b) { return a.path.localeCompare(b.path, undefined, {numeric: true}) })
        if (songs.length) out.push({type: "head", group: "song", text: "要下的歌 " + songs.length + " 首（放到 音声/音乐/ 下的位置）"})
        songs.forEach(function (x) { out.push({type: "song", grp: "song", id: x.id, text: x.path, note: x.source === "纯音轨" ? "" : x.source}) })
        var mvs = (plan.mvs || []).filter(function (m) { return !m.done })
        if (mvs.length) out.push({type: "head", group: "mv", text: "音乐 MV " + mvs.length + " 个（下成视频，放到 短视频/MV/<歌手>/）"})
        mvs.forEach(function (m) { out.push({type: "mv", grp: "mv", id: m.id, text: m.title, note: m.artist}) })
        var videos = plan.videos || []
        if (plan.mode === "girls" || plan.mode === "girlvideo") {
            // 一个艺人一行（勾选整组），点「展开」看每个视频；女团和女团成员在前，其余艺人收在「其他艺人」里（默认不勾）
            var last = null, inOthers = false
            var otherVideos = videos.filter(function (v) { return v.kind !== "girl" })
            videos.forEach(function (v) {
                var key = "a:" + v.group
                if (v.kind !== "girl" && !inOthers) {
                    inOthers = true
                    var nOther = Object.keys(otherVideos.reduce(function (m, x) { m[x.group] = 1; return m }, {})).length
                    out.push({type: "others", group: "others", text: "其他艺人 " + nOther + " 个、" + otherVideos.length + " 个视频（男团、其他歌手、Wikidata 查不到的，默认不勾）"})
                }
                if (inOthers && !panel.expanded["others"]) return
                if (v.group !== last) {
                    last = v.group
                    var mine = videos.filter(function (x) { return x.group === v.group })
                    var on = mine.filter(function (x) { return !panel.excluded[x.id] }).length
                    out.push({type: "artist", group: key, picked: on, total: mine.length,
                              text: v.group + "　" + (on ? on + " / " : "") + mine.length + " 个",
                              note: "放到 " + v.channel + (plan.mode === "girls" ? " / " + v.group : "")})
                }
                if (panel.expanded[key])
                    out.push({type: "video", grp: key, id: v.id, text: "　　" + v.title, note: views(v.views)})
            })
        } else if (videos.length) {
            out.push({type: "head", group: "video", text: (plan.mode === "audio" ? "共 " + videos.length + " 集，按列表顺序" : "视频 " + videos.length + " 个，按播放量排")
                      + (plan.pick_note ? "；" + plan.pick_note : "")})
            videos.forEach(function (v) {
                out.push({type: "video", grp: "video", id: v.id, text: (plan.mode === "audio" ? v.index + ". " : "") + v.title,
                          note: plan.mode === "audio" ? "" : views(v.views) + " · " + v.channel})
            })
        }
        if (plan.mode === "girls" || plan.mode === "girlvideo") {
            var girls = videos.filter(function (v) { return v.kind === "girl" })
            out.splice(0, 0, {type: "head", text: "女团和女团成员 " + Object.keys(girls.reduce(function (m, v) { m[v.group] = 1; return m }, {})).length + " 个、"
                              + (plan.mode === "girls" ? "正式 MV " : "视频 ") + girls.length + " 个（默认全勾）"})
        }
        if (plan.other_count) out.push({type: "head", text: "其他视频 " + plan.other_count + " 个（不是 MV，不列出）"})
        var months = plan.months || []   // 图文平台（X）：一行一个月，勾掉不要的月份
        if (months.length) out.push({type: "head", group: "month", text: "按月份（新的在上面；去掉买号前、刷量的月份）"})
        months.forEach(function (m) {
            out.push({type: "month", grp: "month", id: m.id, text: m.author + "　" + m.month + "　" + m.posts + " 条 · " + m.images + " 张" + (m.done ? "（已下 " + m.done + "）" : ""),
                      note: m.samples.join(" / ")})
        })
        if ((plan.other || []).length) out.push({type: "head", text: "其他网站的链接 " + plan.other.length + " 个（不查纯音轨版，按原视频下音频）"})
        ;(plan.other || []).forEach(function (u) { out.push({type: "other", text: u}) })
        if (plan.skipped.length) out.push({type: "head", text: (plan.mode === "girls" ? "带 MV 字样但不是正式 MV（预告、练习室等），不下 " : "不下 ") + plan.skipped.length + " 个"})
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
        if (count("mv")) parts.push("音乐 MV " + count("mv") + " 个")
        if (count("month")) parts.push(count("month") + " 个月")
        if (count("video")) parts.push((plan && plan.mode === "girls" ? "女团 MV " : plan && plan.mode === "girlvideo" ? "女团相关视频 " : plan && plan.mode === "audio" ? "音频 " : "视频 ") + count("video") + " 个")
        return parts.join("、") || "没有勾选"
    }
    function startPlan() {
        var p = Object.assign({}, values, {album: bookName, plan: planToken, urls: lines.join("\n"), library: stagingLibrary || values.library || "", exclude: Object.keys(excluded).join(",")})
        panel.run(p)
        clearPlan()
    }
    Timer { id: analyzeTimer; interval: 300; onTriggered: if (panel.kind.id === "other") api.send("POST", "/api/downloads/analyze", {urls: panel.input, mode: "video", library: panel.values.library || ""}, function (r) { panel.analysis = r }) }

    // 去向 + 说明（默认收起）
    RowLayout {
        Layout.fillWidth: true
        spacing: 6
        Label { text: "放到"; color: "#8b949e"; font.pixelSize: 12 }
        Label { text: (panel.scheme.mode_destinations || {})[panel.values.mode] || panel.scheme.destination; color: "#3fb950"; font.pixelSize: 14; font.bold: true }
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

    // 选项（放不下自动换行）。「模式」是一排按钮，选中的模式下面一行说明它会怎么做
    Flow {
        Layout.fillWidth: true
        spacing: 8
        Repeater {
            model: panel.kind.fields.filter(function (f) { return f.name !== "urls" && f.name !== "library" })
            delegate: Row {
                required property var modelData
                spacing: 4
                Label { text: modelData.label; color: "#8b949e"; font.pixelSize: 12; anchors.verticalCenter: parent.verticalCenter }
                Repeater {
                    model: modelData.type === "modes" ? modelData.options : []
                    delegate: Chip {
                        required property var modelData
                        text: modelData.label.split("：")[0]
                        selected: panel.values.mode === modelData.value
                        onClicked: panel.set("mode", modelData.value)
                    }
                }
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

    Label {
        readonly property var option: { var f = panel.field("mode"); return f ? (f.options || []).find(function (o) { return o.value === panel.values.mode }) : null }
        visible: !!option
        text: option ? option.label.split("：").slice(1).join("：") : ""
        color: "#6e7681"; font.pixelSize: 12
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
    RowLayout {
        visible: panel.listable && !!panel.plan && panel.plan.mode === "audio"
        spacing: 6
        Label { text: "书名（音声里的专辑名）"; color: "#8b949e"; font.pixelSize: 12 }
        TextField { implicitHeight: 30; Layout.preferredWidth: 360; font.pixelSize: 12; text: panel.bookName; onTextEdited: panel.bookName = text }
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
                readonly property bool pickable: modelData.type === "song" || modelData.type === "mv" || modelData.type === "video" || modelData.type === "month"
                readonly property bool artist: modelData.type === "artist"
                width: planList.width - 12
                spacing: 6
                CheckBox {
                    visible: parent.pickable
                    checked: parent.pickable && !panel.excluded[modelData.id]
                    onToggled: panel.toggle(modelData.id, checked)
                    padding: 0; implicitHeight: 24
                }
                CheckBox {   // 艺人：勾上 / 去掉整组（部分勾选时显示半选）
                    visible: parent.artist
                    tristate: true
                    checkState: !parent.artist ? Qt.Unchecked : modelData.picked === 0 ? Qt.Unchecked : modelData.picked === modelData.total ? Qt.Checked : Qt.PartiallyChecked
                    nextCheckState: function () { return checkState === Qt.Checked ? Qt.Unchecked : Qt.Checked }
                    onClicked: panel.setAll(modelData.group, checkState === Qt.Checked)
                    padding: 0; implicitHeight: 24
                }
                Label {
                    Layout.fillWidth: true; elide: Text.ElideMiddle
                    topPadding: modelData.type === "head" ? 8 : 2; bottomPadding: 2
                    text: modelData.text
                    font.pixelSize: modelData.type === "head" || modelData.type === "others" ? 13 : 12; font.bold: modelData.type === "head" || modelData.type === "others"
                    color: modelData.type === "head" || modelData.type === "artist" || modelData.type === "others" ? "#e6edf3" : modelData.type === "skip" || modelData.type === "done" ? "#6e7681"
                         : panel.excluded[modelData.id] ? "#6e7681" : "#c9d1d9"
                }
                Label {
                    visible: !!modelData.note
                    Layout.maximumWidth: 420; elide: Text.ElideRight
                    text: modelData.note || ""
                    font.pixelSize: 11; color: modelData.type === "skip" ? "#d29922" : "#8b949e"
                }
                Chip { visible: modelData.type === "artist" || modelData.type === "others"; text: panel.expanded[modelData.group] ? "收起" : "展开"; onClicked: panel.expand(modelData.group) }
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
                : panel.lines.length + " 个链接，排成一个任务在后台下完，下过的自动跳过。"
        }
        Chip { visible: panel.isMusic && !panel.plan; text: "不看列表，直接下载"; enabled: panel.ready && !panel.listing; onClicked: panel.start() }
        Chip { visible: panel.listable && !!panel.plan; text: "重新读取"; enabled: panel.ready; onClicked: panel.readList() }
        PrimaryButton {
            visible: panel.listable
            text: panel.plan ? "开始下载（" + panel.pickedText() + "）" : panel.listing ? "读取中…" : "读取列表"
            enabled: panel.ready && !panel.listing && (!panel.plan || panel.count("song") + panel.count("mv") + panel.count("video") + panel.count("month") + (panel.plan.other || []).length > 0)
            onClicked: panel.plan ? panel.startPlan() : panel.readList()
        }
        PrimaryButton { visible: !panel.listable; text: panel.lines.length ? "开始下载 " + panel.lines.length + " 个链接" : "开始下载"; enabled: panel.ready; onClicked: panel.start() }
    }
}

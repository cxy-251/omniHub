import QtQuick
import QtCore
import omniHub.Media 1.0

// 音声播放引擎：全局一份（Main.qml），切换页面不中断，窗口最小化也照常播放。
// 播放本机文件（资源库在本机），断点按账号存到服务端：听了不到 3 秒不记，离结尾不到 5 秒算听完。
// 分集按专辑内序号（0 起）向服务端逐条取，不在客户端保存整张列表。
// 模式：list 列表循环 / single 单曲循环 / random 随机。系统媒体控制（MPRIS）经 nextMedia/previousMedia、hasNext/hasPrevious 接入。
Item {
    id: engine
    visible: false
    width: 0; height: 0

    readonly property var speeds: [1.0, 1.25, 1.5, 1.75, 2.0, 0.75]
    readonly property var sleepChoices: [0, 15, 30, 45, 60]
    readonly property int skipBack: 10
    readonly property int skipForward: 30

    property string albumId: ""
    property string albumTitle: ""
    property var track: null          // {index, title, id, ext, offset, duration}
    property int position: -1         // 当前分集在专辑里的序号（0 起）
    property int total: 0
    property string error: ""
    property int sleepMinutes: 0      // 定时关闭档位（0 = 关）
    property double sleepAt: 0
    readonly property bool active: track !== null
    readonly property bool hasNext: total > 0 && (prefs.mode === "random" || total > 1)
    readonly property bool hasPrevious: position > 0
    readonly property alias player: mpv
    readonly property alias mode: prefs.mode
    readonly property alias rate: prefs.rate
    // 专辑目录里有 cover/folder 图时由 MPRIS 自己找到；这里不另给。
    property string artUrl: ""

    Settings {
        id: prefs
        category: "audio"
        property string mode: "list"
        property real rate: 1.0
        property real volume: 100
        property string lastAlbum: ""
        property string lastAlbumTitle: ""
        property int lastPosition: -1
    }

    ApiClient { id: api; onErrorChanged: engine.error = error }

    MpvItem {
        id: mpv
        audioOnly: true
        playbackRate: prefs.rate
        volume: prefs.volume
        onPlaybackEnded: engine.trackFinished()
        onPlayingChanged: if (!playing) engine.save()
    }

    // 播放中每 15 秒存一次断点；暂停、切集、关闭窗口时也存。
    Timer { interval: 15000; repeat: true; running: mpv.playing; onTriggered: engine.save() }
    // 定时关闭：到点暂停。
    Timer {
        interval: 5000; repeat: true; running: engine.sleepAt > 0
        onTriggered: if (Date.now() >= engine.sleepAt) { mpv.pause(); engine.sleepMinutes = 0; engine.sleepAt = 0 }
    }

    // 启动后恢复上次的专辑与分集（暂停状态，按播放才开始）。
    property bool restored: false
    function restore() {
        if (restored || !backendApi.connected || !backendApi.stateReady) return
        restored = true
        if (prefs.lastAlbum && prefs.lastPosition >= 0) open(prefs.lastAlbum, prefs.lastAlbumTitle, prefs.lastPosition, false)
    }
    Connections {
        target: backendApi
        function onLibrariesChanged() { engine.restore() }
        function onStatusChanged() { engine.restore() }
    }
    Component.onCompleted: restore()

    // 打开专辑第 pos 集；autoplay 为 false 时只加载、停在断点处。
    function open(album, title, pos, autoplay) {
        save()
        var path = "/api/media/audio/tracks?path=" + encodeURIComponent(album) + "&limit=1&offset=" + Math.max(0, pos)
        api.getJSON(path, function (list, xhr) {
            var count = parseInt(xhr.getResponseHeader("X-Total-Count") || "0")
            if (!list.length) { engine.error = "这张专辑没有可播放的分集"; return }
            engine.albumId = album
            engine.albumTitle = title
            engine.total = count
            engine.position = Math.max(0, pos)
            engine.load(list[0], autoplay)
        })
    }
    function load(t, autoplay) {
        var file = api.localPath(t.id)
        if (!file) { error = "资源库不在线"; return }
        track = t
        error = ""
        prefs.lastAlbum = albumId
        prefs.lastAlbumTitle = albumTitle
        prefs.lastPosition = position
        mpv.source = file
        // 有断点且没听完就接着放。
        if (t.offset > 3 && !(t.duration > 0 && t.offset >= t.duration - 5)) mpv.position = t.offset
        if (autoplay) mpv.play(); else mpv.pause()
        if (typeof mprisService !== "undefined") mprisService.bind(engine)
    }

    function save() {
        if (!track || !mpv.mediaLoaded) return
        var pos = mpv.position, dur = mpv.duration
        var done = dur > 0 && pos >= dur - 5
        if (pos < 3 && !done) return
        var offset = done ? dur : Math.floor(pos)
        if (track.offset === offset) return
        track.offset = offset
        if (dur > 0) track.duration = dur
        api.send("POST", "/api/media/audio/progress", {audio_id: track.id, offset: offset, duration: dur > 0 ? dur : 0}, null)
    }

    function togglePlay() { if (mpv.playing) mpv.pause(); else mpv.play() }
    function seekBy(seconds) { mpv.seekRelative(seconds) }
    function goTo(pos, autoplay) { open(albumId, albumTitle, pos, autoplay) }
    function nextMedia() {
        if (!total) return
        if (prefs.mode === "random" && total > 1) {
            var r = Math.floor(Math.random() * (total - 1))
            goTo(r >= position ? r + 1 : r, true)
        } else goTo((position + 1) % total, true)
    }
    function previousMedia() {
        if (mpv.position > 5 || position <= 0) mpv.position = 0
        else goTo(position - 1, true)
    }
    function trackFinished() {
        save()
        if (prefs.mode === "single") { mpv.position = 0; mpv.play() }
        else nextMedia()
    }
    function stop() {
        save()
        mpv.source = ""
        track = null
        position = -1
        sleepMinutes = 0; sleepAt = 0
        prefs.lastPosition = -1
    }
    function cycleRate() { prefs.rate = speeds[(speeds.indexOf(prefs.rate) + 1) % speeds.length] }
    function cycleMode() { var m = ["list", "single", "random"]; prefs.mode = m[(m.indexOf(prefs.mode) + 1) % m.length] }
    function cycleSleep() {
        sleepMinutes = sleepChoices[(sleepChoices.indexOf(sleepMinutes) + 1) % sleepChoices.length]
        sleepAt = sleepMinutes > 0 ? Date.now() + sleepMinutes * 60000 : 0
    }

    function formatTime(s) {
        s = Math.max(0, Math.floor(s || 0))
        var h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60
        return (h > 0 ? h + ":" + (m < 10 ? "0" : "") : "") + m + ":" + (sec < 10 ? "0" : "") + sec
    }
}

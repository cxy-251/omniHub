import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Window
import QtQuick.Dialogs
import omniHub.Media 1.0

Item {
    id: playerView
    property alias source: mpv.source
    property alias position: mpv.position
    property bool pauseRequested: mpv.pauseRequested
    property alias playlistEntries: queue.entries
    property alias playlistIndex: queue.currentIndex
    property string playlistError: ""
    property var audioTracks: mpv.tracks.filter(function(track) { return track.type === "audio" })
    property var subtitleTracks: [{id: -1, label: "关闭字幕", selected: false}].concat(mpv.tracks.filter(function(track) { return track.type === "sub" }))

    MediaQueue { id: queue }
    function restorePlayback(source, position, paused, entries, index) {
        if (entries && entries.length > 0 && queue.replace(entries, index)) mpv.source = queue.currentSource
        else { if (source.length > 0) queue.replace([source]); mpv.source = source }
        if (position > 0) mpv.position = position
        if (paused) mpv.pause()
    }
    function loadCurrent() {
        if (queue.currentSource === mpv.source) mpv.restart()
        else mpv.source = queue.currentSource
    }
    function openFiles(urls) {
        if (!urls || urls.length === 0 || urls.length > 256) { playlistError = "请选择 1–256 个媒体文件"; return false }
        if (!queue.replace(urls.map(function(url) { return url.toString() }))) return false
        playlistError = ""
        loadCurrent()
        return true
    }
    function openMedia(url) { return openFiles([url]) }
    function nextMedia() { if (queue.next()) loadCurrent() }
    function previousMedia() { if (queue.previous()) loadCurrent() }
    function selectedTrackIndex(model) {
        for (var index = 0; index < model.length; index++) if (model[index].selected) return index
        return model.length > 0 ? 0 : -1
    }
    function timeLabel(seconds) {
        var total = Math.max(0, Math.floor(seconds))
        return Math.floor(total / 60) + ":" + (total % 60 < 10 ? "0" : "") + total % 60
    }
    component TrackChoice: ComboBox {
        id: choice
        textRole: "label"
        delegate: ItemDelegate {
            required property var modelData
            required property int index
            width: choice.width
            highlighted: choice.highlightedIndex === index
            contentItem: Label { text: modelData.label; textFormat: Text.PlainText; elide: Text.ElideRight }
        }
    }
    FileDialog {
        id: mediaDialog
        title: "打开音视频"
        fileMode: FileDialog.OpenFiles
        nameFilters: ["音视频 (*.mp4 *.mkv *.webm *.avi *.mov *.m4v *.mp3 *.flac *.ogg *.m4a *.wav *.opus)", "所有文件 (*)"]
        onAccepted: playerView.openFiles(selectedFiles)
    }
    FileDialog {
        id: subtitleDialog
        title: "加载外部字幕"
        fileMode: FileDialog.OpenFile
        nameFilters: ["字幕 (*.srt *.ass *.ssa *.vtt)"]
        onAccepted: mpv.addSubtitle(selectedFile.toString())
    }
    // 控件浮在视频底部；播放中鼠标 2.5 秒不动自动隐藏，暂停或出错时常显。双击或 F 键切换全屏。
    property bool controlsShown: true
    readonly property bool controlsVisible: controlsShown || !mpv.playing || controlsHover.hovered
    function poke() { controlsShown = true; hideTimer.restart() }
    function toggleFullScreen() {
        var w = playerView.Window.window
        if (!w) return
        if (w.visibility === Window.FullScreen) w.showNormal(); else w.showFullScreen()
    }
    property real volumeBeforeMute: 100
    Timer { id: hideTimer; interval: 2500; onTriggered: playerView.controlsShown = false }
    HoverHandler { onPointChanged: playerView.poke() }
    focus: true
    Keys.onPressed: function (event) {
        if (event.key === Qt.Key_Space) { mpv.playing ? mpv.pause() : mpv.play() }
        else if (event.key === Qt.Key_F) toggleFullScreen()
        else if (event.key === Qt.Key_Escape && playerView.Window.window && playerView.Window.window.visibility === Window.FullScreen) playerView.Window.window.showNormal()
        else if (event.key === Qt.Key_Right) mpv.seekRelative(5)
        else if (event.key === Qt.Key_Left) mpv.seekRelative(-5)
        else return
        poke()
        event.accepted = true
    }

    MpvItem {
        id: mpv; anchors.fill: parent
        onPlaybackEnded: { if (queue.next()) playerView.loadCurrent(); else mpv.pause() }
    }
    Label {
        anchors.centerIn: parent
        width: parent.width * 0.8
        text: playlistError || queue.error || mpv.error || (typeof nativeResume !== "undefined" ? nativeResume.error : "") || (mpv.source.length === 0 ? "从文件管理器「打开方式 → omniHub」，或点下方文件夹按钮选择音视频" : "")
        textFormat: Text.PlainText
        color: playlistError || queue.error || mpv.error ? "#f85149" : "#8b949e"
        wrapMode: Text.Wrap
        horizontalAlignment: Text.AlignHCenter
        visible: text.length > 0
    }
    TouchControls {
        anchors.fill: parent
        anchors.bottomMargin: playerView.controlsVisible ? controls.height : 0
        playerItem: mpv
        onShowOsd: function(txt) { osdText.text = txt; osdBox.visible = true; osdTimer.restart() }
    }
    TapHandler { acceptedButtons: Qt.LeftButton; onDoubleTapped: playerView.toggleFullScreen() }
    Rectangle {
        id: osdBox
        visible: false
        anchors.centerIn: parent
        width: 180; height: 50; color: "#aa000000"; radius: 8
        Label { id: osdText; anchors.centerIn: parent; color: "#ffffff"; textFormat: Text.PlainText }
        Timer { id: osdTimer; interval: 1200; onTriggered: osdBox.visible = false }
    }
    Item {
        id: controls
        anchors.bottom: parent.bottom
        anchors.left: parent.left
        anchors.right: parent.right
        height: 84
        opacity: playerView.controlsVisible ? 1 : 0
        visible: opacity > 0
        Behavior on opacity { NumberAnimation { duration: 180 } }
        HoverHandler { id: controlsHover }
        Rectangle {
            anchors.fill: parent
            gradient: Gradient {
                GradientStop { position: 0; color: "#00000000" }
                GradientStop { position: 0.45; color: "#99000000" }
                GradientStop { position: 1; color: "#dd000000" }
            }
        }
        ColumnLayout {
            anchors.fill: parent
            anchors.leftMargin: 12; anchors.rightMargin: 12; anchors.bottomMargin: 6; anchors.topMargin: 18
            spacing: 0
            SeekBar {
                Layout.fillWidth: true
                from: 0; to: Math.max(1, mpv.duration); value: mpv.position
                enabled: mpv.duration > 0
                onMoved: { mpv.position = value; playerView.poke() }
            }
            RowLayout {
                Layout.fillWidth: true
                spacing: 2
                IconButton { glyph: "prev"; enabled: queue.currentIndex > 0; onClicked: playerView.previousMedia(); ToolTip.text: "上一项" }
                IconButton { glyph: mpv.playing ? "pause" : "play"; size: 42; enabled: mpv.source.length > 0; onClicked: mpv.playing ? mpv.pause() : mpv.play() }
                IconButton { glyph: "next"; enabled: queue.currentIndex >= 0 && queue.currentIndex + 1 < queue.entries.length; onClicked: playerView.nextMedia(); ToolTip.text: "下一项" }
                Label { text: playerView.timeLabel(mpv.position) + " / " + playerView.timeLabel(mpv.duration); color: "#e6edf3"; font.pixelSize: 13; Layout.leftMargin: 8 }
                Item { Layout.fillWidth: true }
                IconButton {
                    glyph: mpv.volume === 0 ? "mute" : "volume"; ToolTip.text: "静音"
                    onClicked: { if (mpv.volume > 0) { playerView.volumeBeforeMute = mpv.volume; mpv.volume = 0 } else mpv.volume = playerView.volumeBeforeMute }
                }
                SeekBar { Layout.preferredWidth: 90; from: 0; to: 100; value: mpv.volume; onMoved: mpv.volume = value }
                AbstractButton {
                    id: rateButton
                    readonly property var rates: [0.5, 0.75, 1, 1.25, 1.5, 2, 3]
                    implicitWidth: 44; implicitHeight: 36
                    hoverEnabled: true
                    contentItem: Label { text: mpv.playbackRate + "×"; color: "#e6edf3"; font.pixelSize: 13; horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter }
                    background: Rectangle { radius: 6; color: rateButton.hovered ? "#26ffffff" : "transparent" }
                    ToolTip.visible: hovered; ToolTip.text: "倍速（点击切换）"
                    onClicked: { var i = rates.indexOf(mpv.playbackRate); mpv.playbackRate = rates[(i + 1) % rates.length] }
                }
                IconButton { glyph: "subtitles"; enabled: mpv.duration > 0 || mpv.playing; onClicked: tracksDialog.open(); ToolTip.text: "音轨与字幕" }
                IconButton { glyph: "list"; enabled: queue.entries.length > 0; onClicked: playlistDialog.open(); ToolTip.text: "播放列表 " + (queue.currentIndex + 1) + "/" + queue.entries.length }
                IconButton { glyph: "folder"; onClicked: mediaDialog.open(); ToolTip.text: "打开文件" }
                IconButton {
                    glyph: playerView.Window.window && playerView.Window.window.visibility === Window.FullScreen ? "fullscreen_exit" : "fullscreen"
                    onClicked: playerView.toggleFullScreen(); ToolTip.text: "全屏（F）"
                }
            }
        }
    }
    Dialog {
        id: playlistDialog
        anchors.centerIn: parent
        title: "播放列表"
        width: Math.max(280, Math.min(playerView.width - 32, 600))
        height: Math.max(180, Math.min(playerView.height - 32, 420))
        modal: true
        standardButtons: Dialog.Close
        ListView {
            anchors.fill: parent
            clip: true
            model: queue.entries
            delegate: ItemDelegate {
                required property string modelData
                required property int index
                width: ListView.view.width
                highlighted: queue.currentIndex === index
                contentItem: Label { text: modelData; textFormat: Text.PlainText; elide: Text.ElideMiddle }
                onClicked: { if (queue.select(index)) playerView.loadCurrent(); playlistDialog.close() }
            }
        }
    }
    Dialog {
        id: tracksDialog
        anchors.centerIn: parent
        width: Math.max(280, Math.min(playerView.width - 32, 560))
        title: "音轨与字幕"
        modal: true
        standardButtons: Dialog.Close
        ColumnLayout {
            anchors.fill: parent
            Label { text: "音轨" }
            TrackChoice {
                Layout.fillWidth: true
                model: playerView.audioTracks
                currentIndex: playerView.selectedTrackIndex(playerView.audioTracks)
                enabled: model.length > 0
                onActivated: function(index) { mpv.selectAudioTrack(model[index].id) }
            }
            Label { text: "字幕" }
            TrackChoice {
                Layout.fillWidth: true
                model: playerView.subtitleTracks
                currentIndex: playerView.selectedTrackIndex(playerView.subtitleTracks)
                onActivated: function(index) { mpv.selectSubtitleTrack(model[index].id) }
            }
            Button { text: "加载外部字幕"; onClicked: subtitleDialog.open() }
            RowLayout {
                Label { text: "字幕延迟：" + mpv.subtitleDelay.toFixed(1) + " 秒" }
                Button { text: "−0.1s"; enabled: mpv.subtitleDelay > -60; onClicked: mpv.subtitleDelay = Math.max(-60, mpv.subtitleDelay - 0.1) }
                Button { text: "+0.1s"; enabled: mpv.subtitleDelay < 60; onClicked: mpv.subtitleDelay = Math.min(60, mpv.subtitleDelay + 0.1) }
                Button { text: "重置"; onClicked: mpv.subtitleDelay = 0 }
            }
        }
    }
}

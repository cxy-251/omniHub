import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import omniHub.Media 1.0

Item {
    id: playerView
    property alias source: mpv.source
    property alias position: mpv.position
    property bool pauseRequested: mpv.pauseRequested
    property var audioTracks: mpv.tracks.filter(function(track) { return track.type === "audio" })
    property var subtitleTracks: [{id: -1, label: "关闭字幕", selected: false}].concat(mpv.tracks.filter(function(track) { return track.type === "sub" }))

    function restorePlayback(source, position, paused) {
        mpv.source = source
        if (position > 0) mpv.position = position
        if (paused) mpv.pause()
    }
    function openMedia(url) { mpv.source = url.toString() }
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
        fileMode: FileDialog.OpenFile
        nameFilters: ["音视频 (*.mp4 *.mkv *.webm *.avi *.mov *.m4v *.mp3 *.flac *.ogg *.m4a *.wav *.opus)", "所有文件 (*)"]
        onAccepted: playerView.openMedia(selectedFile)
    }
    FileDialog {
        id: subtitleDialog
        title: "加载外部字幕"
        fileMode: FileDialog.OpenFile
        nameFilters: ["字幕 (*.srt *.ass *.ssa *.vtt)"]
        onAccepted: mpv.addSubtitle(selectedFile.toString())
    }
    MpvItem { id: mpv; anchors.fill: parent; anchors.bottomMargin: controls.height }
    Label {
        anchors.centerIn: parent
        width: parent.width * 0.8
        text: mpv.error || (mpv.source.length === 0 ? "点击“打开文件”选择音视频" : "")
        textFormat: Text.PlainText
        color: mpv.error ? "#f85149" : "#c9d1d9"
        wrapMode: Text.Wrap
        horizontalAlignment: Text.AlignHCenter
        visible: text.length > 0
    }
    TouchControls {
        anchors.fill: parent
        anchors.bottomMargin: controls.height
        playerItem: mpv
        onShowOsd: function(txt) { osdText.text = txt; osdBox.visible = true; osdTimer.restart() }
    }
    Rectangle {
        id: osdBox
        visible: false
        anchors.centerIn: parent
        width: 180; height: 50; color: "#aa000000"; radius: 8
        Label { id: osdText; anchors.centerIn: parent; color: "#ffffff"; textFormat: Text.PlainText }
        Timer { id: osdTimer; interval: 1200; onTriggered: osdBox.visible = false }
    }
    Rectangle {
        id: controls
        anchors.bottom: parent.bottom
        anchors.left: parent.left
        anchors.right: parent.right
        height: 108
        color: "#ee161b22"
        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 8
            spacing: 4
            RowLayout {
                Layout.fillWidth: true
                Button { text: mpv.playing ? "暂停" : "播放"; enabled: mpv.source.length > 0; onClicked: mpv.playing ? mpv.pause() : mpv.play() }
                Slider { Layout.fillWidth: true; from: 0; to: Math.max(1, mpv.duration); value: mpv.position; enabled: mpv.duration > 0; onMoved: mpv.position = value }
                Label { text: playerView.timeLabel(mpv.position) + " / " + playerView.timeLabel(mpv.duration); color: "#e6edf3" }
            }
            RowLayout {
                Layout.fillWidth: true
                Button { text: "打开文件"; onClicked: mediaDialog.open() }
                Button { text: "音轨 / 字幕"; enabled: mpv.duration > 0 || mpv.playing; onClicked: tracksDialog.open() }
                Label { text: "音量"; color: "#e6edf3" }
                Slider { Layout.fillWidth: true; Layout.maximumWidth: 180; from: 0; to: 100; value: mpv.volume; onMoved: mpv.volume = value }
                ComboBox {
                    Layout.preferredWidth: 100
                    model: [0.5, 0.75, 1, 1.25, 1.5, 2, 3]
                    currentIndex: model.indexOf(mpv.playbackRate)
                    displayText: mpv.playbackRate + "×"
                    onActivated: function(index) { mpv.playbackRate = model[index] }
                }
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

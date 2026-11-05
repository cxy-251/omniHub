import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

RowLayout {
    id: feedback
    required property var engine
    property string trackId: ""
    readonly property int rating: engine.musicRatings[trackId] || 0
    visible: trackId.indexOf(":media_library/audio/music/") >= 0
    spacing: 4
    Chip { text: feedback.rating === 1 ? "♥ 已赞" : "♡ 点赞"; selected: feedback.rating === 1; enabled: feedback.engine.musicCanRate
        onClicked: feedback.engine.rateMusic(feedback.trackId, feedback.rating === 1 ? 0 : 1) }
    Chip { text: feedback.rating === -1 ? "已不喜欢" : "不喜欢"; selected: feedback.rating === -1; enabled: feedback.engine.musicCanRate
        onClicked: feedback.engine.rateMusic(feedback.trackId, feedback.rating === -1 ? 0 : -1) }
}

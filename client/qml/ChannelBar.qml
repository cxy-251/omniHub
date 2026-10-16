import QtQuick
import QtQuick.Controls

// ChannelBar：频道条（横向滚动）。推荐 | ♥ 我的点赞 | 额外筛选 | 📌 固定的分组 | 其余分组 | 回收站（管理员）。
ListView {
    id: bar
    required property var feed           // FeedApi
    property string channel: "recommend"
    property var filters: []             // 额外筛选 [{channel, label}]，如短视频的平台
    property bool showHidden: false
    signal picked(string channel)

    WheelScroll { horizontal: true }
    implicitHeight: 28
    orientation: ListView.Horizontal
    spacing: 4
    clip: true
    model: {
        var list = [{channel: "recommend", label: "推荐"}, {channel: "liked", label: "♥ 我的点赞"}].concat(filters)
        var groups = feed.groups
        feed.pins.forEach(function (id) {
            var g = groups.find(function (x) { return x.id === id })
            if (g) list.push({channel: "group:" + g.id, label: "📌 " + g.label + " · " + g.count})
        })
        groups.forEach(function (g) {
            if (feed.pins.indexOf(g.id) < 0) list.push({channel: "group:" + g.id, label: g.label + " · " + g.count})
        })
        if (showHidden) list.push({channel: "hidden", label: "🗑 回收站"})
        return list
    }
    delegate: Chip {
        required property var modelData
        text: modelData.label
        selected: bar.channel === modelData.channel
        onClicked: bar.picked(modelData.channel)
    }
}

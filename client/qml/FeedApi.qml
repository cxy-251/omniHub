import QtQuick

// FeedApi：短视频、相册共用的推荐与互动接口（/api/feed/<domain>/...）。
// 频道：recommend 推荐 | liked 我的点赞 | hidden 回收站 | group:<分组> | tag:<标签>（如 tag:platform:抖音）。
// 行为上报：view 展示过；complete 看完；dwell 停留较久；skip 很快划走 —— 服务端据此调整推荐。
QtObject {
    id: feed
    required property string domain
    property var groups: []           // [{id, label, count, platform?}]
    property var pins: []             // 固定的分组 ID，按固定先后
    property string error: ""
    // 本次浏览的推荐种子：同一种子顺序稳定（翻页、返回不乱），「换一批」时换种子。
    property int seed: Math.floor(Math.random() * 1e9)
    property ApiClient api: ApiClient { onErrorChanged: feed.error = error }

    function path(action) { return "/api/feed/" + domain + "/" + action }
    function itemsPath(channel, extra) {
        return path("items") + "?channel=" + encodeURIComponent(channel || "") + "&seed=" + seed + (extra || "")
    }
    function loadGroups() {
        api.getJSON(path("groups"), function (r) { feed.groups = r.groups; feed.pins = r.pins })
    }
    function reshuffle() { seed = Math.floor(Math.random() * 1e9) }
    function like(item, liked, done) {
        api.send("POST", path("like"), {id: item.id, liked: liked}, function () { if (done) done(liked) })
    }
    function event(item, type) {
        if (item && item.id) api.send("POST", path("event"), {id: item.id, type: type}, null)
    }
    function trash(item, done) { api.send("POST", path("trash"), {id: item.id}, function () { if (done) done() }) }
    function restore(item, done) { api.send("POST", path("restore"), {id: item.id}, function () { if (done) done() }) }
    function setPinned(group, on) {
        api.send("POST", path("pin"), {group: group, pinned: on}, function (list) { feed.pins = list })
    }
    function groupLabel(id) {
        var g = groups.find(function (x) { return x.id === id })
        return g ? g.label : id
    }
}

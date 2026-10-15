import QtQuick

// 短视频接口的请求助手：短视频页与多联放映（含独立窗口）共用。
QtObject {
    id: api
    property string error: ""

    // method 为 GET 时 body 传 null；done(结果, xhr) 只在 2xx 时调用。
    function send(method, path, body, done) {
        var url = backendApi.apiUrl(path)
        if (!url) { api.error = "无法连接 omniHub 服务"; return }
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function () {
            if (xhr.readyState !== XMLHttpRequest.DONE || !api) return   // 所属页面已销毁
            if (xhr.status < 200 || xhr.status >= 300) { api.error = "请求失败（" + xhr.status + "）：" + xhr.responseText; return }
            api.error = ""
            if (done) done(xhr.responseText ? JSON.parse(xhr.responseText) : null, xhr)
        }
        xhr.open(method, url)
        if (body !== null && body !== undefined) {
            xhr.setRequestHeader("Content-Type", "application/json")
            xhr.send(JSON.stringify(body))
        } else xhr.send()
    }
    function getJSON(path, done) { send("GET", path, null, done) }

    function libraryRoot(id) {
        var libs = backendApi.libraries
        for (var i = 0; i < libs.length; i++) if (libs[i].id === id) return libs[i].root_path
        return ""
    }
    // 磁盘文件的绝对路径：资源库根目录 + 分类目录 + 作品相对路径。
    function localFile(post, rel) {
        var root = libraryRoot(post.library_id)
        return root ? root + "/media_library/shortvideo/" + rel : ""
    }
}

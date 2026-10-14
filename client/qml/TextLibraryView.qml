import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import omniHub.Readers 1.0

Item {
    TextReader { id: reader; objectName: "textReader" }
    Component.onCompleted: reader.list()
    function flush() { reader.flush() }
    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 12
        RowLayout {
            Layout.fillWidth: true
            Label { text: reader.reading ? reader.title : "文本书架"; textFormat: Text.PlainText; elide: Text.ElideRight; Layout.fillWidth: true; color: "#e6edf3" }
            Button { text: "从头阅读"; visible: reader.reading; enabled: !reader.loading; onClicked: reader.restart() }
            Button { text: reader.reading ? "返回书架" : "刷新"; onClicked: reader.reading ? reader.closeBook() : reader.list(reader.offset) }
        }
        Label { text: reader.error; visible: text.length>0; textFormat: Text.PlainText; color: "#f85149"; wrapMode: Text.Wrap; Layout.fillWidth: true }
        BusyIndicator { running: reader.loading; visible: running; Layout.alignment: Qt.AlignHCenter }
        ListView {
            visible: !reader.reading
            Layout.fillWidth: true; Layout.fillHeight: true
            model: reader.books; clip: true
            delegate: ItemDelegate {
                required property var modelData
                width: ListView.view.width
                contentItem: Label { text: modelData.name+" · "+modelData.library_name; textFormat: Text.PlainText; elide: Text.ElideRight; color: "#e6edf3" }
                onClicked: reader.open(modelData.rel_path)
            }
        }
        ScrollView {
            id: textScroll
            contentWidth: availableWidth
            visible: reader.reading
            Layout.fillWidth: true; Layout.fillHeight: true
            clip: true
            TextArea {
                objectName: "textContent"
                text: reader.content
                onTextChanged: textScroll.ScrollBar.vertical.position = 0
                textFormat: TextEdit.PlainText
                readOnly: true; selectByMouse: true
                wrapMode: TextEdit.Wrap
                font.pixelSize: 20
                color: "#e6edf3"
                background: Rectangle { color: "#161b22" }
            }
        }
        RowLayout {
            Layout.alignment: Qt.AlignHCenter
            Button { text: "上一页"; enabled: !reader.loading&&(reader.reading?reader.canPrevious:reader.offset>0); onClicked: reader.reading?reader.previous():reader.list(Math.max(0,reader.offset-24)) }
            Label { text: reader.reading?"起始行 "+reader.startLine:"第 "+(Math.floor(reader.offset/24)+1)+" 页"; color: "#c9d1d9" }
            Button { text: "下一页"; enabled: !reader.loading&&(reader.reading?reader.canNext:reader.hasMore); onClicked: reader.reading?reader.next():reader.list(reader.offset+24) }
        }
    }
}

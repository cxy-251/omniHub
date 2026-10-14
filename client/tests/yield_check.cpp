// Real process handoff check: the same executable runs GUI, Core observer and
// restored GUI branches against a caller-owned service/session.
#include "native_lifecycle.h"
#include "backend_api.h"
#include <QGuiApplication>
#include <QCoreApplication>
#include <QQuickWindow>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSaveFile>
#include <QDir>
#include <QTimer>

static bool record(const QString &name, const QJsonObject &data) {
    QSaveFile output(QDir(BackendApi::configurationDirectory()).filePath(name));
    if (!output.open(QIODevice::WriteOnly)) return false;
    const auto bytes = QJsonDocument(data).toJson(QJsonDocument::Compact);
    return output.write(bytes) == bytes.size() && output.commit();
}
int main(int argc,char **argv) {
    if (argc == 3 && QString::fromLocal8Bit(argv[1]) == "--watch-game") return NativeLifecycle::watchGame(argc,argv);
    QGuiApplication app(argc,argv);
    const bool restore = argc > 1 && QString::fromLocal8Bit(argv[1]) == "--resume-ui";
    NativeLifecycle lifecycle(restore);
    QQuickWindow window;
    window.resize(640,360);window.show();
    if (restore) {
        const auto data = lifecycle.restored();
        if (data.value("source").toString() != "fixture" || data.value("position").toDouble() != 4.25 || !data.value("paused").toBool() || data.value("page").toInt() != 1 || data.value("entries").toStringList() != QStringList({"fixture", "fixture"}) || data.value("index").toInt() != 1 || !lifecycle.error().isEmpty()) return 12;
        if (!record("yield-restored.json",QJsonObject{{"restored",true},{"pid",QCoreApplication::applicationPid()}})) return 13;
        QTimer::singleShot(100,&app,&QCoreApplication::quit);
    } else {
        if (argc != 2) return 11;
        QObject::connect(&lifecycle,&NativeLifecycle::observerStarted,&app,[&](qint64 pid){
            if (!record("yield-observer.json",QJsonObject{{"observer_pid",pid},{"gui_pid",QCoreApplication::applicationPid()}})) app.exit(14);
        });
        QObject::connect(&lifecycle,&NativeLifecycle::errorChanged,&app,[&](){app.exit(15);});
        QTimer::singleShot(300,&app,[&](){lifecycle.yieldToGame(QString::fromLocal8Bit(argv[1]),{{"source","fixture"},{"position",4.25},{"paused",true},{"page",1},{"entries",QStringList{"fixture","fixture"}},{"index",1}});});
    }
    return app.exec();
}

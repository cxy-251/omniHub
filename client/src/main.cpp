#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include "player/mpv_item.h"
#include "player/mpris.h"
#include "games/game_launcher.h"

int main(int argc, char *argv[])
{
    qputenv("QT_QUICK_CONTROLS_STYLE", "Basic");
    QGuiApplication app(argc, argv);
    app.setOrganizationName("omniHub");
    app.setApplicationName("omnihub_client");

    // 注册 QML 原生类型
    qmlRegisterType<MpvItem>("omniHub.Media", 1, 0, "MpvItem");
    qmlRegisterType<GameLauncher>("omniHub.Games", 1, 0, "GameLauncher");

    QQmlApplicationEngine engine;

    // 注册 Linux MPRIS 媒体控制服务
    MprisService mprisService;
    engine.rootContext()->setContextProperty("mprisService", &mprisService);

    // 处理外部打开文件命令行参数 (第 9.3 节)
    QString externalFile;
    if (argc > 1) {
        externalFile = QString::fromLocal8Bit(argv[1]);
    }
    engine.rootContext()->setContextProperty("initialExternalFile", externalFile);

    const QUrl url(QStringLiteral("qrc:/qml/Main.qml"));
    QObject::connect(&engine, &QQmlApplicationEngine::objectCreated,
                     &app, [url](QObject *obj, const QUrl &objUrl) {
        if (!obj && url == objUrl)
            QCoreApplication::exit(-1);
    }, Qt::QueuedConnection);

    engine.load(url);

    return app.exec();
}

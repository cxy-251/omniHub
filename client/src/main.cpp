#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QSGRendererInterface>
#include <QEvent>
#include "player/mpv_item.h"
#ifdef Q_OS_LINUX
#include "player/mpris.h"
#endif
#include "games/game_launcher.h"
#include "backend_api.h"
#include "native_lifecycle.h"
#include "player/media_queue.h"
#include "player/native_resume.h"
#include "host/external_router.h"
#include "readers/text_reader.h"

int main(int argc, char *argv[])
{
    if (argc > 1 && QString::fromLocal8Bit(argv[1]) == "--watch-game")
        return NativeLifecycle::watchGame(argc, argv);
    const bool restore = argc > 1 && QString::fromLocal8Bit(argv[1]) == "--resume-ui";
    qputenv("QT_QUICK_CONTROLS_STYLE", "Basic");
    QQuickWindow::setGraphicsApi(QSGRendererInterface::OpenGL);
    QGuiApplication app(argc, argv);
    app.setOrganizationName("omniHub");
    app.setApplicationName("omnihub_client");
    QStringList externalFiles;
    if (argc > 1 && !restore) {
        for (int index=1;index<argc;++index) {
            const QString argument=QString::fromLocal8Bit(argv[index]);
            if(index==1&&argument=="--")continue;
            externalFiles.append(argument);
        }
    }
    ExternalRouter externalRouter;
    if (!externalFiles.isEmpty()) {
        const int disposition=externalRouter.start(externalFiles);
        if (disposition<=0) { if(disposition<0)fprintf(stderr,"%s\n",externalRouter.error().toUtf8().constData());return disposition<0?2:0; }
        QStringList normalized;
        if(!ExternalRouter::localFiles(externalFiles,normalized)){fprintf(stderr,"External media changed during startup\n");return 2;}
        externalFiles=normalized;
    }

    // 注册 QML 原生类型
    qmlRegisterType<MpvItem>("omniHub.Media", 1, 0, "MpvItem");
    qmlRegisterType<GameLauncher>("omniHub.Games", 1, 0, "GameLauncher");
    qmlRegisterType<MediaQueue>("omniHub.Media", 1, 0, "MediaQueue");
    qmlRegisterType<TextReader>("omniHub.Readers", 1, 0, "TextReader");

    int result = 0;
    {
        BackendApi backend;
        NativeLifecycle lifecycle(restore);
        NativeResume resume;
        QObject::connect(&backend,&BackendApi::statusChanged,&resume,[&]{if(backend.connected()&&backend.hasAdmin())resume.refresh();});
        QObject::connect(&lifecycle,&NativeLifecycle::observerStarted,&resume,[&](qint64){resume.flush();});
#ifdef Q_OS_LINUX
        MprisService mprisService(nullptr,externalFiles.isEmpty()?"org.mpris.MediaPlayer2.omnihub":"org.mpris.MediaPlayer2.omnihub.external");
#endif
        QQmlApplicationEngine engine;
        engine.rootContext()->setContextProperty("backendApi", &backend);
        engine.rootContext()->setContextProperty("nativeLifecycle", &lifecycle);
        engine.rootContext()->setContextProperty("nativeResume", &resume);

        // 注册 Linux MPRIS 媒体控制服务
#ifdef Q_OS_LINUX
        engine.rootContext()->setContextProperty("mprisService", &mprisService);
#endif

        // 处理外部打开文件命令行参数 (第 9.3 节)
        QString externalFile;
        if (argc > 1 && !restore) {
            externalFile = QString::fromLocal8Bit(argv[1]);
        }
        engine.rootContext()->setContextProperty("initialExternalFile", externalFile);
        engine.rootContext()->setContextProperty("initialExternalFiles", externalFiles);

        const QUrl url(externalFiles.isEmpty()?QStringLiteral("qrc:/qml/Main.qml"):QStringLiteral("qrc:/qml/ExternalPlayerWindow.qml"));
        QObject::connect(&engine, &QQmlApplicationEngine::objectCreated,
                         &app, [url](QObject *obj, const QUrl &objUrl) {
            if (!obj && url == objUrl)
                QCoreApplication::exit(-1);
        }, Qt::QueuedConnection);

        engine.load(url);
        if(!externalFiles.isEmpty()&&!engine.rootObjects().isEmpty()){
            auto *window=engine.rootObjects().first();
            QObject::connect(&externalRouter,&ExternalRouter::filesRequested,window,[window](const QStringList &files){
                const QVariant values=files;
                if(!QMetaObject::invokeMethod(window,"openFiles",Q_ARG(QVariant,values)))qWarning("External file routing failed");
            });
        }
#ifdef Q_OS_LINUX
        if (!engine.rootObjects().isEmpty()) mprisService.setWindow(qobject_cast<QQuickWindow *>(engine.rootObjects().first()));
#endif

        result = app.exec();
    }
    // Renderers may release their final core reference during engine teardown.
    QCoreApplication::sendPostedEvents(nullptr, QEvent::DeferredDelete);
    return result;
}

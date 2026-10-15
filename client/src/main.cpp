#include <QGuiApplication>
#include <clocale>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QSGRendererInterface>
#include <QEvent>
#include <QPalette>
#include <QTimer>
#include <algorithm>
#include "player/mpv_item.h"
#ifdef Q_OS_LINUX
#include "player/mpris.h"
#endif
#include "games/game_launcher.h"
#include "backend_api.h"
#include "local_network.h"
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
    // 统一控件风格：Fusion + 深色调色板（默认 Basic 样式在深色界面里是浅灰按钮，文字也看不清）。
    if (qEnvironmentVariableIsEmpty("QT_QUICK_CONTROLS_STYLE")) qputenv("QT_QUICK_CONTROLS_STYLE", "Fusion");
    QGuiApplication app(argc, argv);
    {
        QPalette dark;
        const QColor window(0x0d, 0x11, 0x17), base(0x16, 0x1b, 0x22), text(0xc9, 0xd1, 0xd9), accent(0x58, 0xa6, 0xff);
        dark.setColor(QPalette::Window, window);
        dark.setColor(QPalette::WindowText, text);
        dark.setColor(QPalette::Base, base);
        dark.setColor(QPalette::AlternateBase, window);
        dark.setColor(QPalette::Text, text);
        dark.setColor(QPalette::Button, QColor(0x21, 0x26, 0x2d));
        dark.setColor(QPalette::ButtonText, text);
        dark.setColor(QPalette::Highlight, accent);
        dark.setColor(QPalette::HighlightedText, QColor(0x0d, 0x11, 0x17));
        dark.setColor(QPalette::ToolTipBase, base);
        dark.setColor(QPalette::ToolTipText, text);
        dark.setColor(QPalette::PlaceholderText, QColor(0x8b, 0x94, 0x9e));
        dark.setColor(QPalette::Mid, QColor(0x30, 0x36, 0x3d));
        dark.setColor(QPalette::Dark, QColor(0x0b, 0x0e, 0x13));
        dark.setColor(QPalette::Light, QColor(0x30, 0x36, 0x3d));
        dark.setColor(QPalette::Disabled, QPalette::ButtonText, QColor(0x6e, 0x76, 0x81));
        dark.setColor(QPalette::Disabled, QPalette::Text, QColor(0x6e, 0x76, 0x81));
        dark.setColor(QPalette::Disabled, QPalette::WindowText, QColor(0x6e, 0x76, 0x81));
        QGuiApplication::setPalette(dark);
    }
    // libmpv 要求 LC_NUMERIC 为 "C"，否则 mpv_create 失败；QGuiApplication 会按系统区域改写它。
    std::setlocale(LC_NUMERIC, "C");
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
        LocalNetworkFactory networkFactory; // 必须比 engine 活得久
        QQmlApplicationEngine engine;
        engine.setNetworkAccessManagerFactory(&networkFactory);
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

        // 开发验证用：OMNIHUB_SNAPSHOT=<png 路径>:<秒数>，到时把主窗口画面存成 PNG 后退出。
        // 配合 QT_QPA_PLATFORM=offscreen，可以在不打扰桌面的情况下检查界面。
        const QString snapshot = qEnvironmentVariable("OMNIHUB_SNAPSHOT");
        if (!snapshot.isEmpty() && !engine.rootObjects().isEmpty()) {
            const int split = snapshot.lastIndexOf(':');
            const QString file = snapshot.left(split);
            const int seconds = split > 0 ? snapshot.mid(split + 1).toInt() : 5;
            auto *window = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
            QTimer::singleShot(std::max(1, seconds) * 1000, &app, [window, file] {
                if (window && !window->grabWindow().save(file)) fprintf(stderr, "snapshot failed: %s\n", qPrintable(file));
                QCoreApplication::quit();
            });
        }
        result = app.exec();
    }
    // Renderers may release their final core reference during engine teardown.
    QCoreApplication::sendPostedEvents(nullptr, QEvent::DeferredDelete);
    return result;
}

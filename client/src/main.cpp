#include <QGuiApplication>
#ifdef __GLIBC__
#include <malloc.h>
#endif
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
#include "backend_api.h"
#include "local_network.h"
#include "player/media_queue.h"
#include "player/native_resume.h"
#include "host/external_router.h"

int main(int argc, char *argv[])
{
#ifdef __GLIBC__
    // 图片解码/视频帧会反复分配大小不一的大块内存。glibc 默认的 mmap 阈值会动态上调，
    // 之后大块改从堆里分配，释放后留在进程里不还给系统，浏览原图时内存只涨不降。
    // 固定阈值让大块走 mmap、释放即归还；限制 arena 数量减少多线程碎片。
    mallopt(M_MMAP_THRESHOLD, 256 * 1024);
    mallopt(M_ARENA_MAX, 2);
#endif
    qputenv("QT_QUICK_CONTROLS_STYLE", "Basic");
    QQuickWindow::setGraphicsApi(QSGRendererInterface::OpenGL);
    // 统一控件风格：Fusion + 深色调色板（默认 Basic 样式在深色界面里是浅灰按钮，文字也看不清）。
    if (qEnvironmentVariableIsEmpty("QT_QUICK_CONTROLS_STYLE")) qputenv("QT_QUICK_CONTROLS_STYLE", "Fusion");
    // 不使用 X11 会话管理（XSMP）。经 XWayland 运行时 Wayland 桌面没有会话管理服务，
    // Qt 会报 "Session management error: Could not open network socket"。
    qunsetenv("SESSION_MANAGER");
#ifdef Q_OS_LINUX
    // xcb 默认用 GLX；mpv 的 VA-API 显卡直出（解码帧不拷回内存）只能在 EGL 上做。
    // 实测三路 4K HEVC：拷回方式每路只有约 24 帧/秒（卡顿），直出约 41 帧/秒且几乎不占 CPU。
    if (qEnvironmentVariableIsEmpty("QT_XCB_GL_INTEGRATION")) qputenv("QT_XCB_GL_INTEGRATION", "xcb_egl");
#endif
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
    if (argc > 1) {
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
    qmlRegisterType<MediaQueue>("omniHub.Media", 1, 0, "MediaQueue");

    int result = 0;
    {
        BackendApi backend;
        NativeResume resume;
        QObject::connect(&backend,&BackendApi::statusChanged,&resume,[&]{if(backend.connected()&&backend.hasAdmin())resume.refresh();});
#ifdef Q_OS_LINUX
        MprisService mprisService(nullptr,externalFiles.isEmpty()?"org.mpris.MediaPlayer2.omnihub":"org.mpris.MediaPlayer2.omnihub.external");
#endif
        LocalNetworkFactory networkFactory; // 必须比 engine 活得久
        QQmlApplicationEngine engine;
        engine.setNetworkAccessManagerFactory(&networkFactory);
        engine.rootContext()->setContextProperty("backendApi", &backend);
        engine.rootContext()->setContextProperty("nativeResume", &resume);
        // 开发验证用：OMNIHUB_DEV_SCROLL=<像素> 让列表每 100ms 自动下滚，用于测量滚动时的内存。
        engine.rootContext()->setContextProperty("devScroll", qEnvironmentVariableIntValue("OMNIHUB_DEV_SCROLL"));
        // 开发验证：OMNIHUB_DEV_MATRIX=<秒> 打开多联放映并全部播放，每隔这么多秒打印各屏丢帧统计。
        engine.rootContext()->setContextProperty("devMatrix", qEnvironmentVariableIntValue("OMNIHUB_DEV_MATRIX"));

        // 注册 Linux MPRIS 媒体控制服务
#ifdef Q_OS_LINUX
        engine.rootContext()->setContextProperty("mprisService", &mprisService);
#endif

        // 带文件参数时打开外部播放窗口（规格 9.3），文件列表在 externalFiles。
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

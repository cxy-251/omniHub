#include <QGuiApplication>
#ifdef __GLIBC__
#include <malloc.h>
#endif
#include <clocale>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QOpenGLContext>
#include <atomic>
#include <QSGRendererInterface>
#include <QEvent>
#include <QPalette>
#include <QTimer>
#include <QKeyEvent>
#include <QKeySequence>
#include <QElapsedTimer>
#include <QNetworkProxyFactory>
#include <algorithm>
#include "player/mpv_item.h"
#include "gallery/masonry_layout.h"
#ifdef Q_OS_LINUX
#include "player/mpris.h"
#endif
#include "backend_api.h"
#include "local_network.h"
#include "player/media_queue.h"
#include "player/native_resume.h"
#include "host/external_router.h"

// 开发验证：OMNIHUB_DEV_STARTUP=1 时打印启动各阶段距进程开始的毫秒数。
static QElapsedTimer startupClock;
static void startupMark(const char *what)
{
    if (qEnvironmentVariableIsSet("OMNIHUB_DEV_STARTUP")) fprintf(stderr, "startup %6lld ms  %s\n", startupClock.elapsed(), what);
}

// 窗口刚显示、还没被窗口管理器画到屏幕上时，开着垂直同步的 eglSwapBuffers 会等将近 1 秒才放行，
// 主线程同步时跟着卡住，整个界面点不动（实测 swap=973ms；vblank_mode=0 时没有）。
// 所以每个窗口显示后的头 2 秒关掉垂直同步，之后恢复，平时仍按刷新率节流。
class FastFirstFrames : public QObject
{
public:
    using QObject::QObject;
protected:
    bool eventFilter(QObject *object, QEvent *event) override
    {
        if (event->type() == QEvent::Show) {
            if (auto *window = qobject_cast<QQuickWindow *>(object); window && !window->property("_fastFirstFrames").toBool()) {
                window->setProperty("_fastFirstFrames", true);
                auto *interval = new std::atomic<int>(0);   // 渲染线程读、主线程写；随窗口释放
                auto *applied = new int(-1);                  // 只在渲染线程用
                QObject::connect(window, &QQuickWindow::beforeRendering, window, [interval, applied] {
                    const int want = interval->load();
                    if (*applied == want) return;
#if QT_CONFIG(egl)   // 只有带 EGL 的 Qt（Linux）有 QEGLContext；其他平台不调垂直同步
                    auto *context = QOpenGLContext::currentContext();
                    auto *egl = context ? context->nativeInterface<QNativeInterface::QEGLContext>() : nullptr;
                    using SwapInterval = unsigned (*)(void *, int);
                    if (egl) {
                        if (auto swap = reinterpret_cast<SwapInterval>(context->getProcAddress("eglSwapInterval"))) swap(egl->display(), want);
                    }
#endif
                    *applied = want;
                }, Qt::DirectConnection);
                QTimer::singleShot(2000, window, [window, interval] { interval->store(1); window->update(); });
                QObject::connect(window, &QObject::destroyed, [interval, applied] { delete interval; delete applied; });
            }
        }
        return QObject::eventFilter(object, event);
    }
};

int main(int argc, char *argv[])
{
    startupClock.start();
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
    // 「OpenType support missing for "Noto Sans", script N」：字体缺某些文字（天城文、泰文等）的排版表，只是提示，不影响显示。
    if (qEnvironmentVariableIsEmpty("QT_LOGGING_RULES")) qputenv("QT_LOGGING_RULES", "qt.text.font.db=false");
#endif
    QGuiApplication app(argc, argv);
    startupMark("QGuiApplication");
    // 客户端只连本机服务，不需要系统代理；查询系统代理（libproxy）在首次请求时要花几百毫秒。
    QNetworkProxyFactory::setUseSystemConfiguration(false);
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
    qmlRegisterType<MasonryLayout>("omniHub.Media", 1, 0, "MasonryLayout");

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
        engine.rootContext()->setContextProperty("devStartup", qEnvironmentVariableIsSet("OMNIHUB_DEV_STARTUP"));
        engine.rootContext()->setContextProperty("devSwitch", qEnvironmentVariableIntValue("OMNIHUB_DEV_SWITCH"));
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

        app.installEventFilter(new FastFirstFrames(&app));
        startupMark("engine ready, loading QML");
        engine.load(url);
        startupMark("QML loaded");
        // 开发验证：OMNIHUB_DEV_STALLS=<秒> 在这段时间内每 20ms 打一次点，主线程被占住超过 100ms 时打印（界面点不动的时段）。
        if (const int seconds = qEnvironmentVariableIntValue("OMNIHUB_DEV_STALLS"); seconds > 0) {
            auto *tick = new QTimer(&app);
            auto *last = new qint64(startupClock.elapsed());
            tick->setInterval(20);
            QObject::connect(tick, &QTimer::timeout, &app, [tick, last, seconds] {
                const qint64 now = startupClock.elapsed();
                if (now - *last > 100) fprintf(stderr, "stall %5lld ms ending at %6lld ms\n", now - *last, now);
                *last = now;
                if (now > seconds * 1000) tick->stop();
            });
            tick->start();
        }
        // 开发验证：OMNIHUB_DEV_KEYS="毫秒:键,毫秒:键,…"（如 "4000:Delete,4300:Delete"）在指定时刻向窗口发按键，
        // 离屏测试键盘操作用。键名同 Qt::Key_ 后缀（Delete、Z、Space、Right、P、Escape、1、Question……）。
        if (!engine.rootObjects().isEmpty())
            if (auto *w = qobject_cast<QQuickWindow *>(engine.rootObjects().first()))
                for (const QString &item : qEnvironmentVariable("OMNIHUB_DEV_KEYS").split(',', Qt::SkipEmptyParts)) {
                    const int colon = item.indexOf(':');
                    const QString name = item.mid(colon + 1);
                    bool ok = false;
                    const int key = QKeySequence::fromString(name, QKeySequence::PortableText)[0].key();
                    const int at = item.left(colon).toInt(&ok);
                    if (!ok || key == 0) continue;
                    QTimer::singleShot(at, w, [w, key, name] {
                        if (!w->isActive()) w->requestActivate();
                        const QString text = name.size() == 1 ? name.toLower() : QString();
                        QKeyEvent press(QEvent::KeyPress, key, Qt::NoModifier, text), release(QEvent::KeyRelease, key, Qt::NoModifier, text);
                        QCoreApplication::sendEvent(w, &press);
                        QCoreApplication::sendEvent(w, &release);
                        QQuickItem *focus = w->activeFocusItem();
                        fprintf(stderr, "dev-key %s -> %s\n", qPrintable(name), focus ? focus->metaObject()->className() : "(no focus)");
                    });
                }
        if (!engine.rootObjects().isEmpty())
            if (auto *w = qobject_cast<QQuickWindow *>(engine.rootObjects().first())) {
                QObject::connect(w, &QQuickWindow::frameSwapped, w, [] { static bool once = false; if (!once) { once = true; startupMark("first frame"); } });
            }
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

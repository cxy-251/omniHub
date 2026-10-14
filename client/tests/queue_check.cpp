// Actual QML/decoder EOF check: two identical playlist entries must each play,
// reach the final item once, remain paused, and permit explicit replay.
#include "mpv_item.h"
#include "media_queue.h"
#include <QGuiApplication>
#include <QQuickWindow>
#include <QQmlEngine>
#include <QQmlComponent>
#include <QTimer>
#include <QUrl>
#include <QEvent>
#include <QDebug>

int main(int argc, char **argv) {
    if (argc != 2) return 11;
    QQuickWindow::setGraphicsApi(QSGRendererInterface::OpenGL);
    QGuiApplication app(argc, argv);
    app.setQuitOnLastWindowClosed(false);
    const QString source = QUrl::fromLocalFile(QString::fromLocal8Bit(argv[1])).toString();
    MediaQueue bounds;
    if (!bounds.replace({source, source}, 1)) return 12;
    const auto original = bounds.entries();
    QStringList excessive;
    for (int i = 0; i < 257; ++i) excessive.append(source);
    if (bounds.replace(excessive) || bounds.entries() != original || bounds.currentIndex() != 1 || bounds.select(2)) return 13;
    excessive.clear();
    for (int i = 0; i < 256; ++i) excessive.append(QString(8192, 'a'));
    if (bounds.replace(excessive) || bounds.entries() != original) return 14;
    qmlRegisterType<MpvItem>("omniHub.Media", 1, 0, "MpvItem");
    qmlRegisterType<MediaQueue>("omniHub.Media", 1, 0, "MediaQueue");
    int result;
    bool passed = false;
    {
        QQmlEngine engine;
        QQmlComponent component(&engine, QUrl("qrc:/qml/PlayerView.qml"));
        auto *view = qobject_cast<QQuickItem *>(component.create());
        if (!view) { qCritical() << component.errors(); return 15; }
        QQuickWindow window;
        view->setParentItem(window.contentItem());
        view->setWidth(640); view->setHeight(360); window.resize(640, 360);
        auto *player = view->findChild<MpvItem *>();
        if (!player) return 16;
        int endings = 0;
        QObject::connect(player, &MpvItem::errorChanged, &app, [&] { if (!player->error().isEmpty()) { qCritical() << player->error(); app.exit(17); } });
        QObject::connect(player, &MpvItem::playbackEnded, &app, [&] {
            ++endings;
            if (endings > 2) { app.exit(18); return; }
            if (endings == 2) QTimer::singleShot(200, &app, [&] {
                if (view->property("playlistIndex").toInt() != 1 || view->property("playlistEntries").toStringList().size() != 2 || !player->pauseRequested()) { app.exit(19); return; }
                // Wait at the end: no repeated EOF callback or implicit loop.
                QTimer::singleShot(200, &app, [&] {
                    if (endings != 2) { app.exit(20); return; }
                    player->play();
                    QTimer::singleShot(200, &app, [&] {
                        if (!player->isPlaying() || player->position() >= player->duration() - 0.1) { app.exit(21); return; }
                        passed = true;
                        fprintf(stderr, "PLAYLIST_BOUNDS_DUPLICATE_EOF_FINAL_PAUSE_AND_REPLAY_OK\n");
                        app.quit();
                    });
                });
            });
        });
        window.show();
        const QVariant files = QStringList{source, source};
        if (!QMetaObject::invokeMethod(view, "openFiles", Q_ARG(QVariant, files))) return 22;
        QTimer::singleShot(8000, &app, [&] { qCritical() << "PLAYLIST_TIMEOUT" << endings << view->property("playlistIndex"); app.exit(23); });
        result = app.exec();
        delete view;
    }
    QCoreApplication::sendPostedEvents(nullptr, QEvent::DeferredDelete);
    return result == 0 && !passed ? 24 : result;
}

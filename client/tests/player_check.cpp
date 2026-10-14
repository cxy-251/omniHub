// Isolated OpenGL integration check using caller-provided media and screenshot
// paths: real pixels, controls, scene-graph recreation and clean destruction.
#include <QGuiApplication>
#include <QQuickWindow>
#include <QSGRendererInterface>
#include <QTimer>
#include <QEvent>
#include <QDebug>
#include <QSet>
#include "mpv_item.h"

int main(int argc, char **argv)
{
    if (argc != 3) {
        fprintf(stderr, "usage: player_check VIDEO SCREENSHOT\n");
        return 11;
    }
    qInstallMessageHandler([](QtMsgType, const QMessageLogContext &, const QString &message) {
        fprintf(stderr, "%s\n", message.toUtf8().constData());
    });
    QQuickWindow::setGraphicsApi(QSGRendererInterface::OpenGL);
    QGuiApplication app(argc, argv);
    int result = 1;
    {
        QQuickWindow window;
        window.resize(320, 240);
        window.setPersistentSceneGraph(false);
        window.setPersistentGraphics(false);
        auto *player = new MpvItem(window.contentItem());
        player->setWidth(320);
        player->setHeight(240);
        QObject::connect(player, &MpvItem::errorChanged, &app, [&] {
            if (!player->error().isEmpty()) {
                qCritical() << "PLAYER_ERROR" << player->error();
                app.exit(2);
            }
        });
        window.show();
        player->setSource(QString::fromLocal8Bit(argv[1]));
        QTimer::singleShot(2500, &app, [&] {
            if (player->duration() < 7 || player->position() < 0.5 || !player->isPlaying()) {
                qCritical() << "NO_PLAYBACK" << player->duration() << player->position();
                app.exit(3);
                return;
            }
            const QImage frame = window.grabWindow();
            QSet<QRgb> colors;
            for (int y = 0; y < frame.height(); y += 24)
                for (int x = 0; x < frame.width(); x += 32) colors.insert(frame.pixel(x, y));
            if (colors.size() < 8 || !frame.save(QString::fromLocal8Bit(argv[2]))) {
                qCritical() << "NO_VIDEO_PIXELS_OR_SCREENSHOT" << colors.size();
                app.exit(8);
                return;
            }
            qInfo() << "VIDEO_PIXELS" << colors.size();
            qInfo() << "PLAYBACK" << player->duration() << player->position();
            player->pause();
            QTimer::singleShot(500, &app, [&] {
                if (player->isPlaying()) { app.exit(4); return; }
                player->setPosition(4.0);
                QTimer::singleShot(500, &app, [&] {
                    if (qAbs(player->position() - 4.0) > 0.2) {
                        qCritical() << "SEEK_FAILED" << player->position();
                        app.exit(5);
                        return;
                    }
                    player->play();
                    QTimer::singleShot(500, &app, [&] {
                        if (!player->isPlaying() || player->position() < 4.2) { app.exit(6); return; }
                        qInfo() << "PAUSE_SEEK_RESUME_OK" << player->position();
                        window.hide();
                        QTimer::singleShot(500, &app, [&] {
                            window.show();
                            QTimer::singleShot(1000, &app, [&] {
                                if (!player->isPlaying() || player->position() < 4.2) {
                                    qCritical() << "RECREATE_FAILED" << player->position();
                                    app.exit(10);
                                    return;
                                }
                                qInfo() << "RECREATED_RENDERER" << player->position();
                                window.close();
                                app.quit();
                            });
                        });
                    });
                });
            });
        });
        QTimer::singleShot(10000, &app, [&] { qCritical() << "TIMEOUT"; app.exit(7); });
        result = app.exec();
    }
    QCoreApplication::sendPostedEvents(nullptr, QEvent::DeferredDelete);
    qInfo() << "CLEAN_EXIT" << result;
    return result;
}

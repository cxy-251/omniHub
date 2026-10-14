// Isolated OpenGL integration check using caller-provided media and screenshot
// paths: real pixels, controls, scene-graph recreation and clean destruction.
#include <QGuiApplication>
#include <QQuickWindow>
#include <QSGRendererInterface>
#include <QTimer>
#include <QEvent>
#include <QDebug>
#include <QSet>
#include <QUrl>
#include "mpv_item.h"

int main(int argc, char **argv)
{
    if (argc != 3 && argc != 4) {
        fprintf(stderr, "usage: player_check VIDEO SCREENSHOT [EXTERNAL_SUBTITLE]\n");
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
        player->setSource(QUrl::fromLocalFile(QString::fromLocal8Bit(argv[1])).toString());
        QTimer::singleShot(2500, &app, [&] {
            if (player->duration() < 7 || player->position() < 0.5 || !player->isPlaying()) {
                qCritical() << "NO_PLAYBACK" << player->duration() << player->position();
                app.exit(3);
                return;
            }
            const QImage frame = window.grabWindow();
            QSet<QRgb> colors;
            bool colored = false;
            for (int y = 0; y < frame.height(); y += 24)
                for (int x = 0; x < frame.width(); x += 32) { const auto pixel = frame.pixel(x, y); colors.insert(pixel); colored |= qAbs(qRed(pixel) - qGreen(pixel)) > 20 || qAbs(qGreen(pixel) - qBlue(pixel)) > 20; }
            if (colors.size() < 8 || !colored || !frame.save(QString::fromLocal8Bit(argv[2]))) {
                qCritical() << "NO_VIDEO_PIXELS_OR_SCREENSHOT" << colors.size();
                app.exit(8);
                return;
            }
            qInfo() << "VIDEO_PIXELS" << colors.size();
            qInfo() << "PLAYBACK" << player->duration() << player->position();
            if (argc == 4) {
                int audio = 0, subtitles = 0, secondAudio = -1, secondSubtitle = -1;
                for (const auto &track : player->tracks()) {
                    const auto item = track.toMap();
                    if (item.value("type") == "audio") { audio++; secondAudio = item.value("id").toInt(); }
                    if (item.value("type") == "sub") { subtitles++; secondSubtitle = item.value("id").toInt(); }
                }
                if (audio != 2 || subtitles != 2) { qCritical() << "TRACK_METADATA_FAILED" << player->tracks(); app.exit(12); return; }
                player->selectAudioTrack(secondAudio); player->selectSubtitleTrack(secondSubtitle);
                player->setSubtitleDelay(0.3);
                QTimer::singleShot(200, &app, [&, secondAudio, secondSubtitle] {
                    bool audioSelected = false, subtitleSelected = false;
                    for (const auto &track : player->tracks()) { const auto item = track.toMap(); if (!item.value("selected").toBool()) continue;
                        if (item.value("type") == "audio" && item.value("id").toInt() == secondAudio) audioSelected = true;
                        if (item.value("type") == "sub" && item.value("id").toInt() == secondSubtitle) subtitleSelected = true;
                    }
                    if (!audioSelected || !subtitleSelected || qAbs(player->subtitleDelay() - 0.3) > 0.001) { qCritical() << "TRACK_SELECTION_FAILED" << player->tracks(); app.exit(13); return; }
                    player->selectSubtitleTrack(-1);
                    QTimer::singleShot(100, &app, [&] {
                        for (const auto &track : player->tracks()) if (track.toMap().value("type") == "sub" && track.toMap().value("selected").toBool()) { app.exit(14); return; }
                        player->addSubtitle(QUrl::fromLocalFile(QString::fromLocal8Bit(argv[3])).toString());
                        QTimer::singleShot(200, &app, [&] { int subtitles = 0; bool selected = false;
                            for (const auto &track : player->tracks()) { const auto item = track.toMap(); if (item.value("type") == "sub") { subtitles++; selected |= item.value("selected").toBool(); } }
                            if (subtitles != 3 || !selected) { qCritical() << "EXTERNAL_SUBTITLE_FAILED" << player->tracks(); app.exit(15); return; }
                            qInfo() << "TRACK_SWITCH_DISABLE_EXTERNAL_AND_DELAY_OK";
                        });
                    });
                });
            }
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

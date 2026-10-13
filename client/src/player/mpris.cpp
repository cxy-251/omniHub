#include "mpris.h"
#include <QDBusConnection>
#include <QDBusMessage>

MprisService::MprisService(QObject *parent) : QObject(parent)
{
    QDBusConnection bus = QDBusConnection::sessionBus();
    bus.registerService("org.mpris.MediaPlayer2.omnihub");
    bus.registerObject("/org/mpris/MediaPlayer2", this, QDBusConnection::ExportAllSlots | QDBusConnection::ExportAllSignals);
}

MprisService::~MprisService()
{
    QDBusConnection::sessionBus().unregisterService("org.mpris.MediaPlayer2.omnihub");
}

void MprisService::updateMetadata(const QString &title, const QString &artist, const QString &artUrl)
{
    Q_UNUSED(title);
    Q_UNUSED(artist);
    Q_UNUSED(artUrl);
}

void MprisService::updatePlaybackStatus(const QString &status)
{
    m_status = status;
}

void MprisService::Play() { emit playRequested(); }
void MprisService::Pause() { emit pauseRequested(); }
void MprisService::PlayPause() { emit playRequested(); }
void MprisService::Stop() { emit stopRequested(); }
void MprisService::Next() {}
void MprisService::Previous() {}

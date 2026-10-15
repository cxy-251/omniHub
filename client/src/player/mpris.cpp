#include "mpris.h"
#include <QDBusConnection>
#include <QDBusMessage>
#include <QCoreApplication>
#include <QFileInfo>
#include <QUrl>
#include <cmath>
#include <QDir>

static const char *objectPath = "/org/mpris/MediaPlayer2";
MprisService::MprisService(QObject *parent, const QString &serviceName) : QObject(parent), m_serviceName(serviceName) {
    new MprisRoot(this); new MprisPlayer(this);
    auto bus = QDBusConnection::sessionBus();
    if (!bus.registerService(m_serviceName)) { qWarning("MPRIS unavailable: session bus disconnected or name already owned"); return; }
    if (!bus.registerObject(objectPath, this, QDBusConnection::ExportAdaptors)) { bus.unregisterService(m_serviceName); return; }
    m_registered = true;
}
MprisService::~MprisService() {
    if (m_registered) { auto bus = QDBusConnection::sessionBus(); bus.unregisterObject(objectPath); bus.unregisterService(m_serviceName); }
}
void MprisService::bind(QObject *view) {
    for (const auto &connection : m_connections) QObject::disconnect(connection);
    m_connections.clear(); m_view = view;
    m_player = view ? view->findChild<MpvItem *>() : nullptr;
    m_queue = view ? view->findChild<MediaQueue *>() : nullptr;
    ++m_generation;
    if (m_player) {
        auto update = [this] { publish(); };
        m_connections << connect(m_player, &MpvItem::sourceChanged, this, [this] { ++m_generation; refreshArtwork(); publish(); });
        m_connections << connect(m_player, &MpvItem::playingChanged, this, update);
        m_connections << connect(m_player, &MpvItem::pauseRequestedChanged, this, update);
        m_connections << connect(m_player, &MpvItem::durationChanged, this, update);
        m_connections << connect(m_player, &MpvItem::mediaLoadedChanged, this, update);
        m_connections << connect(m_player, &MpvItem::mediaMetadataChanged, this, update);
        m_connections << connect(m_player, &MpvItem::seekCompleted, this, [this](double value) { if (m_registered) emit findChild<MprisPlayer *>()->Seeked(static_cast<qlonglong>(value * 1000000)); });
        m_connections << connect(m_player, &MpvItem::volumeChanged, this, update);
        m_connections << connect(m_player, &MpvItem::playbackRateChanged, this, update);
        m_connections << connect(m_player, &QObject::destroyed, this, [this] { m_player = nullptr; m_queue = nullptr; m_view = nullptr; ++m_generation; publish(); });
        if (m_queue) m_connections << connect(m_queue, &MediaQueue::changed, this, [this] { ++m_generation; publish(); });
    }
    refreshArtwork(); publish();
}
void MprisService::refreshArtwork() {
    m_artwork.clear();
    // 视图可以直接给出封面（如音声页的专辑封面文件）。
    if (m_view) {
        const QString art = m_view->property("artUrl").toString();
        if (!art.isEmpty()) { m_artwork = art; return; }
    }
    if (!m_player || m_player->source().isEmpty()) return;
    const QUrl uri(m_player->source());
    if (!uri.scheme().isEmpty() && !uri.isLocalFile()) return;
    const QDir directory = QFileInfo(uri.isLocalFile() ? uri.toLocalFile() : m_player->source()).absoluteDir();
    for (const auto &name : {"cover.jpg", "cover.png", "folder.jpg"}) {
        const QFileInfo image(directory.filePath(name));
        if (image.isFile() && !image.isSymLink() && image.size() <= 8 * 1024 * 1024) { m_artwork = QUrl::fromLocalFile(image.absoluteFilePath()).toString(); return; }
    }
}
QDBusObjectPath MprisService::trackId() const {
    return QDBusObjectPath(m_player && !m_player->source().isEmpty() ? QString("/org/mpris/MediaPlayer2/track/%1").arg(m_generation) : "/org/mpris/MediaPlayer2/TrackList/NoTrack");
}
QVariantMap MprisService::metadata() const {
    if (!m_player || m_player->source().isEmpty()) return {};
    const QString source = m_player->source();
    const QUrl url(source);
    const auto tags = m_player->mediaMetadata();
    const QString title = tags.value("title").toString().isEmpty() ? QFileInfo(url.isLocalFile() ? url.toLocalFile() : source).fileName().left(512) : tags.value("title").toString();
    QVariantMap result{{"mpris:trackid", QVariant::fromValue(trackId())}, {"mpris:length", static_cast<qlonglong>(qBound(0.0, m_player->duration(), 31536000.0) * 1000000)}, {"xesam:title", title}, {"xesam:url", url.scheme().isEmpty() ? QUrl::fromLocalFile(QFileInfo(source).absoluteFilePath()).toString() : source}};
    if (tags.contains("artist")) result.insert("xesam:artist", QStringList{tags.value("artist").toString()});
    if (tags.contains("album")) result.insert("xesam:album", tags.value("album"));
    if (!m_artwork.isEmpty()) result.insert("mpris:artUrl", m_artwork);
    return result;
}
void MprisService::publish() {
    if (!m_registered || m_deliveryFailed) return;
    auto *adapter = findChild<MprisPlayer *>();
    const QVariantMap current{{"PlaybackStatus",adapter->status()},{"Metadata",metadata()},{"Rate",adapter->rate()},{"Volume",adapter->volume()},{"CanGoNext",adapter->canNext()},{"CanGoPrevious",adapter->canPrevious()},{"CanPlay",adapter->canPlay()},{"CanPause",adapter->canPlay()},{"CanSeek",adapter->canSeek()}};
    QVariantMap changed;
    for (auto it=current.begin(); it!=current.end(); ++it) if (!m_last.contains(it.key()) || m_last.value(it.key()) != it.value()) changed.insert(it.key(),it.value());
    m_last=current;
    if (changed.isEmpty()) return;
    auto message=QDBusMessage::createSignal(objectPath,"org.freedesktop.DBus.Properties","PropertiesChanged");
    message << QString("org.mpris.MediaPlayer2.Player") << changed << QStringList();
    if (!QDBusConnection::sessionBus().send(message)) { qWarning("MPRIS notification delivery failed"); m_deliveryFailed = true; }
}
void MprisRoot::Raise() { if (auto *window=m_service->window()) { window->show(); window->raise(); window->requestActivate(); } }
void MprisRoot::Quit() { if (auto *window=m_service->window()) window->close(); else QCoreApplication::quit(); }
QString MprisPlayer::status() const { const auto *p=m_service->player(); return !p||!p->mediaLoaded()?"Stopped":p->isPlaying()?"Playing":"Paused"; }
double MprisPlayer::rate() const { auto *p=m_service->player(); return p?p->playbackRate():1; }
double MprisPlayer::volume() const { auto *p=m_service->player(); return p?p->volume()/100:0; }
qlonglong MprisPlayer::position() const { auto *p=m_service->player(); return p?static_cast<qlonglong>(qBound(0.0,p->position(),31536000.0)*1000000):0; }
bool MprisPlayer::canPlay() const { return m_service->player()&&!m_service->player()->source().isEmpty(); }
bool MprisPlayer::canSeek() const { return canPlay()&&m_service->player()->mediaLoaded()&&m_service->player()->duration()>0; }
// 没有 MediaQueue 的视图（音声播放器按序号向服务取分集）用 hasNext/hasPrevious 属性回答。
bool MprisPlayer::canNext() const { auto *q=m_service->queue(); if (!q) { auto *v=m_service->view(); return v&&m_service->player()&&v->property("hasNext").toBool(); } return q->currentIndex()>=0&&q->currentIndex()+1<q->entries().size(); }
bool MprisPlayer::canPrevious() const { auto *q=m_service->queue(); if (!q) { auto *v=m_service->view(); return v&&m_service->player()&&v->property("hasPrevious").toBool(); } return q->currentIndex()>0; }
void MprisPlayer::setRate(double value) { if (!std::isfinite(value)) return; if (value==0) Pause(); else if (value>=minimumRate()&&value<=maximumRate()) if (auto *p=m_service->player()) p->setPlaybackRate(value); }
void MprisPlayer::setVolume(double value) { if (std::isfinite(value)) if (auto *p=m_service->player()) p->setVolume(qBound(0.0,value,1.0)*100); }
void MprisPlayer::Play() { if (canPlay()) m_service->player()->play(); }
void MprisPlayer::Pause() { if (canPlay()) m_service->player()->pause(); }
void MprisPlayer::PlayPause() { if (canPlay()) { auto *p=m_service->player(); if (p->isPlaying()||!p->pauseRequested()) p->pause(); else p->play(); } }
void MprisPlayer::Stop() { if (canPlay()) { m_service->player()->stop(); m_service->player()->setPosition(0); } }
static void navigate(MprisService *service,const char *method) {
    auto *p=service->player(); auto *view=service->view(); if (!p||!view) return;
    const bool paused=p->pauseRequested(); QMetaObject::invokeMethod(view,method); if (paused) p->pause();
}
void MprisPlayer::Next() { if (canNext()) navigate(m_service,"nextMedia"); }
void MprisPlayer::Previous() { if (canPrevious()) navigate(m_service,"previousMedia"); }
void MprisPlayer::Seek(qlonglong offset) {
    if (!canSeek()) return;
    const double target=m_service->player()->position()+static_cast<double>(offset)/1000000;
    if (target>m_service->player()->duration()) { Next(); return; }
    m_service->player()->setPosition(qMax(0.0,target));
}
void MprisPlayer::SetPosition(const QDBusObjectPath &track,qlonglong value) {
    if (!canSeek()||track.path()!=m_service->trackId().path()||value<0||static_cast<double>(value)/1000000>m_service->player()->duration()) return;
    m_service->player()->setPosition(static_cast<double>(value)/1000000);
}

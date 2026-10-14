#pragma once
#include <QObject>
#include <QPointer>
#include <QVariantMap>
#include <QQuickWindow>
#include <QDBusAbstractAdaptor>
#include <QDBusObjectPath>
#include "mpv_item.h"
#include "media_queue.h"

// MprisService binds the currently loaded view. It exports only protocol
// adaptors, never its binding helpers; no polling and no decoder ownership.
class MprisService : public QObject {
    Q_OBJECT
public:
    explicit MprisService(QObject *parent = nullptr);
    ~MprisService() override;
    // Replace the active QML view; null/destroyed views clear protocol capabilities.
    Q_INVOKABLE void bind(QObject *view);
    // Attach the host window for the standard Raise command, without owning it.
    void setWindow(QQuickWindow *window) { m_window = window; }
    MpvItem *player() const { return m_player; }
    QObject *view() const { return m_view; }
    QQuickWindow *window() const { return m_window; }
    MediaQueue *queue() const { return m_queue; }
    QVariantMap metadata() const;
    QDBusObjectPath trackId() const;
    void publish();
private:
    QPointer<MpvItem> m_player;
    QPointer<MediaQueue> m_queue;
    QPointer<QObject> m_view;
    QPointer<QQuickWindow> m_window;
    QList<QMetaObject::Connection> m_connections;
    QVariantMap m_last;
    quint64 m_generation = 0;
    bool m_registered = false;
    bool m_deliveryFailed = false;
    QString m_artwork;
    void refreshArtwork();
};

// Root protocol interface; optional TrackList/URI opening are not advertised.
class MprisRoot : public QDBusAbstractAdaptor {
    Q_OBJECT
    Q_CLASSINFO("D-Bus Interface", "org.mpris.MediaPlayer2")
    Q_PROPERTY(bool CanQuit READ yes)
    Q_PROPERTY(bool CanRaise READ yes)
    Q_PROPERTY(bool HasTrackList READ no)
    Q_PROPERTY(QString Identity READ identity)
    Q_PROPERTY(QString DesktopEntry READ desktopEntry)
    Q_PROPERTY(QStringList SupportedUriSchemes READ emptyList)
    Q_PROPERTY(QStringList SupportedMimeTypes READ emptyList)
public:
    explicit MprisRoot(MprisService *service) : QDBusAbstractAdaptor(service), m_service(service) {}
    bool yes() const { return true; } bool no() const { return false; }
    QString identity() const { return "omniHub"; }
    QString desktopEntry() const { return "omnihub"; }
    QStringList emptyList() const { return {}; }
public slots:
    void Raise();
    void Quit();
private: MprisService *m_service;
};

// Player protocol reflects observed playback and delegates finite controls.
class MprisPlayer : public QDBusAbstractAdaptor {
    Q_OBJECT
    Q_CLASSINFO("D-Bus Interface", "org.mpris.MediaPlayer2.Player")
    Q_PROPERTY(QString PlaybackStatus READ status)
    Q_PROPERTY(QVariantMap Metadata READ metadata)
    Q_PROPERTY(double Rate READ rate WRITE setRate)
    Q_PROPERTY(double Volume READ volume WRITE setVolume)
    Q_PROPERTY(qlonglong Position READ position)
    Q_PROPERTY(double MinimumRate READ minimumRate)
    Q_PROPERTY(double MaximumRate READ maximumRate)
    Q_PROPERTY(bool CanGoNext READ canNext)
    Q_PROPERTY(bool CanGoPrevious READ canPrevious)
    Q_PROPERTY(bool CanPlay READ canPlay)
    Q_PROPERTY(bool CanPause READ canPlay)
    Q_PROPERTY(bool CanSeek READ canSeek)
    Q_PROPERTY(bool CanControl READ yes)
public:
    explicit MprisPlayer(MprisService *service) : QDBusAbstractAdaptor(service), m_service(service) {}
    QString status() const;
    QVariantMap metadata() const { return m_service->metadata(); }
    double rate() const; void setRate(double value);
    double volume() const; void setVolume(double value);
    qlonglong position() const;
    double minimumRate() const { return 0.25; } double maximumRate() const { return 3; }
    bool canNext() const; bool canPrevious() const; bool canPlay() const; bool canSeek() const;
    bool yes() const { return true; }
public slots:
    // Seek offsets/positions use microseconds; SetPosition ignores stale track IDs.
    void Play(); void Pause(); void PlayPause(); void Stop(); void Next(); void Previous();
    void Seek(qlonglong offset);
    void SetPosition(const QDBusObjectPath &track, qlonglong position);
    void OpenUri(const QString &) {} // Empty SupportedUriSchemes explicitly disables this optional operation.
signals:
    void Seeked(qlonglong position);
private: MprisService *m_service;
};

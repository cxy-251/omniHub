#pragma once
#include <QObject>
#include <QPointer>
#include <QTimer>
#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QVariantMap>
#include "mpv_item.h"

// NativeResume persists host-account local-file positions through the service.
// One request, at most 16 coalesced snapshots, and one 15s active check; no idle IO.
class NativeResume : public QObject {
    Q_OBJECT
    Q_PROPERTY(QString error READ error NOTIFY errorChanged)
public:
    explicit NativeResume(QObject *parent = nullptr);
    QString error() const { return m_error; }
    // Bind a new view. Snapshot-based restores can suppress database repositioning.
    Q_INVOKABLE void bind(QObject *view, bool allowRestore = true);
    // Retry account discovery after initial setup/reconnection, without taking over playback.
    void refresh();
    // Save current state and drain with a 1.5s bound before normal GUI exit.
    Q_INVOKABLE void flush();
signals:
    void errorChanged();
    void drained();
private:
    void load(bool restore);
    void save();
    void pump();
    void setError(const QString &message);
    QPointer<MpvItem> m_player;
    QList<QMetaObject::Connection> m_connections;
    QNetworkAccessManager m_network;
    QPointer<QNetworkReply> m_reply;
    QTimer m_timer;
    QString m_id, m_version, m_error;
    qint64 m_account = 0;
    quint64 m_generation = 0;
    bool m_readNeeded = false, m_restore = false, m_applying = false, m_completed = false;
    bool m_changing = false, m_userEdited = false;
    bool m_retryBlocked = false;
    QList<QVariantMap> m_saves;
    QVariantMap m_last;
};

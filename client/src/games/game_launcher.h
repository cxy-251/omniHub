#pragma once

#include <QObject>
#include <QVariantList>
#include <QVariantMap>
#include <QNetworkAccessManager>
#include <QTimer>

// GameLauncher displays one bounded catalog page and observes service-owned
// sessions. Destroying a page releases requests, never the running game.
class GameLauncher : public QObject {
    Q_OBJECT
    Q_PROPERTY(QVariantList games READ games NOTIFY gamesChanged)
    Q_PROPERTY(bool loading READ loading NOTIFY loadingChanged)
    Q_PROPERTY(bool busy READ busy NOTIFY sessionChanged)
    Q_PROPERTY(bool gameRunning READ gameRunning NOTIFY sessionChanged)
    Q_PROPERTY(QVariantMap session READ session NOTIFY sessionChanged)
    Q_PROPERTY(QString error READ error NOTIFY errorChanged)
    Q_PROPERTY(int offset READ offset NOTIFY gamesChanged)
    Q_PROPERTY(bool hasMore READ hasMore NOTIFY gamesChanged)
public:
    explicit GameLauncher(QObject *parent = nullptr);
    // Property accessors return snapshots without issuing requests or starting processes.
    QVariantList games() const { return m_games; }
    bool loading() const { return m_loading; }
    bool busy() const { return m_controlBusy; }
    bool gameRunning() const { return !m_session.value("id").toString().isEmpty() && m_session.value("state").toString() != "exited"; }
    QVariantMap session() const { return m_session; }
    QString error() const { return m_error; }
    int offset() const { return m_offset; }
    bool hasMore() const { return m_hasMore; }
    // Load one 24-record page; reject invalid offsets and concurrent fetches.
    Q_INVOKABLE void fetchGames(int offset = 0);
    // Start by library/game identity; host metadata supplies executable and argv.
    Q_INVOKABLE void launchGame(const QString &libraryId, const QString &gameId);
    // Observe a retained session, or discover the service's active game.
    Q_INVOKABLE void refreshSession();
    // Request managed stop; completion is determined by subsequent observation.
    Q_INVOKABLE void stopGame();
signals:
    void gamesChanged();
    void loadingChanged();
    void sessionChanged();
    void errorChanged();
    void launchFinished(const QString &gameId, bool success);
private:
    void setError(const QString &error);
    void consumeSession(const QVariantMap &session);
    QNetworkAccessManager m_net;
    QTimer m_poll;
    QVariantList m_games;
    QVariantMap m_session;
    QString m_error;
    int m_offset = 0;
    quint64 m_sessionGeneration = 0;
    bool m_hasMore = false;
    bool m_loading = false;
    bool m_controlBusy = false;
    bool m_queryBusy = false;
};

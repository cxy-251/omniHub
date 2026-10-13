#pragma once

#include <QObject>
#include <QVariantList>
#include <QNetworkAccessManager>
#include <QNetworkReply>

class GameLauncher : public QObject
{
    Q_OBJECT
    Q_PROPERTY(QVariantList games READ games NOTIFY gamesChanged)
    Q_PROPERTY(bool loading READ loading NOTIFY loadingChanged)

public:
    explicit GameLauncher(QObject *parent = nullptr);

    QVariantList games() const { return m_games; }
    bool loading() const { return m_loading; }

    Q_INVOKABLE void fetchGames();
    Q_INVOKABLE void launchGame(const QString &gameId, const QString &exePath, const QString &args);

signals:
    void gamesChanged();
    void loadingChanged();
    void launchFinished(const QString &gameId, bool success);

private:
    QNetworkAccessManager m_net;
    QVariantList m_games;
    bool m_loading = false;
};

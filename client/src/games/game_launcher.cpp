#include "game_launcher.h"
#include <QJsonDocument>
#include <QJsonArray>
#include <QJsonObject>
#include <QProcess>

GameLauncher::GameLauncher(QObject *parent) : QObject(parent) {}

void GameLauncher::fetchGames(const QString &hostUrl)
{
    m_loading = true;
    emit loadingChanged();

    QUrl url(hostUrl + "/api/games/list?target=gui_only");
    QNetworkRequest req(url);
    QNetworkReply *reply = m_net.get(req);

    connect(reply, &QNetworkReply::finished, this, [this, reply]() {
        m_loading = false;
        emit loadingChanged();

        if (reply->error() == QNetworkReply::NoError) {
            QByteArray data = reply->readAll();
            QJsonDocument doc = QJsonDocument::fromJson(data);
            if (doc.isArray()) {
                m_games = doc.array().toVariantList();
                emit gamesChanged();
            }
        }
        reply->deleteLater();
    });
}

void GameLauncher::launchGame(const QString &gameId, const QString &exePath, const QString &args)
{
    QStringList cmdArgs;
    cmdArgs << "-c" << "3" << exePath;
    if (!args.isEmpty()) {
        cmdArgs << args.split(' ');
    }

    bool started = QProcess::startDetached("ionice", cmdArgs);
    emit launchFinished(gameId, started);
}

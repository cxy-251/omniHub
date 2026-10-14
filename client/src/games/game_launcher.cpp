#include "game_launcher.h"
#include "backend_api.h"
#include <QJsonDocument>
#include <QJsonArray>
#include <QJsonObject>
#include <QNetworkReply>
#include <QUrl>
#include <QUrlQuery>

namespace {
void boundReply(QNetworkReply *reply, qint64 bytes) {
    reply->setReadBufferSize(bytes + 1);
    QObject::connect(reply, &QIODevice::readyRead, reply, [reply, bytes]() {
        if (reply->bytesAvailable() > bytes) reply->abort();
    });
}
QString replyError(QNetworkReply *reply, const QByteArray &body) {
    return QString::fromUtf8(body.left(2048)).trimmed().isEmpty()
        ? reply->errorString() : QString::fromUtf8(body.left(2048)).trimmed();
}
QNetworkRequest request(const QString &path) {
    auto value = BackendApi::localRequest(path);
    value.setTransferTimeout(10000);
    return value;
}
}

GameLauncher::GameLauncher(QObject *parent) : QObject(parent) {
    m_poll.setInterval(1000);
    connect(&m_poll, &QTimer::timeout, this, &GameLauncher::refreshSession);
}

void GameLauncher::setError(const QString &error) {
    if (m_error == error) return;
    m_error = error;
    emit errorChanged();
}

void GameLauncher::fetchGames(int offset) {
    if (m_loading || offset < 0 || offset > 1000000) return;
    m_loading = true;
    emit loadingChanged();
    auto *reply = m_net.get(request(QString("/api/games/list?limit=24&offset=%1").arg(offset)));
    boundReply(reply, 512 * 1024);
    connect(reply, &QNetworkReply::finished, this, [this, reply, offset]() {
        const QByteArray data = reply->readAll();
        QJsonParseError parse;
        const auto document = QJsonDocument::fromJson(data, &parse);
        if (reply->error() == QNetworkReply::NoError && parse.error == QJsonParseError::NoError && document.isArray() && document.array().size() <= 24) {
            m_games = document.array().toVariantList();
            m_offset = offset;
            m_hasMore = reply->rawHeader("X-Has-More") == "true";
            setError({});
            emit gamesChanged();
        } else setError(reply->error() == QNetworkReply::NoError ? "游戏清单响应无效" : replyError(reply, data));
        m_loading = false;
        emit loadingChanged();
        reply->deleteLater();
    });
}

void GameLauncher::consumeSession(const QVariantMap &session) {
    m_session = session;
    if (gameRunning()) m_poll.start(); else m_poll.stop();
    emit sessionChanged();
}

void GameLauncher::launchGame(const QString &libraryId, const QString &gameId) {
    if (m_controlBusy || gameRunning()) return;
    if (libraryId.isEmpty() || libraryId.size() > 128 || gameId.isEmpty() || gameId.size() > 512) { setError("游戏标识无效"); return; }
    m_controlBusy = true;
    ++m_sessionGeneration; // A late pre-launch idle response cannot erase this run.
    emit sessionChanged();
    auto req = request("/api/games/launch");
    req.setHeader(QNetworkRequest::ContentTypeHeader, "application/json");
    auto *reply = m_net.post(req, QJsonDocument(QJsonObject{{"library_id", libraryId}, {"id", gameId}}).toJson(QJsonDocument::Compact));
    boundReply(reply, 16384);
    connect(reply, &QNetworkReply::finished, this, [this, reply, gameId]() {
        const auto data = reply->readAll();
        QJsonParseError parse;
        const auto document = QJsonDocument::fromJson(data, &parse);
        const bool success = reply->error() == QNetworkReply::NoError && parse.error == QJsonParseError::NoError && document.isObject() && !document.object().value("id").toString().isEmpty();
        m_controlBusy = false;
        if (success) { setError({}); consumeSession(document.object().toVariantMap()); }
        else {
            setError(reply->error() == QNetworkReply::NoError ? "启动响应无效" : replyError(reply, data));
            // A lost reply does not prove startup failed. Reconcile the owning
            // service until an active session or confirmed idle is observed.
            m_poll.start();
            refreshSession();
            emit sessionChanged();
        }
        emit launchFinished(gameId, success);
        reply->deleteLater();
    });
}

void GameLauncher::refreshSession() {
    if (m_queryBusy) return;
    m_queryBusy = true;
    const auto generation = m_sessionGeneration;
    QString path = "/api/games/session";
    const QString id = m_session.value("id").toString();
    if (!id.isEmpty()) {
        QUrlQuery query; query.addQueryItem("id", id);
        path += "?" + query.toString(QUrl::FullyEncoded);
    }
    auto *reply = m_net.get(request(path));
    boundReply(reply, 16384);
    connect(reply, &QNetworkReply::finished, this, [this, reply, generation]() {
        m_queryBusy = false;
        const auto data = reply->readAll();
        if (generation == m_sessionGeneration) {
            QJsonParseError parse;
            const auto document = QJsonDocument::fromJson(data, &parse);
            const QString state = document.object().value("state").toString();
            const bool validSession = parse.error == QJsonParseError::NoError && document.isObject()
                && !document.object().value("id").toString().isEmpty()
                && (state == "running" || state == "stopping" || state == "exited");
            if (reply->error() == QNetworkReply::NoError && (data.trimmed() == "null" || validSession)) {
                consumeSession(document.isObject() ? document.object().toVariantMap() : QVariantMap{});
            } else setError(reply->error() == QNetworkReply::NoError ? "会话响应无效" : replyError(reply, data));
        }
        reply->deleteLater();
    });
}

void GameLauncher::stopGame() {
    if (m_controlBusy || !gameRunning()) return;
    m_controlBusy = true;
    emit sessionChanged();
    auto req = request("/api/games/stop");
    req.setHeader(QNetworkRequest::ContentTypeHeader, "application/json");
    auto *reply = m_net.post(req, QJsonDocument(QJsonObject{{"id", m_session.value("id").toString()}}).toJson(QJsonDocument::Compact));
    boundReply(reply, 16384);
    connect(reply, &QNetworkReply::finished, this, [this, reply]() {
        const auto data = reply->readAll();
        m_controlBusy = false;
        if (reply->error() != QNetworkReply::NoError) setError(replyError(reply, data));
        else { setError({}); refreshSession(); }
        emit sessionChanged();
        reply->deleteLater();
    });
}

void GameLauncher::indexLibrary(const QString &libraryId) {
    if (m_controlBusy || gameRunning()) return;
    if (libraryId.isEmpty() || libraryId.size() > 128) { setError("资源库标识无效"); return; }
    m_controlBusy = true;
    emit sessionChanged();
    auto req = request("/api/games/index");
    req.setTransferTimeout(60000);
    req.setHeader(QNetworkRequest::ContentTypeHeader, "application/json");
    auto *reply = m_net.post(req, QJsonDocument(QJsonObject{{"library_id", libraryId}}).toJson(QJsonDocument::Compact));
    boundReply(reply, 16384);
    connect(reply, &QNetworkReply::finished, this, [this, reply]() {
        const auto data = reply->readAll();
        QJsonParseError parse;
        const auto document = QJsonDocument::fromJson(data, &parse);
        m_controlBusy = false;
        if (reply->error() == QNetworkReply::NoError && parse.error == QJsonParseError::NoError && document.isObject() && document.object().value("indexed").isDouble()) {
            setError({});
            fetchGames();
        } else setError(reply->error() == QNetworkReply::NoError ? "扫描响应无效" : replyError(reply, data));
        emit sessionChanged();
        reply->deleteLater();
    });
}

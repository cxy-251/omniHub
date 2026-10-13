#include "backend_api.h"
#include <QDir>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>

BackendApi::BackendApi(QObject *parent) : QObject(parent) {}

QNetworkRequest BackendApi::localRequest(const QString &path) {
    QString configDir = qEnvironmentVariable("OMNIHUB_CONFIG_DIR");
    if (configDir.isEmpty()) {
#ifdef Q_OS_WIN
        configDir = QDir(qEnvironmentVariable("APPDATA")).filePath("omnihub");
#elif defined(Q_OS_MACOS)
        configDir = QDir::home().filePath("Library/Application Support/omnihub");
#else
        QString base = qEnvironmentVariable("XDG_CONFIG_HOME");
        if (base.isEmpty()) base = QDir::home().filePath(".config");
        configDir = QDir(base).filePath("omnihub");
#endif
    }
    QFile connection(QDir(configDir).filePath("local-client.json"));
    if (!connection.open(QIODevice::ReadOnly)) return QNetworkRequest();
    const QJsonObject info = QJsonDocument::fromJson(connection.readAll()).object();
    QUrl url(info.value("url").toString());
    // A changed local file must never forward the privileged token to a remote URL.
    if (url.scheme() != "http" || (url.host() != "127.0.0.1" && url.host() != "::1") ||
        !path.startsWith("/api/")) return QNetworkRequest();
    url.setPath(path);
    QNetworkRequest request(url);
    request.setRawHeader("X-Omni-Local-Token", info.value("token").toString().toUtf8());
    request.setHeader(QNetworkRequest::ContentTypeHeader, "application/json");
    request.setTransferTimeout(5000);
    return request;
}

void BackendApi::consumeStatus(QNetworkReply *reply) {
    if (reply->error() == QNetworkReply::NoError) {
        const QJsonObject info = QJsonDocument::fromJson(reply->readAll()).object();
        m_connected = info.contains("has_admin");
        m_hasAdmin = info.value("has_admin").toBool();
        m_error = m_connected ? QString() : tr("服务状态无效");
    } else {
        m_connected = false;
        m_error = tr("无法连接 omniHub 服务：") + reply->errorString();
    }
    reply->deleteLater();
    emit statusChanged();
}

void BackendApi::refresh() {
    auto *reply = m_network.get(localRequest("/api/auth/status"));
    connect(reply, &QNetworkReply::finished, this, [this, reply] { consumeStatus(reply); });
}

void BackendApi::setupAdmin(const QString &username, const QString &password) {
    const QJsonObject body{{"username", username}, {"password", password}};
    auto *reply = m_network.post(localRequest("/api/auth/setup"), QJsonDocument(body).toJson(QJsonDocument::Compact));
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        if (reply->error() == QNetworkReply::NoError) { reply->deleteLater(); refresh(); }
        else { m_error = reply->errorString(); reply->deleteLater(); emit statusChanged(); }
    });
}

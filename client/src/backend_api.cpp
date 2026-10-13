#include "backend_api.h"
#include <QDir>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJsonArray>
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
    refreshLibraries();
}

void BackendApi::libraryRequest(const QString &path, const QVariantMap &body, bool get, bool preview) {
    if (m_libraryBusy) return;
    m_libraryBusy = true;
    m_libraryError.clear();
    emit librariesChanged();
    QNetworkRequest request = localRequest(path);
    // 迁移需要流式校验数据库，超过普通状态查询的 5 秒窗口。
    request.setTransferTimeout(path.endsWith("/commit") ? 0 : 5000);
    auto *reply = get ? m_network.get(request)
                     : m_network.post(request, QJsonDocument::fromVariant(body).toJson(QJsonDocument::Compact));
    connect(reply, &QNetworkReply::finished, this, [this, reply, body, preview] {
        m_libraryBusy = false;
        const QByteArray data = reply->readAll();
        if (reply->error() != QNetworkReply::NoError) {
            m_libraryError = data.isEmpty() ? reply->errorString() : QString::fromUtf8(data).trimmed();
        } else {
            QJsonParseError parseError;
            const QJsonDocument document = QJsonDocument::fromJson(data, &parseError);
            const QJsonObject info = document.object();
            if (parseError.error != QJsonParseError::NoError || (preview ? !info.contains("confirmation_token") : !info.contains("libraries"))) {
                m_libraryError = tr("资源库服务返回了无效数据");
            } else if (preview) {
                m_migrationPreview = info.toVariantMap();
                m_previewLibraryId = body.value("library_id").toString();
            } else {
                m_libraries = info.value("libraries").toArray().toVariantList();
                m_stateReady = info.value("state_ready").toBool();
                m_stateDirectory = info.value("state_directory").toString();
                m_libraryError = info.value("state_error").toString();
                m_migrationPreview.clear();
                m_previewLibraryId.clear();
            }
        }
        reply->deleteLater();
        emit librariesChanged();
        if (!preview && m_stateReady) {
            auto *status = m_network.get(localRequest("/api/auth/status"));
            connect(status, &QNetworkReply::finished, this, [this, status] { consumeStatus(status); });
        }
    });
}

void BackendApi::refreshLibraries() { libraryRequest("/api/library/manage", {}, true); }
void BackendApi::recoverLibraries() { libraryRequest("/api/library/refresh", {}); }
void BackendApi::addLibrary(const QString &path, bool readOnly) {
    libraryRequest("/api/library/add", {{"path", path}, {"read_only", readOnly}});
}
void BackendApi::removeLibrary(const QString &id) {
    libraryRequest("/api/library/remove", {{"library_id", id}});
}
void BackendApi::previewPrimary(const QString &id) {
    libraryRequest("/api/library/primary/preview", {{"library_id", id}}, false, true);
}
void BackendApi::confirmPrimary() {
    if (m_migrationPreview.isEmpty()) return;
    libraryRequest("/api/library/primary/commit", {{"library_id", m_previewLibraryId},
                    {"confirmation_token", m_migrationPreview.value("confirmation_token")}});
}
void BackendApi::cancelMigration() {
    if (m_libraryBusy) return;
    m_migrationPreview.clear();
    m_previewLibraryId.clear();
    emit librariesChanged();
}

void BackendApi::setupAdmin(const QString &username, const QString &password) {
    const QJsonObject body{{"username", username}, {"password", password}};
    auto *reply = m_network.post(localRequest("/api/auth/setup"), QJsonDocument(body).toJson(QJsonDocument::Compact));
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        if (reply->error() == QNetworkReply::NoError) { reply->deleteLater(); refresh(); }
        else { m_error = reply->errorString(); reply->deleteLater(); emit statusChanged(); }
    });
}

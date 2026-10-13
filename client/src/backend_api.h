#pragma once

#include <QObject>
#include <QNetworkAccessManager>
#include <QNetworkRequest>
#include <QVariantMap>

// BackendApi discovers the service through the user's protected local connection file.
class BackendApi : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool hasAdmin READ hasAdmin NOTIFY statusChanged)
    Q_PROPERTY(bool connected READ connected NOTIFY statusChanged)
    Q_PROPERTY(QString error READ error NOTIFY statusChanged)
public:
    explicit BackendApi(QObject *parent = nullptr);
    bool hasAdmin() const { return m_hasAdmin; }
    bool connected() const { return m_connected; }
    QString error() const { return m_error; }
    Q_INVOKABLE void refresh();
    Q_INVOKABLE void setupAdmin(const QString &username, const QString &password);
    static QNetworkRequest localRequest(const QString &path);
signals:
    void statusChanged();
private:
    void consumeStatus(class QNetworkReply *reply);
    QNetworkAccessManager m_network;
    bool m_hasAdmin = false;
    bool m_connected = false;
    QString m_error;
};

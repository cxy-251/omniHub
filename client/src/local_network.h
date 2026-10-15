#pragma once
#include <QNetworkAccessManager>
#include <QQmlNetworkAccessManagerFactory>

// LocalNetworkFactory 让 QML 的 Image、XMLHttpRequest 访问本机 omniHub 服务时自动带上本机凭证。
// 只对与 local-client.json 中地址同源（http://127.0.0.1 或 ::1 的同一端口）的 /api/ 请求加凭证，
// 其他地址的请求保持原样，凭证不会外泄。
class LocalNetworkFactory : public QQmlNetworkAccessManagerFactory {
public:
    QNetworkAccessManager *create(QObject *parent) override;
};

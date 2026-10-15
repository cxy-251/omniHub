#include "local_network.h"
#include "backend_api.h"
#include <QNetworkRequest>

namespace {
class LocalAuthManager : public QNetworkAccessManager {
public:
    using QNetworkAccessManager::QNetworkAccessManager;

protected:
    QNetworkReply *createRequest(Operation op, const QNetworkRequest &original, QIODevice *data) override {
        const QUrl url = original.url();
        if (!url.path().startsWith("/api/"))
            return QNetworkAccessManager::createRequest(op, original, data);
        // localRequest 自己校验目标必须是本机回环地址，并返回带凭证的请求；不匹配时返回空请求。
        QNetworkRequest local = BackendApi::localRequest(url.path() + (url.hasQuery() ? "?" + url.query(QUrl::FullyEncoded) : QString()));
        if (local.url().isEmpty() || local.url().host() != url.host() || local.url().port() != url.port())
            return QNetworkAccessManager::createRequest(op, original, data);
        QNetworkRequest request(original);
        request.setRawHeader("X-Omni-Local-Token", local.rawHeader("X-Omni-Local-Token"));
        request.setAttribute(QNetworkRequest::RedirectPolicyAttribute, QNetworkRequest::ManualRedirectPolicy);
        return QNetworkAccessManager::createRequest(op, request, data);
    }
};
}

QNetworkAccessManager *LocalNetworkFactory::create(QObject *parent) {
    return new LocalAuthManager(parent);
}

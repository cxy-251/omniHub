#include "masonry_layout.h"
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QQmlEngine>
#include <algorithm>
#include <QElapsedTimer>
#include <QDebug>

void MasonryLayout::setSource(const QString &source)
{
    if (source == m_source) return;
    m_source = source;
    emit sourceChanged();
    if (m_reply) { auto *old = m_reply.data(); m_reply = nullptr; old->abort(); old->deleteLater(); }
    if (source.isEmpty()) return;
    // 用 QML 引擎的网络管理器：本机服务请求由 LocalNetworkFactory 自动带凭证。
    auto *engine = qmlEngine(this);
    if (!engine || !engine->networkAccessManager()) return;
    QNetworkRequest request{QUrl(source)};
    auto *reply = engine->networkAccessManager()->get(request);
    m_reply = reply;
    emit loadingChanged();
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        reply->deleteLater();
        if (m_reply != reply) return;   // 已被新的 source 取代
        m_reply = nullptr;
        emit loadingChanged();
        if (reply->error() != QNetworkReply::NoError) return;
        QElapsedTimer timer; timer.start();
        const QJsonArray list = QJsonDocument::fromJson(reply->readAll()).object().value("ratios").toArray();
        QVector<quint16> ratios;
        ratios.reserve(list.size());
        for (const auto &v : list) ratios.append(static_cast<quint16>(qBound(1, v.toInt(1000), 65535)));
        m_ratios = std::move(ratios);
        relayout();
        if (qEnvironmentVariableIsSet("OMNIHUB_DEV_SWITCH")) qInfo("masonry-timing parse+layout %lld ms for %d photos", timer.elapsed(), int(m_ratios.size()));
        emit loaded();
    });
}

void MasonryLayout::relayout()
{
    const int n = m_ratios.size();
    const int columns = std::min(m_columns, 255);
    m_ys.resize(n);
    m_cols.resize(n);
    QVector<double> heights(columns, 0.0);
    double tallest = 0;
    for (int i = 0; i < n; ++i) {
        const int best = int(std::min_element(heights.begin(), heights.end()) - heights.begin());
        const double h = heightFor(m_ratios[i]);
        m_ys[i] = heights[best];
        m_cols[i] = quint8(best);
        heights[best] += h + m_gap;
        tallest = std::max(tallest, h);
    }
    m_height = heights.isEmpty() ? 0 : *std::max_element(heights.begin(), heights.end());
    m_maxItem = tallest;
    ++m_revision;
    emit layoutChanged();
}

int MasonryLayout::find(double y) const
{
    return int(std::lower_bound(m_ys.begin(), m_ys.end(), y) - m_ys.begin());
}

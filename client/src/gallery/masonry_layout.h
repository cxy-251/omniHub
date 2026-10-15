#pragma once
#include <QObject>
#include <QPointer>
#include <QVector>
#include <QNetworkReply>

// MasonryLayout：相册瀑布流的排版。自己向服务取全部照片的高宽比（×1000），每张放进当前最矮的一列。
// 纵坐标随序号单调不减，find() 用二分查找可视范围。10 万张在 C++ 里排版约几毫秒，
// 避免在 QML/JS 里解析大 JSON、逐张排版时卡住界面线程（实测约 100ms）。
class MasonryLayout : public QObject {
    Q_OBJECT
    Q_PROPERTY(QString source READ source WRITE setSource NOTIFY sourceChanged)
    Q_PROPERTY(int columns READ columns WRITE setColumns NOTIFY layoutChanged)
    Q_PROPERTY(double columnWidth READ columnWidth WRITE setColumnWidth NOTIFY layoutChanged)
    Q_PROPERTY(double gap READ gap WRITE setGap NOTIFY layoutChanged)
    Q_PROPERTY(int count READ count NOTIFY layoutChanged)
    Q_PROPERTY(double contentHeight READ contentHeight NOTIFY layoutChanged)
    Q_PROPERTY(double maxItem READ maxItem NOTIFY layoutChanged)
    Q_PROPERTY(int revision READ revision NOTIFY layoutChanged)
    Q_PROPERTY(bool loading READ loading NOTIFY loadingChanged)
public:
    explicit MasonryLayout(QObject *parent = nullptr) : QObject(parent) {}
    ~MasonryLayout() override { if (m_reply) m_reply->abort(); }
    QString source() const { return m_source; }
    void setSource(const QString &source);
    int columns() const { return m_columns; }
    void setColumns(int c) { if (c > 0 && c != m_columns) { m_columns = c; relayout(); } }
    double columnWidth() const { return m_columnWidth; }
    void setColumnWidth(double w) { if (w > 0 && w != m_columnWidth) { m_columnWidth = w; relayout(); } }
    double gap() const { return m_gap; }
    void setGap(double g) { if (g >= 0 && g != m_gap) { m_gap = g; relayout(); } }
    int count() const { return m_ratios.size(); }
    double contentHeight() const { return m_height; }
    double maxItem() const { return m_maxItem; }
    int revision() const { return m_revision; }
    bool loading() const { return !m_reply.isNull(); }
    // 第一个纵坐标 >= y 的序号
    Q_INVOKABLE int find(double y) const;
    Q_INVOKABLE double itemX(int i) const { return valid(i) ? m_cols[i] * (m_columnWidth + m_gap) : 0; }
    Q_INVOKABLE double itemY(int i) const { return valid(i) ? m_ys[i] : 0; }
    Q_INVOKABLE double itemHeight(int i) const { return valid(i) ? heightFor(m_ratios[i]) : 0; }
signals:
    void sourceChanged();
    void layoutChanged();
    void loadingChanged();
    void loaded();
private:
    bool valid(int i) const { return i >= 0 && i < m_ys.size(); }
    double heightFor(quint16 ratio) const { return m_columnWidth * qBound(0.4, ratio / 1000.0, 2.5); }
    void relayout();
    QString m_source;
    QPointer<QNetworkReply> m_reply;
    QVector<quint16> m_ratios;
    QVector<double> m_ys;
    QVector<quint8> m_cols;
    int m_columns = 4;
    double m_columnWidth = 200, m_gap = 4, m_height = 0, m_maxItem = 0;
    int m_revision = 0;
};

#pragma once
#include <QObject>
#include <QVariantList>
#include <QNetworkAccessManager>
#include <QPointer>
#include <QNetworkReply>
#include <QTimer>
#include <QJsonObject>
#include <QJsonDocument>

// TextReader keeps one 24-book catalog page, one bounded content chunk and up
// to 256 navigation cursors. Only 16Ki UTF-16 characters reach the text layout.
class TextReader : public QObject {
    Q_OBJECT
    Q_PROPERTY(QVariantList books READ books NOTIFY changed)
    Q_PROPERTY(QString content READ content NOTIFY changed)
    Q_PROPERTY(QString title READ title NOTIFY changed)
    Q_PROPERTY(QString error READ error NOTIFY changed)
    Q_PROPERTY(bool loading READ loading NOTIFY changed)
    Q_PROPERTY(bool reading READ reading NOTIFY changed)
    Q_PROPERTY(bool hasMore READ hasMore NOTIFY changed)
    Q_PROPERTY(bool canNext READ canNext NOTIFY changed)
    Q_PROPERTY(bool canPrevious READ canPrevious NOTIFY changed)
    Q_PROPERTY(int offset READ offset NOTIFY changed)
    Q_PROPERTY(int startLine READ startLine NOTIFY changed)
public:
    explicit TextReader(QObject *parent=nullptr);
    ~TextReader() override;
    QVariantList books() const {return m_books;}
    QString content() const {return m_text.mid(m_segments.value(m_segment), m_segments.value(m_segment+1)-m_segments.value(m_segment));}
    QString title() const {return m_title;}
    QString error() const {return m_error;}
    bool loading() const {return m_reply!=nullptr;}
    bool reading() const {return !m_path.isEmpty();}
    bool hasMore() const {return m_more;}
    bool canNext() const {return m_segment+2<m_segments.size()||m_chunkMore;}
    bool canPrevious() const {return m_segment>0||!m_history.isEmpty();}
    int offset() const {return m_offset;}
    int startLine() const {return m_line;}
    // Explicit catalog navigation; identity must be a displayed UUID-qualified entry.
    Q_INVOKABLE void list(int offset=0);
    Q_INVOKABLE void open(const QString &path);
    Q_INVOKABLE void next();
    Q_INVOKABLE void previous();
    Q_INVOKABLE void closeBook();
    // Bounded close-time save; failure remains visible and never means success.
    Q_INVOKABLE void flush();
    // Restart from the physical beginning without rescanning a large document.
    Q_INVOKABLE void restart();
signals:
    void changed();
private:
    enum RequestKind { Catalog, Progress, Chunk };
    struct Cursor {int line; qint64 offset;};
    void cancel();
    void request(const QString &path,RequestKind kind);
    void acceptCatalog(const QJsonDocument &document, bool more);
    void acceptChunk(const QJsonObject &object);
    void chunk(int line,qint64 offset);
    void scheduleSave();
    void sendSave();
    QNetworkAccessManager m_net;
    QPointer<QNetworkReply> m_reply;
    QVariantList m_books;
    QString m_text,m_path,m_title,m_error,m_version;
    QList<qsizetype> m_segments{0,0};
    QTimer m_saveTimer;
    QPointer<QNetworkReply> m_saveReply;
    QJsonObject m_pendingSave;
    qint64 m_account=0;
    bool m_canSave=false;
    QList<Cursor> m_history;
    int m_offset=0,m_line=1,m_lineCount=0,m_segment=0;
    qint64 m_startOffset=0,m_nextOffset=0;
    quint64 m_generation=0;
    bool m_more=false,m_chunkMore=false,m_previousChunk=false;
};

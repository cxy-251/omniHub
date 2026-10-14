#pragma once
#include <QObject>
#include <QStringList>

// MediaQueue owns a bounded ordered list independently of the decoder. Mutations
// validate the entire list before publishing; navigation never opens files.
class MediaQueue : public QObject {
    Q_OBJECT
    Q_PROPERTY(QStringList entries READ entries NOTIFY changed)
    Q_PROPERTY(int currentIndex READ currentIndex NOTIFY changed)
    Q_PROPERTY(QString currentSource READ currentSource NOTIFY changed)
    Q_PROPERTY(QString error READ error NOTIFY errorChanged)
public:
    explicit MediaQueue(QObject *parent = nullptr) : QObject(parent) {}
    QStringList entries() const { return m_entries; }
    int currentIndex() const { return m_index; }
    QString currentSource() const { return m_index < 0 ? QString() : m_entries.at(m_index); }
    QString error() const { return m_error; }
    // Replace/restore up to 256 entries and 1MiB UTF-8 addresses, with valid index.
    Q_INVOKABLE bool replace(const QStringList &sources, int index = 0);
    // Select or move within bounds; repeated selection of an index is valid.
    Q_INVOKABLE bool select(int index);
    Q_INVOKABLE bool next();
    Q_INVOKABLE bool previous();
signals:
    void changed();
    void errorChanged();
private:
    QStringList m_entries;
    int m_index = -1;
    QString m_error;
};

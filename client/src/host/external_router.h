#pragma once
#include <QObject>
#include <QLocalServer>
#include <QLockFile>
#include <memory>
#include <QStringList>

// ExternalRouter owns one private per-config external window endpoint. A second
// process forwards bounded local paths and exits; the hub UI remains independent.
class ExternalRouter : public QObject {
    Q_OBJECT
public:
    explicit ExternalRouter(QObject *parent = nullptr);
    // 0 = forwarded, 1 = this process owns the window, -1 = explicit failure.
    int start(const QStringList &files);
    QString error() const { return m_error; }
    // Validate a whole batch of at most 32 existing local files/128KiB addresses.
    static bool localFiles(const QStringList &input, QStringList &output);
    ~ExternalRouter() override;
signals:
    void filesRequested(const QStringList &files);
private:
    QLocalServer m_server;
    std::unique_ptr<QLockFile> m_lock;
    QString m_name, m_error;
    bool m_owner = false;
    int m_connections = 0;
};

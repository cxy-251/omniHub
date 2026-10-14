#pragma once
#include <QObject>
#include <QVariantMap>

// NativeLifecycle hands the GUI off to a Core-only observer, preserving a
// bounded UI snapshot. The service retains ownership of the game process.
class NativeLifecycle : public QObject {
    Q_OBJECT
    Q_PROPERTY(QString error READ error NOTIFY errorChanged)
    Q_PROPERTY(QVariantMap restored READ restored CONSTANT)
public:
    explicit NativeLifecycle(bool restore, QObject *parent = nullptr);
    QString error() const { return m_error; }
    QVariantMap restored() const { return m_restored; }
    // Save state, start a detached observer, then terminate the GUI event loop.
    Q_INVOKABLE bool yieldToGame(const QString &sessionId, const QVariantMap &state);
    // Run without QGuiApplication/QML/OpenGL and reopen the GUI after completion.
    static int watchGame(int argc, char **argv);
signals:
    void errorChanged();
    // Observer identity for diagnostics only; never used to control game PIDs.
    void observerStarted(qint64 pid);
private:
    QString m_error;
    QVariantMap m_restored;
};

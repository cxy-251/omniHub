#pragma once

#include <QObject>
#include <QDBusConnection>

class MprisService : public QObject
{
    Q_OBJECT
    Q_CLASSINFO("D-Bus Interface", "org.mpris.MediaPlayer2.Player")

public:
    explicit MprisService(QObject *parent = nullptr);
    ~MprisService() override;

    Q_INVOKABLE void updateMetadata(const QString &title, const QString &artist, const QString &artUrl);
    Q_INVOKABLE void updatePlaybackStatus(const QString &status);

public slots:
    void Play();
    void Pause();
    void PlayPause();
    void Stop();
    void Next();
    void Previous();

signals:
    void playRequested();
    void pauseRequested();
    void stopRequested();

private:
    QString m_status = "Stopped";
};

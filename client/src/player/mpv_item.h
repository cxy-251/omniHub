#pragma once

#include <QQuickFramebufferObject>
#include <mpv/client.h>
#include <mpv/render_gl.h>

class MpvRenderer;

class MpvItem : public QQuickFramebufferObject
{
    Q_OBJECT
    Q_PROPERTY(QString source READ source WRITE setSource NOTIFY sourceChanged)
    Q_PROPERTY(double position READ position WRITE setPosition NOTIFY positionChanged)
    Q_PROPERTY(double duration READ duration NOTIFY durationChanged)
    Q_PROPERTY(double volume READ volume WRITE setVolume NOTIFY volumeChanged)
    Q_PROPERTY(double playbackRate READ playbackRate WRITE setPlaybackRate NOTIFY playbackRateChanged)
    Q_PROPERTY(bool playing READ isPlaying NOTIFY playingChanged)

public:
    explicit MpvItem(QQuickItem *parent = nullptr);
    ~MpvItem() override;

    Renderer *createRenderer() const override;

    QString source() const { return m_source; }
    void setSource(const QString &source);

    double position() const { return m_position; }
    void setPosition(double pos);

    double duration() const { return m_duration; }
    double volume() const { return m_volume; }
    void setVolume(double vol);

    double playbackRate() const { return m_playbackRate; }
    void setPlaybackRate(double rate);

    bool isPlaying() const { return m_playing; }

    Q_INVOKABLE void play();
    Q_INVOKABLE void pause();
    Q_INVOKABLE void stop();
    Q_INVOKABLE void seekRelative(double seconds);

signals:
    void sourceChanged();
    void positionChanged();
    void durationChanged();
    void volumeChanged();
    void playbackRateChanged();
    void playingChanged();

private:
    void initMpv();
    void onMpvEvents();

    mpv_handle *m_mpv = nullptr;
    mpv_render_context *m_mpvGl = nullptr;
    QString m_source;
    double m_position = 0.0;
    double m_duration = 0.0;
    double m_volume = 100.0;
    double m_playbackRate = 1.0;
    bool m_playing = false;

    friend class MpvRenderer;
};

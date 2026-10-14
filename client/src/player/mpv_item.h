#pragma once

#include <QQuickFramebufferObject>
#include <memory>
#include <QVariantList>

class MpvRenderer;
class MpvCore;
struct mpv_node;

// MpvItem owns GUI playback state; its renderer owns the OpenGL context.
class MpvItem : public QQuickFramebufferObject
{
    Q_OBJECT
    Q_PROPERTY(QString source READ source WRITE setSource NOTIFY sourceChanged)
    Q_PROPERTY(double position READ position WRITE setPosition NOTIFY positionChanged)
    Q_PROPERTY(double duration READ duration NOTIFY durationChanged)
    Q_PROPERTY(double volume READ volume WRITE setVolume NOTIFY volumeChanged)
    Q_PROPERTY(double playbackRate READ playbackRate WRITE setPlaybackRate NOTIFY playbackRateChanged)
    Q_PROPERTY(bool playing READ isPlaying NOTIFY playingChanged)
    Q_PROPERTY(QString error READ error NOTIFY errorChanged)
    Q_PROPERTY(bool pauseRequested READ pauseRequested NOTIFY pauseRequestedChanged)
    Q_PROPERTY(QVariantList tracks READ tracks NOTIFY tracksChanged)
    Q_PROPERTY(double subtitleDelay READ subtitleDelay WRITE setSubtitleDelay NOTIFY subtitleDelayChanged)
    Q_PROPERTY(double videoBrightness READ videoBrightness WRITE setVideoBrightness NOTIFY videoBrightnessChanged)

public:
    // Creates GUI state; media loading waits for a render-thread GL context.
    explicit MpvItem(QQuickItem *parent = nullptr);
    // Detaches callbacks; the last renderer releases the core after its GL context.
    ~MpvItem() override;

    // Called by Qt during GUI-blocked synchronization, never from application code.
    Renderer *createRenderer() const override;

    // Returns the requested media address, including a pending load.
    QString source() const { return m_source; }
    // Accepts paths/URLs up to 8,192 characters, excluding embedded nulls.
    void setSource(const QString &source);

    // Returns observed seconds, or the queued restore time before a file loads.
    double position() const { return m_position; }
    // Accepts finite seconds in [0, 31536000], clamped to the known duration.
    void setPosition(double pos);

    // Returns the latest duration in seconds, or zero before metadata is known.
    double duration() const { return m_duration; }
    // Returns the requested volume percentage.
    double volume() const { return m_volume; }
    // Accepts finite volume in [0, 100]; rejection is reported through error.
    void setVolume(double vol);

    // Returns the requested playback multiplier.
    double playbackRate() const { return m_playbackRate; }
    // Accepts finite speed in [0.25, 3]; rejection is reported through error.
    void setPlaybackRate(double rate);

    // Derived from observed pause and idle state, rather than command optimism.
    bool isPlaying() const { return m_playing; }
    // Returns the latest initialization, media or command error for QML display.
    QString error() const { return m_error; }
    // Returns pause intent even while a source is waiting for initialization.
    bool pauseRequested() const { return m_loadPaused; }
    // Bounded audio/subtitle descriptors from the loaded media, not file guesses.
    QVariantList tracks() const { return m_tracks; }
    // Subtitle timing adjustment in seconds; finite values within +/-60 accepted.
    double subtitleDelay() const { return m_subtitleDelay; }
    void setSubtitleDelay(double seconds);
    // Video color adjustment in [-100,100], independent of display backlight.
    double videoBrightness() const { return m_videoBrightness; }
    void setVideoBrightness(double value);
    // Select a known loaded track; subtitle -1 explicitly disables subtitles.
    Q_INVOKABLE void selectAudioTrack(int id);
    Q_INVOKABLE void selectSubtitleTrack(int id);
    // Load a local SRT/ASS/SSA/VTT file, limited to 8MiB, without shell commands.
    Q_INVOKABLE void addSubtitle(const QString &path);

    // Resumes playback, loading the current source again after a stop.
    Q_INVOKABLE void play();
    // Requests pause without destroying the decoder or current position.
    Q_INVOKABLE void pause();
    // Stops decoding; the source address remains available for a later play.
    Q_INVOKABLE void stop();
    // Applies a finite relative seek within +/-31536000 seconds.
    Q_INVOKABLE void seekRelative(double seconds);

signals:
    void sourceChanged();
    void positionChanged();
    void durationChanged();
    void volumeChanged();
    void playbackRateChanged();
    void playingChanged();
    void errorChanged();
    void pauseRequestedChanged();
    void tracksChanged();
    void subtitleDelayChanged();
    void videoBrightnessChanged();

private:
    void initMpv();
    void onMpvEvents();
    void setError(const QString &error);
    void rendererReady();
    void rendererUnavailable();
    void loadSource();
    void seek(double seconds, const char *mode);
    void setPaused(bool paused);
    void selectTrack(const char *type, int id);
    void readTracks();
    void updateTracks(const mpv_node &node);

    std::shared_ptr<MpvCore> m_core;
    QString m_source;
    double m_position = 0.0;
    double m_duration = 0.0;
    double m_volume = 100.0;
    double m_playbackRate = 1.0;
    bool m_playing = false;
    bool m_rendererReady = false;
    bool m_paused = true;
    bool m_idle = true;
    bool m_loadPaused = false;
    bool m_loaded = false;
    double m_restorePosition = -1;
    QString m_error;
    QVariantList m_tracks;
    double m_subtitleDelay = 0;
    double m_videoBrightness = 0;
    quint64 m_trackRevision = 0;
    int m_trackCount = 0;

    friend class MpvRenderer;
    friend class MpvCore;
};

#include "mpv_item.h"
#include <mpv/client.h>
#include <mpv/render_gl.h>
#include <QOpenGLContext>
#include <QOpenGLFramebufferObject>
#include <QOpenGLFunctions>
#include <QQuickOpenGLUtils>
#include <QQuickWindow>
#include <QPointer>
#include <QThread>
#include <QSGRendererInterface>
#include <atomic>
#include <cmath>
#include <cstring>

// The core lives on the GUI thread, but a renderer retains it until its GL
// context is freed. deleteLater avoids destroying the core on the render thread.
class MpvCore : public QObject
{
public:
    explicit MpvCore(MpvItem *item) : owner(item) {}
    ~MpvCore() override {
        if (handle) {
            mpv_set_wakeup_callback(handle, nullptr, nullptr);
            mpv_terminate_destroy(handle);
        }
    }
    void post(bool frame) {
        auto &pending = frame ? framePending : eventPending;
        if (pending.exchange(true)) return;
        QMetaObject::invokeMethod(this, [this, frame] {
            (frame ? framePending : eventPending).store(false);
            if (!owner) return;
            if (frame) owner->update(); else owner->onMpvEvents();
        }, Qt::QueuedConnection);
    }
    mpv_handle *handle = nullptr;
    QPointer<MpvItem> owner;
    std::atomic_bool renderAvailable{false};
private:
    std::atomic_bool framePending{false}, eventPending{false};
};

static void *getProcAddress(void *, const char *name)
{
    auto *context = QOpenGLContext::currentContext();
    return context ? reinterpret_cast<void *>(context->getProcAddress(name)) : nullptr;
}

// All render API calls and GL resource destruction stay on Qt's render thread.
class MpvRenderer : public QQuickFramebufferObject::Renderer
{
public:
    explicit MpvRenderer(std::shared_ptr<MpvCore> core)
        : m_core(std::move(core)) {}
    ~MpvRenderer() override {
        if (m_context) {
            m_core->renderAvailable.store(false);
            mpv_render_context_set_update_callback(m_context, nullptr, nullptr);
            mpv_render_context_free(m_context);
            auto *core = m_core.get();
            QMetaObject::invokeMethod(core, [core] {
                if (core->owner) core->owner->rendererUnavailable();
            }, Qt::QueuedConnection);
        }
    }
    void render() override {
        if (!m_attempted) initialize();
        auto *fbo = framebufferObject();
        if (!m_context || !fbo) {
            auto *context = QOpenGLContext::currentContext();
            if (context && fbo) {
                auto *functions = context->functions();
                functions->glClearColor(0, 0, 0, 1);
                functions->glClear(GL_COLOR_BUFFER_BIT);
            }
            if (context) QQuickOpenGLUtils::resetOpenGLState();
            return;
        }
        mpv_render_context_update(m_context);
        mpv_opengl_fbo target{static_cast<int>(fbo->handle()), fbo->width(), fbo->height(), 0};
        int flip = 1;
        mpv_render_param params[] = {
            {MPV_RENDER_PARAM_OPENGL_FBO, &target},
            {MPV_RENDER_PARAM_FLIP_Y, &flip},
            {MPV_RENDER_PARAM_INVALID, nullptr}
        };
        const int status = mpv_render_context_render(m_context, params);
        if (status < 0) reportError(status);
        QQuickOpenGLUtils::resetOpenGLState();
    }
    QOpenGLFramebufferObject *createFramebufferObject(const QSize &size) override {
        return new QOpenGLFramebufferObject(size);
    }
private:
    void reportError(int status) {
        if (m_reported) return;
        m_reported = true;
        auto *core = m_core.get();
        const QString message = QString::fromUtf8(mpv_error_string(status));
        QMetaObject::invokeMethod(core, [core, message] {
            if (core->owner) core->owner->setError(message);
        }, Qt::QueuedConnection);
    }
    void initialize() {
        m_attempted = true;
        if (!m_core || !m_core->handle || !QOpenGLContext::currentContext()) return;
        mpv_opengl_init_params gl{getProcAddress, nullptr};
        mpv_render_param params[] = {
            {MPV_RENDER_PARAM_API_TYPE, const_cast<char *>(MPV_RENDER_API_TYPE_OPENGL)},
            {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &gl},
            {MPV_RENDER_PARAM_INVALID, nullptr}
        };
        const int status = mpv_render_context_create(&m_context, m_core->handle, params);
        if (status < 0) { reportError(status); return; }
        m_core->renderAvailable.store(true);
        mpv_render_context_set_update_callback(m_context, [](void *data) {
            static_cast<MpvCore *>(data)->post(true);
        }, m_core.get());
        auto *core = m_core.get();
        QMetaObject::invokeMethod(core, [core] {
            if (core->owner) core->owner->rendererReady();
        }, Qt::QueuedConnection);
    }
    std::shared_ptr<MpvCore> m_core;
    mpv_render_context *m_context = nullptr;
    bool m_attempted = false;
    bool m_reported = false;
};

MpvItem::MpvItem(QQuickItem *parent) : QQuickFramebufferObject(parent)
{
    initMpv();
    const auto watchWindow = [this](QQuickWindow *window) {
        if (!window) return;
        QPointer<QQuickWindow> guard(window);
        connect(window, &QQuickWindow::sceneGraphInitialized, this, [this, guard] {
            if (guard && guard == this->window() && guard->rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL)
                setError(tr("播放器需要 OpenGL 渲染后端"));
        }, Qt::QueuedConnection);
    };
    connect(this, &QQuickItem::windowChanged, this, watchWindow);
    watchWindow(window());
}

MpvItem::~MpvItem()
{
    if (m_core) m_core->owner = nullptr;
}

QQuickFramebufferObject::Renderer *MpvItem::createRenderer() const
{
    return new MpvRenderer(m_core);
}

void MpvItem::setError(const QString &error)
{
    if (m_error == error) return;
    m_error = error;
    emit errorChanged();
}

void MpvItem::initMpv()
{
    m_core = std::shared_ptr<MpvCore>(new MpvCore(this), [](MpvCore *core) {
        if (QThread::currentThread() == core->thread()) delete core;
        else core->deleteLater();
    });
    auto *core = m_core.get();
    core->handle = mpv_create();
    if (!core->handle) { setError(tr("无法创建播放器")); m_core.reset(); return; }
    const char *options[][2] = {{"config", "no"}, {"load-scripts", "no"},
        {"terminal", "no"}, {"vo", "libmpv"}, {"keep-open", "yes"}};
    for (const auto &option : options) {
        const int status = mpv_set_option_string(core->handle, option[0], option[1]);
        if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    }
    int status = mpv_initialize(core->handle);
    if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    for (const auto &property : {"time-pos", "duration", "pause", "idle-active"}) {
        const mpv_format format = (std::strcmp(property, "pause") == 0 || std::strcmp(property, "idle-active") == 0) ? MPV_FORMAT_FLAG : MPV_FORMAT_DOUBLE;
        status = mpv_observe_property(core->handle, 0, property, format);
        if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    }
    mpv_set_wakeup_callback(core->handle, [](void *data) { static_cast<MpvCore *>(data)->post(false); }, core);
}

void MpvItem::rendererReady()
{
    m_rendererReady = true;
    setError({});
    loadSource();
}

void MpvItem::rendererUnavailable()
{
    m_rendererReady = false;
    m_restorePosition = m_position;
    if (!m_core) return;
    int paused = 1;
    const int status = mpv_set_property_async(m_core->handle, 0, "pause", MPV_FORMAT_FLAG, &paused);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}

void MpvItem::onMpvEvents()
{
    if (!m_core || !m_core->handle) return;
    // Bound one GUI turn; coalescing schedules remaining work without polling.
    for (int count = 0; count < 128; ++count) {
        auto *event = mpv_wait_event(m_core->handle, 0);
        if (event->event_id == MPV_EVENT_NONE) return;
        if (event->event_id == MPV_EVENT_PROPERTY_CHANGE && event->data) {
            auto *property = static_cast<mpv_event_property *>(event->data);
            if (!property->data) continue;
            if (property->format == MPV_FORMAT_DOUBLE) {
                const double value = *static_cast<double *>(property->data);
                if (!std::isfinite(value) || value < 0) continue;
                if (std::strcmp(property->name, "time-pos") == 0 && m_loaded && m_position != value) { m_position = value; emit positionChanged(); }
                if (std::strcmp(property->name, "duration") == 0 && m_duration != value) { m_duration = value; emit durationChanged(); }
            } else if (property->format == MPV_FORMAT_FLAG) {
                const bool value = *static_cast<int *>(property->data) != 0;
                if (std::strcmp(property->name, "pause") == 0) m_paused = value;
                if (std::strcmp(property->name, "idle-active") == 0) m_idle = value;
                const bool playing = !m_paused && !m_idle;
                if (playing != m_playing) { m_playing = playing; emit playingChanged(); }
            }
        } else if (event->event_id == MPV_EVENT_FILE_LOADED) {
            m_loaded = true;
            if (m_restorePosition >= 0) { setPosition(m_restorePosition); m_restorePosition = -1; }
            setPaused(m_loadPaused);
        } else if ((event->event_id == MPV_EVENT_COMMAND_REPLY || event->event_id == MPV_EVENT_SET_PROPERTY_REPLY) && event->error < 0) {
            setError(QString::fromUtf8(mpv_error_string(event->error)));
        } else if (event->event_id == MPV_EVENT_END_FILE && event->data) {
            auto *end = static_cast<mpv_event_end_file *>(event->data);
            // Losing the scene graph deliberately disables libmpv's VO. The
            // next renderer reloads the saved position instead of surfacing
            // that expected teardown as a corrupt-media error.
            if (end->reason == MPV_END_FILE_REASON_ERROR && m_core->renderAvailable.load())
                setError(QString::fromUtf8(mpv_error_string(end->error)));
        }
    }
    m_core->post(false);
}

void MpvItem::loadSource()
{
    if (!m_rendererReady || !m_core || m_source.isEmpty()) return;
    m_loaded = false;
    const QByteArray source = m_source.toUtf8();
    const char *command[] = {"loadfile", source.constData(), nullptr};
    const int status = mpv_command_async(m_core->handle, 0, command);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}

void MpvItem::setSource(const QString &source)
{
    if (source.size() > 8192 || source.contains(QChar::Null)) { setError(tr("媒体地址无效")); return; }
    if (source == m_source) return;
    stop();
    m_source = source;
    if (m_loadPaused) { m_loadPaused = false; emit pauseRequestedChanged(); }
    m_restorePosition = -1;
    setError({});
    emit sourceChanged();
    m_position = m_duration = 0;
    emit positionChanged(); emit durationChanged();
    loadSource();
    update();
}

void MpvItem::setPosition(double position)
{
    if (!std::isfinite(position) || position < 0 || position > 31536000) { setError(tr("播放位置无效")); return; }
    if (!m_loaded) {
        m_restorePosition = position;
        if (m_position != position) { m_position = position; emit positionChanged(); }
        return;
    }
    if (m_duration > 0) position = qMin(position, m_duration);
    seek(position, "absolute+exact");
}

void MpvItem::seek(double seconds, const char *mode)
{
    if (!std::isfinite(seconds) || std::abs(seconds) > 31536000) { setError(tr("播放位置无效")); return; }
    if (!m_core) return;
    const QByteArray value = QByteArray::number(seconds, 'g', 16);
    const char *command[] = {"seek", value.constData(), mode, nullptr};
    const int status = mpv_command_async(m_core->handle, 0, command);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}

void MpvItem::setVolume(double volume)
{
    if (!std::isfinite(volume) || volume < 0 || volume > 100) { setError(tr("音量无效")); return; }
    if (m_volume == volume) return;
    m_volume = volume; emit volumeChanged();
    if (m_core) { const int status = mpv_set_property_async(m_core->handle, 0, "volume", MPV_FORMAT_DOUBLE, &volume); if (status < 0) setError(QString::fromUtf8(mpv_error_string(status))); }
}

void MpvItem::setPlaybackRate(double rate)
{
    if (!std::isfinite(rate) || rate < 0.25 || rate > 4) { setError(tr("播放速度无效")); return; }
    if (m_playbackRate == rate) return;
    m_playbackRate = rate; emit playbackRateChanged();
    if (m_core) { const int status = mpv_set_property_async(m_core->handle, 0, "speed", MPV_FORMAT_DOUBLE, &rate); if (status < 0) setError(QString::fromUtf8(mpv_error_string(status))); }
}

void MpvItem::setPaused(bool paused)
{
    if (m_loadPaused != paused) { m_loadPaused = paused; emit pauseRequestedChanged(); }
    if (!m_core) return;
    int value = paused;
    const int status = mpv_set_property_async(m_core->handle, 0, "pause", MPV_FORMAT_FLAG, &value);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}

void MpvItem::play() { if (m_idle && !m_source.isEmpty()) loadSource(); setPaused(false); }
void MpvItem::pause() { setPaused(true); }
void MpvItem::stop() {
    m_loaded = false;
    if (!m_loadPaused) { m_loadPaused = true; emit pauseRequestedChanged(); }
    if (!m_core) return;
    const char *command[] = {"stop", nullptr};
    const int status = mpv_command_async(m_core->handle, 0, command);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}
void MpvItem::seekRelative(double seconds) { seek(seconds, "relative+exact"); }

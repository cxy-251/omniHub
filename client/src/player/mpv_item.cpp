#include <QElapsedTimer>
#include "mpv_item.h"
#include "media_address.h"
#include <mpv/client.h>
#include <mpv/render_gl.h>
#include <QOpenGLContext>
#include <QtGui/qopenglcontext_platform.h>
#include <QGuiApplication>
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
#include <QUrl>
#include <QFileInfo>
#include <limits>

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
        // QQuickFramebufferObject 已按 Qt 场景图方向处理 FBO，mpv 不能再翻转，否则画面上下颠倒。
        int flip = 0;
        // 不在渲染线程里等这一帧的显示时刻：多联放映的几个屏共用 Qt 一个渲染线程，逐个等待会叠加成丢帧。
        int block = 0;
        mpv_render_param params[] = {
            {MPV_RENDER_PARAM_OPENGL_FBO, &target},
            {MPV_RENDER_PARAM_FLIP_Y, &flip},
            {MPV_RENDER_PARAM_BLOCK_FOR_TARGET_TIME, &block},
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
        // VA-API 直出需要知道显示连接（xcb 下是 X11 Display）；不传时 mpv 打不开 VA 显示，只能软解。
        void *x11 = nullptr;
#ifdef Q_OS_LINUX
        if (auto *native = qGuiApp->nativeInterface<QNativeInterface::QX11Application>()) x11 = native->display();
#endif
        mpv_render_param params[] = {
            {MPV_RENDER_PARAM_API_TYPE, const_cast<char *>(MPV_RENDER_API_TYPE_OPENGL)},
            {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &gl},
            {x11 ? MPV_RENDER_PARAM_X11_DISPLAY : MPV_RENDER_PARAM_INVALID, x11},
            {MPV_RENDER_PARAM_INVALID, nullptr}
        };
        QElapsedTimer clock;
        clock.start();
        const int status = mpv_render_context_create(&m_context, m_core->handle, params);
        if (qEnvironmentVariableIsSet("OMNIHUB_DEV_STARTUP")) fprintf(stderr, "mpv render context %lld ms\n", clock.elapsed());
        if (status < 0) { reportError(status); return; }
        m_core->renderAvailable.store(true);
        const bool egl = QOpenGLContext::currentContext()->nativeInterface<QNativeInterface::QEGLContext>() != nullptr;
        mpv_render_context_set_update_callback(m_context, [](void *data) {
            static_cast<MpvCore *>(data)->post(true);
        }, m_core.get());
        auto *core = m_core.get();
        QMetaObject::invokeMethod(core, [core, egl] {
            if (core->owner) core->owner->rendererReady(egl);
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
    emit closing();
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

// Linux 只用 VA-API（Deck 的 AMD 显卡）；auto 会逐个尝试 CUDA 等接口并打印 "Cannot load libcuda.so.1"。
// 先以拷回方式（*-copy）起步；渲染环境是 EGL 时，rendererReady 改成 vaapi 直出：解码帧留在显存里直接贴图，
// 不经内存拷贝（三路 4K HEVC 实测从每路约 24 帧/秒提到约 41 帧/秒）。互通层只加载 vaapi，避免探测 CUDA。
#ifdef Q_OS_LINUX
static const char *kHwdec = "vaapi-copy";
static const char *kHwdecDirect = "vaapi";
static const char *kInterop = "vaapi";
#else
static const char *kHwdec = "auto-copy-safe";
static const char *kHwdecDirect = nullptr;
static const char *kInterop = "no";
#endif

void MpvItem::initMpv()
{
    m_core = std::shared_ptr<MpvCore>(new MpvCore(this), [](MpvCore *core) {
        if (QThread::currentThread() == core->thread()) delete core;
        else core->deleteLater();
    });
    auto *core = m_core.get();
    core->handle = mpv_create();
    if (!core->handle) { setError(tr("无法创建播放器")); m_core.reset(); return; }
    // 内存：短视频里有大量 4K HEVC，软件解码时每个线程各持几帧 4K 画面，三屏多联放映实测 1.2–1.6GB。
    // 硬解后拷回（auto-copy-safe，Linux 为 vaapi-copy）+ 软解回退限 2 线程 + 少预留硬解帧，三屏实测约 300MB，CPU 减半。
    const char *options[][2] = {{"config", "no"}, {"load-scripts", "no"},
        {"terminal", "no"}, {"vo", "libmpv"}, {"keep-open", "yes"},
        {"hwdec", kHwdec}, {"gpu-hwdec-interop", kInterop}, {"vd-lavc-threads", "2"}, {"hwdec-extra-frames", "2"}, {"video-latency-hacks", "yes"}};
    for (const auto &option : options) {
        const int status = mpv_set_option_string(core->handle, option[0], option[1]);
        if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    }
    // 调试用：OMNIHUB_MPV_OPTS="key=value,key=value" 覆盖上面的选项，用于测试内存/解码参数。
    for (const QString &pair : qEnvironmentVariable("OMNIHUB_MPV_OPTS").split(',', Qt::SkipEmptyParts)) {
        const int eq = pair.indexOf('=');
        if (eq > 0) mpv_set_option_string(core->handle, pair.left(eq).toUtf8().constData(), pair.mid(eq + 1).toUtf8().constData());
    }
    int status = mpv_initialize(core->handle);
    if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    for (const auto &property : {"time-pos", "duration", "pause", "idle-active", "eof-reached"}) {
        const mpv_format format = (std::strcmp(property, "time-pos") == 0 || std::strcmp(property, "duration") == 0) ? MPV_FORMAT_DOUBLE : MPV_FORMAT_FLAG;
        status = mpv_observe_property(core->handle, 0, property, format);
        if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    }
    status = mpv_observe_property(core->handle, 0, "track-list", MPV_FORMAT_NODE);
    if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    status = mpv_observe_property(core->handle, 0, "metadata", MPV_FORMAT_NODE);
    if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); m_core.reset(); return; }
    mpv_set_wakeup_callback(core->handle, [](void *data) { static_cast<MpvCore *>(data)->post(false); }, core);
}

void MpvItem::rendererReady(bool egl)
{
    m_rendererReady = true;
    if (egl && kHwdecDirect && m_core && qEnvironmentVariable("OMNIHUB_MPV_OPTS").indexOf("hwdec=") < 0)
        mpv_set_property_string(m_core->handle, "hwdec", kHwdecDirect);
    setError({});
    loadSource();
}

void MpvItem::setAudioOnly(bool on)
{
    if (m_audioOnly == on) return;
    m_audioOnly = on;
    if (m_core) mpv_set_property_string(m_core->handle, "vid", on ? "no" : "auto");
    emit audioOnlyChanged();
    loadSource();
}

void MpvItem::rendererUnavailable()
{
    m_rendererReady = false;
    if (m_audioOnly) return;   // 纯音频不依赖渲染环境
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
            if (std::strcmp(property->name, "track-list") == 0 && m_loaded) {
                updateTracks(*static_cast<mpv_node *>(property->data));
            } else if (std::strcmp(property->name, "metadata") == 0 && m_loaded && property->format == MPV_FORMAT_NODE) {
                updateMetadata(*static_cast<mpv_node *>(property->data));
            } else if (property->format == MPV_FORMAT_DOUBLE) {
                const double value = *static_cast<double *>(property->data);
                if (!std::isfinite(value) || value < 0) continue;
                if (std::strcmp(property->name, "time-pos") == 0 && m_loaded && m_position != value) { m_position = value; emit positionChanged(); }
                if (std::strcmp(property->name, "duration") == 0 && m_duration != value) { m_duration = value; emit durationChanged(); }
            } else if (property->format == MPV_FORMAT_FLAG) {
                const bool value = *static_cast<int *>(property->data) != 0;
                if (std::strcmp(property->name, "pause") == 0) m_paused = value;
                if (std::strcmp(property->name, "idle-active") == 0) m_idle = value;
                if (std::strcmp(property->name, "eof-reached") == 0 && m_loaded && m_eof != value) {
                    m_eof = value;
                    if (value) { emit mediaCompleted(); emit playbackEnded(); }
                }
                const bool playing = !m_paused && !m_idle;
                if (playing != m_playing) { m_playing = playing; emit playingChanged(); }
            }
        } else if (event->event_id == MPV_EVENT_FILE_LOADED) {
            m_loaded = true;
            m_loading = false;
            emit mediaLoadedChanged();
            readTracks();
            if (m_restorePosition >= 0) { setPosition(m_restorePosition); m_restorePosition = -1; }
            setPaused(m_loadPaused);
        } else if (event->event_id == MPV_EVENT_GET_PROPERTY_REPLY && event->reply_userdata == m_trackRevision && m_loaded) {
            auto *property = static_cast<mpv_event_property *>(event->data);
            if (event->error < 0) setError(QString::fromUtf8(mpv_error_string(event->error)));
            else if (property && property->data && property->format == MPV_FORMAT_DOUBLE && std::strcmp(property->name, "time-pos") == 0) {
                const double value = *static_cast<double *>(property->data);
                if (std::isfinite(value) && value >= 0 && value <= 31536000) emit seekCompleted(value);
            } else if (property && property->data && property->format == MPV_FORMAT_NODE) {
                if (std::strcmp(property->name, "track-list") == 0) updateTracks(*static_cast<mpv_node *>(property->data));
                else if (std::strcmp(property->name, "metadata") == 0) updateMetadata(*static_cast<mpv_node *>(property->data));
            }
        } else if (event->event_id == MPV_EVENT_PLAYBACK_RESTART && m_seekPending && m_loaded) {
            m_seekPending = false;
            const int status = mpv_get_property_async(m_core->handle, m_trackRevision, "time-pos", MPV_FORMAT_DOUBLE);
            if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
        } else if ((event->event_id == MPV_EVENT_COMMAND_REPLY || event->event_id == MPV_EVENT_SET_PROPERTY_REPLY) && event->error < 0) {
            setError(QString::fromUtf8(mpv_error_string(event->error)));
        } else if (event->event_id == MPV_EVENT_END_FILE && event->data) {
            auto *end = static_cast<mpv_event_end_file *>(event->data);
            // Losing the scene graph deliberately disables libmpv's VO. The
            // next renderer reloads the saved position instead of surfacing
            // that expected teardown as a corrupt-media error.
            if (end->reason == MPV_END_FILE_REASON_ERROR && (m_core->renderAvailable.load() || m_audioOnly))
                { m_loading = false; m_loaded = false; emit mediaLoadedChanged(); setError(QString::fromUtf8(mpv_error_string(end->error))); }
        }
    }
    m_core->post(false);
}

void MpvItem::loadSource()
{
    if ((!m_rendererReady && !m_audioOnly) || !m_core || m_source.isEmpty() || m_loading) return;
    m_loaded = false;
    m_loading = true;
    m_seekPending = false;
    emit mediaLoadedChanged();
    m_eof = false;
    ++m_trackRevision;
    const QByteArray source = m_source.toUtf8();
    const char *command[] = {"loadfile", source.constData(), nullptr};
    const int status = mpv_command_async(m_core->handle, 0, command);
    if (status < 0) { m_loading = false; setError(QString::fromUtf8(mpv_error_string(status))); }
}

void MpvItem::setSource(const QString &source)
{
    const auto parsed = source.isEmpty() ? std::optional<QString>(QString()) : mediaAddress(source);
    if (!parsed) { setError(tr("媒体地址无效")); return; }
    const QString address = *parsed;
    if (address == m_source) return;
    emit sourceChanging();
    stop();
    if (!m_mediaMetadata.isEmpty()) { m_mediaMetadata.clear(); emit mediaMetadataChanged(); }
    m_source = address;
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
        emit seekRequested();
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
    else { m_seekPending = true; emit seekRequested(); }
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
    if (!std::isfinite(rate) || rate < 0.25 || rate > 3) { setError(tr("播放速度无效")); return; }
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

void MpvItem::play() { if (!m_loaded && !m_loading && !m_source.isEmpty()) loadSource(); else if (m_eof) setPosition(0); setPaused(false); }
void MpvItem::restart() { if (m_source.isEmpty()) return; stop(); m_restorePosition = 0; setPaused(false); loadSource(); }
void MpvItem::pause() { setPaused(true); }
void MpvItem::stop() {
    emit stopping();
    m_loaded = false;
    m_loading = false;
    m_seekPending = false;
    emit mediaLoadedChanged();
    m_eof = false;
    ++m_trackRevision;
    if (!m_tracks.isEmpty()) { m_tracks.clear(); emit tracksChanged(); }
    m_trackCount = 0;
    if (!m_loadPaused) { m_loadPaused = true; emit pauseRequestedChanged(); }
    if (!m_core) return;
    const char *command[] = {"stop", nullptr};
    const int status = mpv_command_async(m_core->handle, 0, command);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}
void MpvItem::seekRelative(double seconds) { seek(seconds, "relative+exact"); }

namespace {
const mpv_node *trackField(const mpv_node &node, const char *name) {
    if (node.format != MPV_FORMAT_NODE_MAP || !node.u.list || node.u.list->num < 0 || node.u.list->num > 64) return nullptr;
    for (int i = 0; i < node.u.list->num; ++i)
        if (node.u.list->keys[i] && std::strncmp(node.u.list->keys[i], name, std::strlen(name) + 1) == 0) return &node.u.list->values[i];
    return nullptr;
}
QString trackText(const mpv_node *node) {
    if (!node || node->format != MPV_FORMAT_STRING || !node->u.string) return {};
    int length = 0;
    while (length < 512 && node->u.string[length]) ++length;
    return QString::fromUtf8(node->u.string, length);
}
}

void MpvItem::readTracks() {
    if (!m_core || !m_loaded) return;
    const int status = mpv_get_property_async(m_core->handle, m_trackRevision, "track-list", MPV_FORMAT_NODE);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
    const int metadataStatus = mpv_get_property_async(m_core->handle, m_trackRevision, "metadata", MPV_FORMAT_NODE);
    if (metadataStatus < 0) setError(QString::fromUtf8(mpv_error_string(metadataStatus)));
}

void MpvItem::updateMetadata(const mpv_node &node) {
    if (node.format != MPV_FORMAT_NODE_MAP || !node.u.list || node.u.list->num < 0 || node.u.list->num > 128) return;
    QVariantMap metadata;
    for (int i = 0; i < node.u.list->num; ++i) {
        const auto &value = node.u.list->values[i];
        mpv_node key{}; key.format = MPV_FORMAT_STRING; key.u.string = node.u.list->keys[i];
        const QString name = trackText(&key).toLower();
        if (name == "title" || name == "artist" || name == "album") metadata.insert(name, trackText(&value));
    }
    if (metadata != m_mediaMetadata) { m_mediaMetadata = metadata; emit mediaMetadataChanged(); }
}

void MpvItem::updateTracks(const mpv_node &node) {
    if (node.format != MPV_FORMAT_NODE_ARRAY || !node.u.list || node.u.list->num < 0 || node.u.list->num > 256) { setError(tr("媒体轨道数量或格式无效")); return; }
    m_trackCount = node.u.list->num;
    QVariantList tracks;
    for (int i = 0; i < node.u.list->num; ++i) {
        const auto &entry = node.u.list->values[i];
        const auto *id = trackField(entry, "id");
        const QString type = trackText(trackField(entry, "type"));
        if (type != "audio" && type != "sub") continue;
        if (!id || id->format != MPV_FORMAT_INT64 || id->u.int64 <= 0 || id->u.int64 > std::numeric_limits<int>::max()) { setError(tr("媒体轨道编号无效")); return; }
        const auto *selected = trackField(entry, "selected");
        const QString title = trackText(trackField(entry, "title"));
        const QString language = trackText(trackField(entry, "lang"));
        const QString label = QString::number(id->u.int64) + (language.isEmpty() ? "" : " · " + language) + (title.isEmpty() ? "" : " · " + title);
        tracks.append(QVariantMap{{"id", static_cast<int>(id->u.int64)}, {"type", type}, {"label", label}, {"selected", selected && selected->format == MPV_FORMAT_FLAG && selected->u.flag != 0}});
    }
    if (tracks != m_tracks) { m_tracks = tracks; emit tracksChanged(); }
}

void MpvItem::selectTrack(const char *type, int id) {
    bool valid = std::strcmp(type, "sub") == 0 && id == -1;
    for (const auto &track : m_tracks) {
        const auto entry = track.toMap();
        if (entry.value("type").toString() == QString::fromLatin1(type) && entry.value("id").toInt() == id) valid = true;
    }
    if (!valid || !m_loaded || !m_core) { setError(tr("请选择当前媒体已有轨道")); return; }
    const char *property = std::strcmp(type, "sub") == 0 ? "sid" : "aid";
    int status;
    if (id == -1) { const char *value = "no"; status = mpv_set_property_async(m_core->handle, 0, property, MPV_FORMAT_STRING, &value); }
    else { int64_t value = id; status = mpv_set_property_async(m_core->handle, 0, property, MPV_FORMAT_INT64, &value); }
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}
void MpvItem::selectAudioTrack(int id) { selectTrack("audio", id); }
void MpvItem::selectSubtitleTrack(int id) { selectTrack("sub", id); }

void MpvItem::setSubtitleDelay(double seconds) {
    if (!std::isfinite(seconds) || seconds < -60 || seconds > 60) { setError(tr("字幕延迟应在 -60 到 60 秒之间")); return; }
    if (!m_core || m_subtitleDelay == seconds) return;
    const int status = mpv_set_property_async(m_core->handle, 0, "sub-delay", MPV_FORMAT_DOUBLE, &seconds);
    if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); return; }
    m_subtitleDelay = seconds; emit subtitleDelayChanged();
}

void MpvItem::setVideoBrightness(double value) {
    if (!std::isfinite(value) || value < -100 || value > 100) { setError(tr("画面亮度范围无效")); return; }
    if (!m_core || value == m_videoBrightness) return;
    const int status = mpv_set_property_async(m_core->handle, 0, "brightness", MPV_FORMAT_DOUBLE, &value);
    if (status < 0) { setError(QString::fromUtf8(mpv_error_string(status))); return; }
    m_videoBrightness = value; emit videoBrightnessChanged();
}

void MpvItem::addSubtitle(const QString &path) {
    const auto address = mediaAddress(path);
    if (!address) { setError(tr("字幕地址无效")); return; }
    const QString local = *address;
    const QFileInfo file(local);
    const QString suffix = file.suffix().toLower();
    if (!m_core || !m_loaded || m_trackCount >= 256 || local.size() > 8192 || local.contains(QChar::Null) || !file.isAbsolute() || !file.isFile() || file.size() > 8 * 1024 * 1024 || (suffix != "srt" && suffix != "ass" && suffix != "ssa" && suffix != "vtt")) { setError(tr("请选择不超过 8MiB 的本地 SRT/ASS/SSA/VTT 字幕，媒体轨道最多 256 条")); return; }
    const QByteArray name = local.toUtf8();
    const char *command[] = {"sub-add", name.constData(), "select", nullptr};
    const int status = mpv_command_async(m_core->handle, 0, command);
    if (status < 0) setError(QString::fromUtf8(mpv_error_string(status)));
}

QVariantMap MpvItem::stats() const {
    QVariantMap out;
    if (!m_core || !m_core->handle) return out;
    for (const char *name : {"frame-drop-count", "decoder-frame-drop-count", "vo-delayed-frame-count", "width", "height"}) {
        int64_t value = 0;
        if (mpv_get_property(m_core->handle, name, MPV_FORMAT_INT64, &value) >= 0) out.insert(QString::fromLatin1(name), static_cast<qlonglong>(value));
    }
    for (const char *name : {"estimated-vf-fps", "container-fps", "avsync"}) {
        double value = 0;
        if (mpv_get_property(m_core->handle, name, MPV_FORMAT_DOUBLE, &value) >= 0) out.insert(QString::fromLatin1(name), value);
    }
    for (const char *name : {"hwdec-current", "video-codec"}) {
        if (char *value = mpv_get_property_string(m_core->handle, name)) { out.insert(QString::fromLatin1(name), QString::fromUtf8(value)); mpv_free(value); }
    }
    return out;
}

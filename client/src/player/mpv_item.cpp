#include "mpv_item.h"
#include <QOpenGLFramebufferObject>
#include <QQuickWindow>
#include <QDebug>

static void *get_proc_address(void *ctx, const char *name)
{
    Q_UNUSED(ctx);
    QOpenGLContext *glctx = QOpenGLContext::currentContext();
    if (!glctx) return nullptr;
    return reinterpret_cast<void *>(glctx->getProcAddress(QByteArray(name)));
}

class MpvRenderer : public QQuickFramebufferObject::Renderer
{
public:
    explicit MpvRenderer(const MpvItem *item) : m_item(item) {}

    void render() override
    {
        if (!m_item || !m_item->m_mpvGl) return;
        QOpenGLFramebufferObject *fbo = framebufferObject();
        if (!fbo) return;

        mpv_opengl_fbo mpvFbo{
            static_cast<int>(fbo->handle()),
            fbo->width(),
            fbo->height(),
            0
        };

        int flip_y = 1;
        mpv_render_param params[] = {
            {MPV_RENDER_PARAM_OPENGL_FBO, &mpvFbo},
            {MPV_RENDER_PARAM_FLIP_Y, &flip_y},
            {MPV_RENDER_PARAM_INVALID, nullptr}
        };

        mpv_render_context_render(m_item->m_mpvGl, params);
    }

    QOpenGLFramebufferObject *createFramebufferObject(const QSize &size) override
    {
        return new QOpenGLFramebufferObject(size, QOpenGLFramebufferObject::CombinedDepthStencil);
    }

private:
    const MpvItem *m_item;
};

MpvItem::MpvItem(QQuickItem *parent) : QQuickFramebufferObject(parent)
{
    initMpv();
    connect(this, &QQuickItem::windowChanged, this, [this](QQuickWindow *win) {
        if (win) {
            connect(win, &QQuickWindow::beforeRendering, this, &MpvItem::onMpvEvents, Qt::DirectConnection);
        }
    });
}

MpvItem::~MpvItem()
{
    if (m_mpvGl) {
        mpv_render_context_free(m_mpvGl);
    }
    if (m_mpv) {
        mpv_terminate_destroy(m_mpv);
    }
}

QQuickFramebufferObject::Renderer *MpvItem::createRenderer() const
{
    return new MpvRenderer(this);
}

void MpvItem::initMpv()
{
    m_mpv = mpv_create();
    if (!m_mpv) return;

    mpv_set_option_string(m_mpv, "terminal", "yes");
    mpv_set_option_string(m_mpv, "vo", "libmpv");
    mpv_set_option_string(m_mpv, "keep-open", "yes");
    mpv_initialize(m_mpv);

    mpv_opengl_init_params glParams{get_proc_address, nullptr};
    mpv_render_param params[]{
        {MPV_RENDER_PARAM_API_TYPE, const_cast<char *>(MPV_RENDER_API_TYPE_OPENGL)},
        {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &glParams},
        {MPV_RENDER_PARAM_INVALID, nullptr}
    };

    if (mpv_render_context_create(&m_mpvGl, m_mpv, params) < 0) {
        qWarning() << "Failed to initialize mpv OpenGL context";
    }

    mpv_render_context_set_update_callback(m_mpvGl, [](void *ctx) {
        QMetaObject::invokeMethod(static_cast<MpvItem *>(ctx), "update");
    }, this);
}

void MpvItem::onMpvEvents()
{
    if (!m_mpv) return;
    while (true) {
        mpv_event *event = mpv_wait_event(m_mpv, 0);
        if (event->event_id == MPV_EVENT_NONE) break;
        if (event->event_id == MPV_EVENT_PROPERTY_CHANGE) {
            auto *prop = static_cast<mpv_event_property *>(event->data);
            if (strcmp(prop->name, "time-pos") == 0 && prop->format == MPV_FORMAT_DOUBLE) {
                m_position = *static_cast<double *>(prop->data);
                emit positionChanged();
            } else if (strcmp(prop->name, "duration") == 0 && prop->format == MPV_FORMAT_DOUBLE) {
                m_duration = *static_cast<double *>(prop->data);
                emit durationChanged();
            }
        }
    }
}

void MpvItem::setSource(const QString &source)
{
    if (m_source == source) return;
    m_source = source;
    emit sourceChanged();

    if (m_mpv && !source.isEmpty()) {
        const QByteArray bytes = source.toUtf8();
        const char *cmd[] = {"loadfile", bytes.constData(), nullptr};
        mpv_command_async(m_mpv, 0, cmd);
        m_playing = true;
        emit playingChanged();
    }
}

void MpvItem::setPosition(double pos)
{
    if (!m_mpv) return;
    QByteArray posStr = QByteArray::number(pos);
    const char *cmd[] = {"seek", posStr.constData(), "absolute", nullptr};
    mpv_command_async(m_mpv, 0, cmd);
}

void MpvItem::setVolume(double vol)
{
    m_volume = vol;
    emit volumeChanged();
    if (!m_mpv) return;
    mpv_set_property(m_mpv, "volume", MPV_FORMAT_DOUBLE, &m_volume);
}

void MpvItem::setPlaybackRate(double rate)
{
    m_playbackRate = rate;
    emit playbackRateChanged();
    if (!m_mpv) return;
    mpv_set_property(m_mpv, "speed", MPV_FORMAT_DOUBLE, &m_playbackRate);
}

void MpvItem::play()
{
    if (!m_mpv) return;
    int pauseVal = 0;
    mpv_set_property(m_mpv, "pause", MPV_FORMAT_FLAG, &pauseVal);
    m_playing = true;
    emit playingChanged();
}

void MpvItem::pause()
{
    if (!m_mpv) return;
    int pauseVal = 1;
    mpv_set_property(m_mpv, "pause", MPV_FORMAT_FLAG, &pauseVal);
    m_playing = false;
    emit playingChanged();
}

void MpvItem::stop()
{
    if (!m_mpv) return;
    const char *cmd[] = {"stop", nullptr};
    mpv_command_async(m_mpv, 0, cmd);
    m_playing = false;
    emit playingChanged();
}

void MpvItem::seekRelative(double seconds)
{
    if (!m_mpv) return;
    QByteArray secStr = QByteArray::number(seconds);
    const char *cmd[] = {"seek", secStr.constData(), "relative", nullptr};
    mpv_command_async(m_mpv, 0, cmd);
}

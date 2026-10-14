#include "native_lifecycle.h"
#include "backend_api.h"
#include <QCoreApplication>
#include <QFile>
#include <QSaveFile>
#include <QDir>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLockFile>
#include <QProcess>
#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QTimer>
#include <cmath>
#include <memory>

namespace {
std::unique_ptr<QLockFile> handoffLock;
QString statePath() { return QDir(BackendApi::configurationDirectory()).filePath("native-ui.json"); }
bool validSession(const QString &id) {
    if (id.isEmpty() || id.size() > 128) return false;
    for (const auto character : id) if (!character.isLetterOrNumber()) return false;
    return true;
}
}
NativeLifecycle::NativeLifecycle(bool restore, QObject *parent) : QObject(parent) {
    if (!restore) return;
    if (QCoreApplication::arguments().contains("--session-uncertain")) m_error = "游戏会话服务不可用，无法确认进程状态，请重新连接服务";
    if (QCoreApplication::arguments().contains("--statistics-failed")) m_error = "游戏已退出，但游玩时长保存失败，请检查服务与用户状态存储";
    QFile file(statePath());
    if (!file.exists()) return;
    if (file.size() > 16384 || !file.open(QIODevice::ReadOnly)) { m_error = "无法读取原生界面恢复状态"; return; }
    QJsonParseError parse;
    const auto bytes = file.read(16385);
    if (bytes.size() > 16384 || file.error() != QFile::NoError) { m_error = "原生界面恢复状态超限或读取失败"; return; }
    const auto document = QJsonDocument::fromJson(bytes, &parse);
    if (parse.error != QJsonParseError::NoError || !document.isObject()) { m_error = "原生界面恢复状态无效"; return; }
    const auto state = document.object();
    const double position = state.value("position").toDouble(-1);
    const double page = state.value("page").toDouble(-1);
    if (!state.value("source").isString() || state.value("source").toString().size() > 4096 || !state.value("paused").isBool() || !state.value("position").isDouble() || !std::isfinite(position) || position < 0 || position > 31536000 || !state.value("page").isDouble() || !std::isfinite(page) || page < 0 || page > 2 || std::floor(page) != page) { m_error = "原生界面恢复字段无效"; return; }
    m_restored = document.object().toVariantMap();
}

bool NativeLifecycle::yieldToGame(const QString &sessionId, const QVariantMap &state) {
    auto fail = [&](const QString &message) { m_error = message; emit errorChanged(); return false; };
    const QString source = state.value("source").toString();
    const double position = state.value("position").toDouble();
    const int page = state.value("page").toInt();
    if (!validSession(sessionId) || source.size() > 4096 || !std::isfinite(position) || position < 0 || position > 31536000 || page < 0 || page > 2) return fail("游戏或恢复状态无效");
    const QString directory = BackendApi::configurationDirectory();
    if (!QDir().mkpath(directory)) return fail("无法准备界面状态目录");
    auto lock = std::make_unique<QLockFile>(QDir(directory).filePath("native-yield.lock"));
    lock->setStaleLockTime(0);
    if (!lock->tryLock()) return fail("另一个客户端正在让出资源");
    QSaveFile output(statePath());
    if (!output.open(QIODevice::WriteOnly) || !output.setPermissions(QFile::ReadOwner | QFile::WriteOwner)) return fail("无法保存界面恢复状态");
    const QByteArray data = QJsonDocument(QJsonObject{{"source", source}, {"position", position}, {"paused", state.value("paused").toBool()}, {"page", page}}).toJson(QJsonDocument::Compact);
    if (output.write(data) != data.size() || !output.commit()) return fail("界面状态保存失败");
    qint64 observerPid = 0;
    QProcess observer;
    observer.setProgram(QCoreApplication::applicationFilePath());
    observer.setArguments({"--watch-game", sessionId});
    observer.setStandardInputFile(QProcess::nullDevice());
    observer.setStandardOutputFile(QProcess::nullDevice());
    observer.setStandardErrorFile(QProcess::nullDevice());
    if (!observer.startDetached(&observerPid)) return fail("游戏观察器启动失败");
    // Held until after application destruction; the observer must not race a
    // quick game exit by creating a second heavyweight GUI during teardown.
    handoffLock = std::move(lock);
    emit observerStarted(observerPid);
    QCoreApplication::quit();
    return true;
}

int NativeLifecycle::watchGame(int argc, char **argv) {
    QCoreApplication application(argc, argv);
    if (argc != 3 || !validSession(QString::fromLocal8Bit(argv[2]))) return 2;
    const QString id = QString::fromLocal8Bit(argv[2]);
    QNetworkAccessManager network;
    QTimer timer;
    timer.setInterval(1000);
    bool pending = false, finished = false, uncertain = false, statisticsFailed = false;
    int failures = 0;
    QNetworkRequest endpoint = BackendApi::localRequest("/api/games/session?id=" + id);
    auto restore = [&]() {
        QLockFile barrier(QDir(BackendApi::configurationDirectory()).filePath("native-yield.lock"));
        barrier.setStaleLockTime(0);
        if (!barrier.tryLock()) return;
        barrier.unlock();
        QStringList arguments{"--resume-ui"};
        if (uncertain) arguments << "--session-uncertain";
        if (statisticsFailed) arguments << "--statistics-failed";
        const bool started = QProcess::startDetached(QCoreApplication::applicationFilePath(), arguments);
        application.exit(started ? 0 : 3);
    };
    QObject::connect(&timer, &QTimer::timeout, &application, [&]() {
        if (finished) { restore(); return; }
        if (pending) return;
        pending = true;
        auto *reply = network.get(endpoint);
        reply->setReadBufferSize(16385);
        QObject::connect(reply, &QIODevice::readyRead, reply, [reply]() { if (reply->bytesAvailable() > 16384) reply->abort(); });
        QObject::connect(reply, &QNetworkReply::finished, &application, [&, reply]() {
            pending = false;
            QJsonParseError parse;
            const auto document = QJsonDocument::fromJson(reply->readAll(), &parse);
            const QString state = document.object().value("state").toString();
            if (reply->error() == QNetworkReply::NoError && parse.error == QJsonParseError::NoError && document.isObject() && document.object().value("id").toString() == id && (state == "running" || state == "stopping" || state == "exited")) {
                failures = 0;
                finished = document.object().value("state").toString() == "exited";
                statisticsFailed = finished && !document.object().value("persistence_error").toString().isEmpty();
            } else {
                // A missing service/session is not proof of process exit. Retry
                // discovery, then return control to the user with explicit uncertainty.
                endpoint = BackendApi::localRequest("/api/games/session?id=" + id);
                if (++failures >= 15) { uncertain = true; finished = true; }
            }
            reply->deleteLater();
            if (finished) restore();
        });
    });
    timer.start();
    const int result = application.exec();
    return result;
}

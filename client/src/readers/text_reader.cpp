#include "text_reader.h"
#include "../backend_api.h"
#include <QEventLoop>
#include <QThread>
#include <QJsonArray>
#include <QJsonDocument>
#include <QStringList>
#include <QUrl>

TextReader::TextReader(QObject *parent) : QObject(parent) {
    m_saveTimer.setSingleShot(true);
    m_saveTimer.setInterval(500);
    connect(&m_saveTimer, &QTimer::timeout, this, &TextReader::sendSave);
}

TextReader::~TextReader() {
    cancel();
    // A nested loop after app.exec() has returned cannot finish network work.
    // Main.qml flushes before window close; Loader teardown can flush here.
    if (QThread::currentThread()->loopLevel() > 0) flush();
    if (m_saveReply) {
        m_saveReply->disconnect(this);
        m_saveReply->abort();
    }
}

void TextReader::scheduleSave() {
    if (!m_canSave || m_version.isEmpty() || m_account <= 0) return;
    // Store the chunk's physical cursor. Layout segments intentionally restore
    // to the beginning of that chunk; normalized newlines are not byte offsets.
    m_pendingSave = {{"document_id", m_path}, {"start_line", m_line},
                     {"byte_offset", m_startOffset}, {"file_version", m_version},
                     {"account_id", m_account}};
    m_saveTimer.start();
}

void TextReader::sendSave() {
    m_saveTimer.stop();
    if (m_saveReply || m_pendingSave.isEmpty()) return;
    auto body = m_pendingSave;
    m_pendingSave = {};
    auto request = BackendApi::localRequest("/api/media/text/progress");
    request.setRawHeader("X-Omni-Account-ID", QByteArray::number(body.take("account_id").toInteger()));
    auto *reply = m_net.post(request, QJsonDocument(body).toJson(QJsonDocument::Compact));
    m_saveReply = reply;
    reply->setReadBufferSize(8193);
    connect(reply, &QIODevice::readyRead, reply, [reply] {
        if (reply->bytesAvailable() > 8192) reply->abort();
    });
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        m_saveReply = nullptr;
        reply->deleteLater();
        if (reply->error() != QNetworkReply::NoError) {
            m_error = "阅读位置保存失败：" + reply->errorString();
            emit changed();
        }
        sendSave();
    });
}

void TextReader::flush() {
    sendSave();
    if (!m_saveReply) return;
    QEventLoop loop;
    QTimer timeout;
    timeout.setSingleShot(true);
    connect(&timeout, &QTimer::timeout, &loop, &QEventLoop::quit);
    connect(&m_net, &QNetworkAccessManager::finished, &loop, [this, &loop] {
        if (!m_saveReply && m_pendingSave.isEmpty()) loop.quit();
    });
    timeout.start(1000);
    loop.exec();
    if (m_saveReply) {
        m_error = "阅读位置尚未保存，服务响应超时";
        emit changed();
    }
}

void TextReader::cancel() {
    ++m_generation;
    if (!m_reply) return;
    auto *reply = m_reply.data();
    m_reply = nullptr;
    reply->abort();
    reply->deleteLater();
}

void TextReader::list(int offset) {
    if (offset < 0 || offset > 1000000) return;
    // Navigation must remain responsive while an earlier snapshot is in flight.
    sendSave();
    cancel();
    m_path.clear();
    m_books.clear();
    m_text.clear();
    m_segments = {0, 0};
    m_history.clear();
    m_offset = offset;
    m_error.clear();
    request("/api/media/text/list?limit=24&offset=" + QString::number(offset), Catalog);
}

void TextReader::open(const QString &path) {
    QString name, ext;
    for (const auto &book : m_books) {
        const auto data = book.toMap();
        if (data.value("rel_path").toString() != path) continue;
        name = data.value("name").toString();
        ext = data.value("ext").toString().toLower();
        break;
    }
    if (name.isEmpty() || path.size() > 4096 || path.contains(QChar::Null)) {
        m_error = "请选择当前书架中的文档";
        emit changed();
        return;
    }
    if (ext != ".txt" && ext != ".md" && ext != ".rst") {
        m_error = "EPUB 原生章节阅读尚在实现，当前支持 TXT/Markdown/RST 源文本";
        emit changed();
        return;
    }
    sendSave();
    cancel();
    m_canSave = false;
    m_account = 0;
    m_version.clear();
    m_path = path;
    m_title = name;
    m_error.clear();
    m_text.clear();
    m_segments = {0, 0};
    m_segment = 0;
    m_history.clear();
    request("/api/media/text/progress?document_id=" + QString::fromLatin1(QUrl::toPercentEncoding(path)), Progress);
}

void TextReader::chunk(int line, qint64 offset) {
    m_line = line;
    m_startOffset = offset;
    m_segment = 0;
    m_text.clear();
    m_segments = {0, 0};
    m_chunkMore = false;
    m_previousChunk = false;
    QString path = "/api/media/text/chunk?path=" + QString::fromLatin1(QUrl::toPercentEncoding(m_path)) +
                   "&start=" + QString::number(line) + "&limit=80";
    if (offset >= 0) path += "&offset=" + QString::number(offset);
    request(path, Chunk);
}

void TextReader::next() {
    if (loading()) return;
    if (m_segment + 2 < m_segments.size()) {
        ++m_segment;
        emit changed();
        return;
    }
    if (!m_chunkMore) return;
    if (m_history.size() >= 256) m_history.removeFirst();
    m_history.append({m_line, m_startOffset});
    chunk(m_line + m_lineCount, m_nextOffset);
}

void TextReader::previous() {
    if (loading()) return;
    if (m_segment > 0) {
        --m_segment;
        emit changed();
        return;
    }
    if (m_history.isEmpty()) return;
    const auto cursor = m_history.takeLast();
    chunk(cursor.line, cursor.offset);
    m_previousChunk = true;
}

void TextReader::closeBook() { list(m_offset); }

void TextReader::restart() {
    if (!reading() || loading()) return;
    m_error.clear();
    m_history.clear();
    chunk(1, 0);
}

void TextReader::request(const QString &path, RequestKind kind) {
    const quint64 generation = m_generation;
    auto *reply = m_net.get(BackendApi::localRequest(path));
    m_reply = reply;
    // JSON escaping can expand a 2MiB source chunk by six times.
    const int limit = kind == Chunk ? 13 * 1024 * 1024 : 256 * 1024;
    reply->setReadBufferSize(limit + 1);
    connect(reply, &QIODevice::readyRead, reply, [reply, limit] {
        if (reply->bytesAvailable() > limit) reply->abort();
    });
    emit changed();
    connect(reply, &QNetworkReply::finished, this, [this, reply, generation, kind, limit] {
        reply->deleteLater();
        if (generation != m_generation) return;
        const QByteArray bytes = reply->readAll();
        m_reply = nullptr;
        QJsonParseError parse;
        const auto document = QJsonDocument::fromJson(bytes, &parse);
        if (reply->error() != QNetworkReply::NoError) {
            m_error = "文档读取失败：" + reply->errorString();
        } else if (bytes.size() > limit || parse.error != QJsonParseError::NoError) {
            m_error = "文档响应无效或超出容量";
        } else if (kind == Catalog) {
            acceptCatalog(document, reply->rawHeader("X-Has-More") == "true");
        } else if (!document.isObject()) {
            m_error = "文档响应格式无效";
        } else if (kind == Progress) {
            const auto object = document.object();
            m_canSave = object.value("can_save").toBool();
            m_account = object.value("account_id").toInteger();
            m_version = object.value("file_version").toString();
            if ((!object.value("position").isNull() && !object.value("position").isObject()) ||
                (m_canSave && (m_account <= 0 || m_version.isEmpty()))) {
                m_error = "文档断点身份无效";
                emit changed();
                return;
            }
            const auto position = object.value("position").toObject();
            const int line = position.value("start_line").toInt(1);
            const qint64 offset = position.value("byte_offset").toInteger(0);
            if (line < 1 || line > 10000000 || offset < 0 || m_version.size() > 128) {
                m_error = "文档断点无效";
            } else {
                chunk(line, offset);
                return;
            }
        } else {
            acceptChunk(document.object());
        }
        emit changed();
    });
}

void TextReader::acceptCatalog(const QJsonDocument &document, bool more) {
    if (!document.isArray() || document.array().size() > 24) {
        m_error = "书架响应超出边界";
        return;
    }
    QVariantList books;
    for (const auto &value : document.array()) {
        const auto book = value.toObject();
        const auto name = book.value("name").toString();
        const auto path = book.value("rel_path").toString();
        if (!value.isObject() || name.isEmpty() || name.size() > 4096 ||
            path.isEmpty() || path.size() > 4096 || path.contains(QChar::Null)) {
            m_error = "书架字段无效";
            return;
        }
        books.append(book.toVariantMap());
    }
    m_books = books;
    m_more = more;
}

void TextReader::acceptChunk(const QJsonObject &object) {
    const auto lines = object.value("lines").toArray();
    if (!object.value("lines").isArray() || lines.size() > 80) {
        m_error = "文档行数超限";
        return;
    }
    QStringList textLines;
    qsizetype total = 0;
    for (const auto &line : lines) {
        if (!line.isString()) { m_error = "文档行格式无效"; return; }
        const QString value = line.toString();
        total += value.toUtf8().size();
        if (total > 2 * 1024 * 1024) { m_error = "文档内容超限"; return; }
        textLines.append(value);
    }
    const int startLine = object.value("start_line").toInt(-1);
    const qint64 startOffset = object.value("start_offset").toInteger(-1);
    const qint64 nextOffset = object.value("next_offset").toInteger(-1);
    const bool more = object.value("has_more").toBool();
    if (startLine < 1 || startLine > 10000000 || startOffset < 0 ||
        nextOffset < startOffset || (more && (lines.isEmpty() || nextOffset == startOffset))) {
        m_error = "文档游标无效";
        return;
    }
    m_line = startLine;
    m_startOffset = startOffset;
    m_nextOffset = nextOffset;
    m_lineCount = lines.size();
    m_chunkMore = more;
    m_text = textLines.join('\n');
    m_segments = {0};
    // Keep UTF-16 surrogate pairs together at layout boundaries.
    for (qsizetype start = 0; start < m_text.size();) {
        qsizetype end = qMin(start + 16384, m_text.size());
        if (end < m_text.size() && m_text.at(end - 1).isHighSurrogate() && m_text.at(end).isLowSurrogate()) --end;
        m_segments.append(end);
        start = end;
    }
    if (m_segments.size() == 1) m_segments.append(0);
    if (m_previousChunk) m_segment = m_segments.size() - 2;
    scheduleSave();
}

#include "native_resume.h"
#include "../backend_api.h"
#include <QCryptographicHash>
#include <QFileInfo>
#include <QJsonDocument>
#include <QJsonObject>
#include <QUrl>
#include <QEventLoop>
#include <cmath>

namespace {
QString digest(const QByteArray &value) { return QString::fromLatin1(QCryptographicHash::hash(value,QCryptographicHash::Sha256).toHex()); }
}
NativeResume::NativeResume(QObject *parent) : QObject(parent) {
    m_timer.setInterval(15000);
    connect(&m_timer,&QTimer::timeout,this,[this]{m_retryBlocked=false;save();pump();});
}
void NativeResume::setError(const QString &message) { if (m_error!=message) { m_error=message;emit errorChanged(); } }
void NativeResume::bind(QObject *view,bool allowRestore) {
    save();
    for (const auto &connection:m_connections) disconnect(connection);
    m_connections.clear(); m_player=view?view->findChild<MpvItem *>():nullptr;
    if (!m_player) { m_timer.stop();return; }
    m_connections << connect(m_player,&MpvItem::sourceChanging,this,[this]{save();m_changing=true;});
    m_connections << connect(m_player,&MpvItem::sourceChanged,this,[this]{load(true);});
    m_connections << connect(m_player,&MpvItem::pauseRequestedChanged,this,[this]{if(!m_applying&&!m_changing)m_userEdited=true;save();});
    auto edited=[this]{if(!m_applying&&!m_changing)m_userEdited=true;};
    m_connections << connect(m_player,&MpvItem::volumeChanged,this,edited);
    m_connections << connect(m_player,&MpvItem::playbackRateChanged,this,edited);
    m_connections << connect(m_player,&MpvItem::seekRequested,this,edited);
    m_connections << connect(m_player,&MpvItem::playingChanged,this,[this]{if(m_player&&m_player->isPlaying()&&!m_id.isEmpty())m_timer.start();else m_timer.stop();});
    m_connections << connect(m_player,&MpvItem::mediaCompleted,this,[this]{m_completed=true;save();});
    m_connections << connect(m_player,&MpvItem::stopping,this,[this]{if(!m_changing){m_completed=true;save();}});
    m_connections << connect(m_player,&MpvItem::seekCompleted,this,[this](double value){if(m_player&&value<m_player->duration()-0.1)m_completed=false;});
    m_connections << connect(m_player,&MpvItem::closing,this,[this]{save();m_player=nullptr;m_timer.stop();++m_generation;});
    load(allowRestore);
}
void NativeResume::load(bool restore) {
    ++m_generation;m_id.clear();m_version.clear();m_account=0;m_completed=false;m_changing=false;m_userEdited=false;m_last.clear();m_timer.stop();m_readNeeded=false;
    if (!m_player) return;
    const QUrl url(m_player->source());
    if (!url.scheme().isEmpty()&&!url.isLocalFile()) return;
    const QFileInfo file(url.isLocalFile()?url.toLocalFile():m_player->source());
    if (!file.isFile()) return;
    if(file.canonicalFilePath().isEmpty())return;
    m_id=digest(file.canonicalFilePath().toUtf8());
    m_version=digest(QByteArray::number(file.size())+":"+QByteArray::number(file.lastModified().toMSecsSinceEpoch())+":"+QByteArray::number(file.birthTime().toMSecsSinceEpoch()));
    m_restore=restore;m_readNeeded=true;setError({});pump();
}
void NativeResume::refresh() { m_retryBlocked=false;if(m_player&&m_account==0)load(false);else pump(); }
void NativeResume::save() {
    if (!m_player||m_id.isEmpty()||m_account<=0||m_readNeeded||m_applying||m_changing||!m_player->mediaLoaded())return;
    const double duration=m_player->duration();if(!std::isfinite(duration)||duration<=0||duration>31536000)return;
    const double position=m_completed?0:qBound(0.0,std::round(m_player->position()*10)/10,duration);
    QVariantMap state{{"id",m_id},{"version",m_version},{"account_id",m_account},{"position",position},{"duration",duration},{"paused",m_completed?false:m_player->pauseRequested()},{"volume",m_player->volume()},{"rate",m_player->playbackRate()}};
    if(state==m_last)return;m_last=state;
    for(auto &pending:m_saves)if(pending.value("id")==state.value("id")&&pending.value("version")==state.value("version")&&pending.value("account_id")==state.value("account_id")){pending=state;pump();return;}
    if(m_saves.size()>=16){setError("断点待保存队列已满，请检查服务连接");m_last.clear();return;}
    m_saves.append(state);pump();
}
void NativeResume::pump() {
    if(m_reply||m_retryBlocked)return;
    if(m_saves.isEmpty()&&!m_readNeeded){emit drained();return;}
    const bool writing=!m_saves.isEmpty();
    const QVariantMap saved=writing?m_saves.takeFirst():QVariantMap();
    const quint64 generation=m_generation;
    const QString id=m_id,version=m_version;
    if(!writing)m_readNeeded=false;
    auto request=BackendApi::localRequest(writing?"/api/native/progress":"/api/native/progress?id="+id+"&version="+version);
    request.setTransferTimeout(3000);
    auto *reply=writing?m_network.post(request,QJsonDocument::fromVariant(saved).toJson(QJsonDocument::Compact)):m_network.get(request);
    m_reply=reply;reply->setReadBufferSize(8193);
    connect(reply,&QIODevice::readyRead,reply,[reply]{if(reply->bytesAvailable()>8192)reply->abort();});
    connect(reply,&QNetworkReply::finished,this,[this,reply,writing,saved,generation,id,version]{
        m_reply=nullptr;
        QJsonParseError parse;const auto document=QJsonDocument::fromJson(reply->readAll(),&parse);
        const auto object=document.object();
        const qint64 account=object.value("account_id").toInteger();
        const bool good=reply->error()==QNetworkReply::NoError&&parse.error==QJsonParseError::NoError&&document.isObject()&&account>0;
        if(!good){
            setError("断点保存/恢复服务不可用："+reply->errorString());
            if(writing){
                const int status=reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
                if(status==409||status==400||status==403){if(m_last==saved)m_last.clear();}
                else {
                    bool superseded=false;
                    for(const auto &pending:m_saves)if(pending.value("id")==saved.value("id")&&pending.value("version")==saved.value("version")&&pending.value("account_id")==saved.value("account_id"))superseded=true;
                    if(!superseded){if(m_saves.size()<16)m_saves.prepend(saved);else{m_last.clear();setError("断点保存失败且队列已满");}}
                    m_retryBlocked=true;
                }
            }
        }
        else if(writing){if(account!=saved.value("account_id").toLongLong())setError("断点账号已变化，请重新打开媒体");else setError({});}
        else if(generation==m_generation&&id==m_id&&version==m_version&&m_player){
            m_account=account;
            const auto data=object.value("position").toObject();
            if(!data.isEmpty()&&m_restore&&!m_userEdited&&m_player->position()<2){
                const double position=data.value("position").toDouble(-1),duration=data.value("duration").toDouble(-1),volume=data.value("volume").toDouble(-1),rate=data.value("rate").toDouble(-1);
                if(data.value("id").toString()==id&&data.value("version").toString()==version&&std::isfinite(position)&&position>=0&&position<=duration&&duration>0&&duration<=31536000&&volume>=0&&volume<=100&&rate>=0.25&&rate<=3&&data.value("paused").isBool()){
                    m_applying=true;m_player->setVolume(volume);m_player->setPlaybackRate(rate);m_player->setPosition(position);if(data.value("paused").toBool())m_player->pause();m_applying=false;
                }else setError("原生断点字段无效，已忽略");
            }
            if(m_player->isPlaying())m_timer.start();
        }
        reply->deleteLater();pump();
    });
}
void NativeResume::flush() {
    m_retryBlocked=false;save();pump();if(!m_reply&&m_saves.isEmpty())return;
    QEventLoop loop;QTimer deadline;deadline.setSingleShot(true);deadline.setInterval(1500);
    connect(this,&NativeResume::drained,&loop,&QEventLoop::quit);connect(&deadline,&QTimer::timeout,&loop,&QEventLoop::quit);
    deadline.start();loop.exec();
    if(m_reply||!m_saves.isEmpty()){setError("退出等待断点保存超时");qWarning("Native resume flush timed out");}
}

#include "external_router.h"
#include "../backend_api.h"
#include "../player/media_address.h"
#include <QCryptographicHash>
#include <QDir>
#include <QFileInfo>
#include <QLocalSocket>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJsonArray>
#include <QTimer>

namespace { constexpr int packetLimit = 256 * 1024; }
ExternalRouter::ExternalRouter(QObject *parent) : QObject(parent) {
    m_server.setSocketOptions(QLocalServer::UserAccessOption);
    m_server.setMaxPendingConnections(16);
    connect(&m_server,&QLocalServer::newConnection,this,[this]{
        while(m_server.hasPendingConnections()){
            auto *socket=m_server.nextPendingConnection();
            if(m_connections>=16){socket->abort();socket->deleteLater();continue;}
            ++m_connections;connect(socket,&QObject::destroyed,this,[this]{--m_connections;});
            socket->setReadBufferSize(packetLimit+1);
            auto *timer=new QTimer(socket);timer->setSingleShot(true);timer->start(3000);
            connect(timer,&QTimer::timeout,socket,&QLocalSocket::abort);
            connect(socket,&QLocalSocket::disconnected,socket,&QObject::deleteLater);
            connect(socket,&QLocalSocket::readyRead,this,[this,socket,timer]{
                if(socket->bytesAvailable()>packetLimit){socket->abort();return;}
                if(!socket->canReadLine())return;
                const auto data=socket->readLine(packetLimit+1);
                if(!data.endsWith('\n')||socket->bytesAvailable()>0){socket->abort();return;}
                QJsonParseError parse;const auto document=QJsonDocument::fromJson(data,&parse);
                const auto object=document.object();const auto array=object.value("files").toArray();
                if(parse.error!=QJsonParseError::NoError||!document.isObject()||object.size()!=1||!object.value("files").isArray()||array.isEmpty()||array.size()>32){socket->abort();return;}
                QStringList input,files;for(const auto &value:array){if(!value.isString()){socket->abort();return;}input.append(value.toString());}
                if(!localFiles(input,files)){socket->abort();return;}
                timer->stop();socket->disconnect(this);
                emit filesRequested(files);
                if(socket->write("OK\n")!=3){socket->abort();return;}
                socket->flush();socket->disconnectFromServer();
            });
        }
    });
}
ExternalRouter::~ExternalRouter(){
    // Child sockets are destroyed with the server member, before QObject's
    // base destructor disconnects receivers; do not let them access retired fields.
    for(auto *socket:m_server.findChildren<QLocalSocket *>())socket->disconnect(this);
    m_server.disconnect(this);
    if(m_owner)m_server.close();
}
bool ExternalRouter::localFiles(const QStringList &input,QStringList &output){
    if(input.isEmpty()||input.size()>32)return false;
    QStringList files;qsizetype bytes=0;
    for(const auto &source:input){
        const auto address=mediaAddress(source);if(!address)return false;
        const QUrl url(source);if(!url.scheme().isEmpty()&&!url.isLocalFile()&&!QFileInfo(source).isAbsolute())return false;
        const QFileInfo info(*address);if(!info.isFile())return false;
        const QString path=info.absoluteFilePath();bytes+=path.toUtf8().size();if(bytes>128*1024)return false;files.append(path);
    }
    output=std::move(files);return true;
}
int ExternalRouter::start(const QStringList &input){
    QStringList files;if(!localFiles(input,files)){m_error="外部打开要求 1–32 个有效本地文件，地址共不超过 128KiB";return -1;}
    const QString config=QFileInfo(BackendApi::configurationDirectory()).absoluteFilePath();
    if(!QDir().mkpath(config)){m_error="无法准备外部播放器配置目录";return -1;}
#ifdef Q_OS_WIN
    m_name="omnihub-external-"+QString::fromLatin1(QCryptographicHash::hash(config.toUtf8(),QCryptographicHash::Sha256).toHex().left(32));
#else
    m_name=QDir(config).filePath("external-player.socket");
#endif
    m_lock=std::make_unique<QLockFile>(QDir(config).filePath("external-player.lock"));m_lock->setStaleLockTime(0);
    if(m_lock->tryLock()){
        const QFileInfo existing(m_name);
        if(existing.exists()&&(existing.isFile()||existing.isDir()||existing.isSymLink())){m_error="外部播放器端点被非套接字文件占用";return -1;}
        if(!m_server.listen(m_name)){
            // Only the lock owner can remove this private stale runtime endpoint.
            QLocalSocket probe;probe.connectToServer(m_name);
            if(probe.waitForConnected(150)||!QLocalServer::removeServer(m_name)||!m_server.listen(m_name)){m_error="外部播放器端点不可用："+m_server.errorString();return -1;}
        }
        m_owner=true;return 1;
    }
    QLocalSocket socket;
    for(int attempt=0;attempt<15;++attempt){socket.connectToServer(m_name);if(socket.waitForConnected(150))break;socket.abort();}
    if(socket.state()!=QLocalSocket::ConnectedState){m_error="已有外部播放器尚未就绪，请重试";return -1;}
    QJsonArray array;for(const auto &file:files)array.append(file);
    const QByteArray data=QJsonDocument(QJsonObject{{"files",array}}).toJson(QJsonDocument::Compact)+'\n';
    if(data.size()>packetLimit||socket.write(data)!=data.size()||(socket.bytesToWrite()>0&&!socket.waitForBytesWritten(1500))||(socket.bytesAvailable()==0&&!socket.waitForReadyRead(3000))||socket.readLine(16)!="OK\n"){m_error="外部播放器未确认接收文件";return -1;}
    return 0;
}

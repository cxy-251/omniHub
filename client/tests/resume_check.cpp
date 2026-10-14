#include "mpv_item.h"
#include "native_resume.h"
#include <QGuiApplication>
#include <QQuickWindow>
#include <QTimer>
#include <QEvent>
#include <QSGRendererInterface>
#include <cmath>

// Two separate invocations prove real service persistence, not an in-process snapshot.
int main(int argc,char **argv){
 if(argc!=3)return 11;
 QQuickWindow::setGraphicsApi(QSGRendererInterface::OpenGL);
 QGuiApplication app(argc,argv);app.setQuitOnLastWindowClosed(false);
 bool passed=false;int result;
 {
  NativeResume resume;
  QQuickWindow window;window.resize(320,240);
  auto *player=new MpvItem(window.contentItem());player->setWidth(320);player->setHeight(240);
  player->setSource(QString::fromLocal8Bit(argv[1]));resume.bind(window.contentItem());window.show();
  const bool saving=QString::fromLocal8Bit(argv[2])=="save";
  QTimer::singleShot(1500,&app,[&]{
   if(!player->mediaLoaded()||!resume.error().isEmpty()){fprintf(stderr,"RESUME_INIT_FAILED %s\n",resume.error().toUtf8().constData());app.exit(12);return;}
   if(saving){player->pause();player->setVolume(42);player->setPlaybackRate(1.5);player->setPosition(4.25);}
   QTimer::singleShot(600,&app,[&]{
    if(!player->pauseRequested()||std::abs(player->position()-4.25)>0.15||player->volume()!=42||player->playbackRate()!=1.5){fprintf(stderr,"RESUME_STATE_FAILED pos=%f vol=%f rate=%f\n",player->position(),player->volume(),player->playbackRate());app.exit(13);return;}
    resume.flush();if(!resume.error().isEmpty()){fprintf(stderr,"RESUME_SAVE_FAILED %s\n",resume.error().toUtf8().constData());app.exit(14);return;}
    passed=true;fprintf(stderr,saving?"NATIVE_RESUME_SAVED\n":"NATIVE_RESUME_REOPEN_RESTORED\n");app.quit();
   });
  });
  QTimer::singleShot(7000,&app,[&]{app.exit(15);});result=app.exec();
 }
 QCoreApplication::sendPostedEvents(nullptr,QEvent::DeferredDelete);
 return result==0&&!passed?16:result;
}

#include "readers/text_reader.h"
#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQuickWindow>
#include <QQuickItem>
#include <QTimer>
#include <cstdio>

// Exercises actual HTTP pagination, bounded text layout, Unicode and persisted
// byte cursors. The Go fixture runs this again in a fresh process to restore.
int main(int argc,char **argv) {
    QGuiApplication app(argc,argv);
    app.setQuitOnLastWindowClosed(false);
    if(argc!=2)return 10;
    qmlRegisterType<TextReader>("omniHub.Readers",1,0,"TextReader");
    QQuickWindow window;window.resize(800,600);
    QQmlApplicationEngine engine;engine.load(QUrl("qrc:/qml/TextLibraryView.qml"));
    if(engine.rootObjects().isEmpty())return 20;
    auto *root=qobject_cast<QQuickItem *>(engine.rootObjects().first());
    if(!root)return 21;
    root->setParentItem(window.contentItem());root->setWidth(800);root->setHeight(600);window.show();
    auto *controller=root->findChild<TextReader *>("textReader");
    if(!controller)return 22;
    auto &reader=*controller;
    const bool restoring=QString::fromLocal8Bit(argv[1])=="restore";
    int stage=0;
    bool passed=false,advancing=false;
    QString collected;
    QObject::connect(&reader,&TextReader::changed,&app,[&]{
        if(advancing||reader.loading())return;
        if(!reader.error().isEmpty()){
            fprintf(stderr,"TEXT_FAILED %s\n",reader.error().toUtf8().constData());app.exit(11);return;
        }
        advancing=true;
        if(stage==0) {
            if(reader.books().size()!=24||!reader.hasMore()){app.exit(12);return;}
            stage=1;reader.list(24);
        } else if(stage==1) {
            if(reader.books().size()!=2||reader.hasMore()){app.exit(13);return;}
            stage=2;reader.list();
        } else if(stage==2) {
            stage=3;reader.open(reader.books().first().toMap().value("rel_path").toString());
        } else if(stage==3&&reader.reading()&&!reader.content().isEmpty()) {
            if(restoring) {
                if(reader.startLine()!=81||reader.content()!="line80\nline81"){app.exit(14);return;}
                passed=true;fprintf(stderr,"TEXT_CURSOR_RESTORED\n");app.quit();
            } else if(reader.startLine()==1) {
                const auto page=reader.content();
                if(page.size()>16384||(!page.isEmpty()&&(page.front().isLowSurrogate()||page.back().isHighSurrogate()))){app.exit(15);return;}
                collected+=page;
                QTimer::singleShot(0,&reader,&TextReader::next);
            } else {
                QString expected="\n\n"+QString(16381,'a')+QString::fromUcs4(U"😀")+"end";
                for(int i=3;i<80;++i)expected+="\nline"+QString::number(i);
                if(reader.startLine()!=81||collected!=expected){fprintf(stderr,"TEXT_MISMATCH size=%lld expected=%lld line=%d\n",(long long)collected.size(),(long long)expected.size(),reader.startLine());app.exit(16);return;}
                stage=4;
                QTimer::singleShot(600,&app,[&]{
                    auto *content=root->findChild<QQuickItem *>("textContent");
                    if(!content||content->property("text").toString()!=reader.content()||content->width()<500||content->width()>800||window.grabWindow().isNull()){app.exit(23);return;}
                    reader.flush();
                    if(!reader.error().isEmpty()){app.exit(17);return;}
                    passed=true;fprintf(stderr,"TEXT_PAGES_UNICODE_AND_SAVE_OK\n");app.quit();
                });
            }
        }
        advancing=false;
    });
    QTimer::singleShot(8000,&app,[&]{app.exit(18);});
    const int result=app.exec();
    return result==0&&!passed?19:result;
}

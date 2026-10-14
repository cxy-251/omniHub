// Opt-in check against a caller-owned isolated Go server and real child fixture.
#include <QCoreApplication>
#include <QTimer>
#include <QFileInfo>
#include <QDebug>
#include <cstdio>
#include "game_launcher.h"

int main(int argc, char **argv) {
    QCoreApplication app(argc, argv);
    if (argc != 3) return 11;
    const QString libraryId = QString::fromUtf8(argv[1]);
    const QString observed = QString::fromUtf8(argv[2]);
    GameLauncher launcher;
    int phase = 0;
    auto fail = [&](const QString &reason) { qCritical() << reason; app.exit(2); };
    QObject::connect(&launcher, &GameLauncher::errorChanged, &app, [&]() {
        if (!launcher.error().isEmpty()) fail(launcher.error());
    });
    QObject::connect(&launcher, &GameLauncher::loadingChanged, &app, [&]() {
        if (launcher.loading()) return;
        if (phase == 0) {
            if (launcher.games().size() != 24 || !launcher.hasMore() || launcher.offset() != 0) { fail("first page invalid"); return; }
            phase = 1; launcher.fetchGames(24);
        } else if (phase == 1) {
            if (launcher.games().size() != 2 || launcher.hasMore() || launcher.offset() != 24) { fail("second page invalid"); return; }
            phase = 2; launcher.fetchGames(0);
        } else if (phase == 2) {
            const auto first = launcher.games().first().toMap();
            if (first.value("library_id").toString() != libraryId || first.value("id").toString() != "041") { fail("wrong library/game"); return; }
            phase = 3; launcher.launchGame(libraryId, "041");
        }
    });
    QTimer ready;
    ready.setInterval(50);
    QObject::connect(&ready, &QTimer::timeout, &app, [&]() {
        if (QFileInfo::exists(observed)) { ready.stop(); phase = 4; launcher.stopGame(); }
    });
    QObject::connect(&launcher, &GameLauncher::launchFinished, &app, [&](const QString &, bool success) {
        if (!success || !launcher.gameRunning() || launcher.session().value("key").toString() != libraryId + ":041") { fail("launch/session invalid"); return; }
        ready.start();
    });
    QObject::connect(&launcher, &GameLauncher::sessionChanged, &app, [&]() {
        if (phase == 4 && !launcher.gameRunning() && launcher.session().value("state").toString() == "exited") {
            std::puts("GAME_CHECK_OK: real pages, indexed launch, child readiness, session stop and exit");
            app.exit(0);
        }
    });
    QTimer::singleShot(30000, &app, [&]() { fail("game integration timeout"); });
    QTimer::singleShot(0, &launcher, [&]() { launcher.fetchGames(); });
    return app.exec();
}

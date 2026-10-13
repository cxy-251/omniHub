cmake_minimum_required(VERSION 3.22)
project(omnihub_client VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)
set(CMAKE_AUTOUIC ON)

find_package(Qt6 6.4 REQUIRED COMPONENTS Core Gui Quick Qml DBus OpenGL)
find_package(PkgConfig REQUIRED)
pkg_check_modules(MPV REQUIRED mpv)

include_directories(
    ${CMAKE_CURRENT_SOURCE_DIR}/src
    ${MPV_INCLUDE_DIRS}
)

set(SOURCES
    src/main.cpp
    src/player/mpv_item.cpp
    src/player/mpv_item.h
    src/player/mpris.cpp
    src/player/mpris.h
    src/games/game_launcher.cpp
    src/games/game_launcher.h
)

set(QML_RESOURCES
    qml.qrc
)

qt_add_executable(omnihub_client
    ${SOURCES}
    ${QML_RESOURCES}
)

target_link_libraries(omnihub_client PRIVATE
    Qt6::Core
    Qt6::Gui
    Qt6::Quick
    Qt6::Qml
    Qt6::DBus
    Qt6::OpenGL
    ${MPV_LIBRARIES}
)

install(TARGETS omnihub_client DESTINATION bin)

cmake_minimum_required(VERSION 3.22)
project(omnihub_client VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)
set(CMAKE_AUTOUIC ON)

find_package(Qt6 6.6 REQUIRED COMPONENTS Core Gui Quick Qml Network OpenGL)
if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
    find_package(Qt6 6.6 REQUIRED COMPONENTS DBus)
endif()
find_package(PkgConfig REQUIRED)
pkg_check_modules(MPV REQUIRED mpv)

include_directories(
    ${CMAKE_CURRENT_SOURCE_DIR}/src
    ${MPV_INCLUDE_DIRS}
)

set(SOURCES
    src/main.cpp
    src/backend_api.cpp
    src/backend_api.h
    src/player/mpv_item.cpp
    src/player/mpv_item.h
    src/games/game_launcher.cpp
    src/games/game_launcher.h
)

if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
    list(APPEND SOURCES src/player/mpris.cpp src/player/mpris.h)
endif()

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
    Qt6::Network
    Qt6::OpenGL
    ${MPV_LIBRARIES}
)
if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
    target_link_libraries(omnihub_client PRIVATE Qt6::DBus)
endif()

install(TARGETS omnihub_client DESTINATION bin)

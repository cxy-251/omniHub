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

find_package(PkgConfig QUIET)
if(PkgConfig_FOUND)
    pkg_check_modules(MPV QUIET IMPORTED_TARGET mpv)
endif()
if(TARGET PkgConfig::MPV)
    set(MPV_TARGET PkgConfig::MPV)
else()
    # Portable SDKs and Windows packages need not ship pkg-config. Respect
    # CMAKE_PREFIX_PATH and keep the actual library path on the imported target.
    find_path(MPV_INCLUDE_DIR NAMES mpv/client.h REQUIRED)
    find_library(MPV_LIBRARY NAMES mpv libmpv REQUIRED)
    add_library(Mpv::Mpv UNKNOWN IMPORTED)
    set_target_properties(Mpv::Mpv PROPERTIES
        IMPORTED_LOCATION "${MPV_LIBRARY}"
        INTERFACE_INCLUDE_DIRECTORIES "${MPV_INCLUDE_DIR}"
    )
    set(MPV_TARGET Mpv::Mpv)
endif()

set(SOURCES
    src/main.cpp
    src/readers/text_reader.cpp
    src/readers/text_reader.h
    src/host/external_router.cpp
    src/host/external_router.h
    src/backend_api.cpp
    src/backend_api.h
    src/local_network.cpp
    src/local_network.h
    src/native_lifecycle.cpp
    src/native_lifecycle.h
    src/player/mpv_item.cpp
    src/player/mpv_item.h
    src/player/media_queue.cpp
    src/player/media_queue.h
    src/player/media_address.h
    src/player/native_resume.cpp
    src/player/native_resume.h
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
target_include_directories(omnihub_client PRIVATE src)

target_link_libraries(omnihub_client PRIVATE
    Qt6::Core
    Qt6::Gui
    Qt6::Quick
    Qt6::Qml
    Qt6::Network
    Qt6::OpenGL
    ${MPV_TARGET}
)
if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
    target_link_libraries(omnihub_client PRIVATE Qt6::DBus)
endif()

install(TARGETS omnihub_client DESTINATION bin)
if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
    install(PROGRAMS desktop/omnihub-client DESTINATION bin)
    install(FILES desktop/omnihub-player.desktop DESTINATION share/applications)
endif()

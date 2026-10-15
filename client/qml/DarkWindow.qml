import QtQuick
import QtQuick.Window

// DarkWindow：统一的深色窗口（主窗口与外部播放窗口共用）。Fusion 控件按调色板着色。
Window {
    color: "#0d1117"
    // 深色调色板（Fusion 控件按调色板着色）。
    palette.window: "#0d1117"
    palette.windowText: "#c9d1d9"
    palette.base: "#161b22"
    palette.alternateBase: "#0d1117"
    palette.text: "#c9d1d9"
    palette.button: "#21262d"
    palette.buttonText: "#c9d1d9"
    palette.highlight: "#58a6ff"
    palette.highlightedText: "#0d1117"
    palette.placeholderText: "#8b949e"
    palette.mid: "#30363d"
    palette.light: "#30363d"
    palette.dark: "#0b0e13"
    palette.toolTipBase: "#161b22"
    palette.toolTipText: "#c9d1d9"
}

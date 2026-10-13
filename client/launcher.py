#!/usr/bin/env python3
import sys
import os

try:
    from PyQt6.QtWidgets import QApplication, QMainWindow, QWidget, QVBoxLayout, QPushButton, QLabel
    from PyQt6.QtCore import Qt, QUrl
except ImportError:
    print("PyQt6 not available, run with C++ client or install PyQt6")
    sys.exit(0)

class MainWindow(QMainWindow):
    def __init__(self):
        super().__init__()
        self.setWindowTitle("omniHub Native Client (Deck)")
        self.resize(1280, 800)
        self.setStyleSheet("background-color: #0d1117; color: #e6edf3;")

        central = QWidget()
        self.setCentralWidget(central)
        layout = QVBoxLayout(central)

        title = QLabel("omniHub Native Qt 6 Client")
        title.setAlignment(Qt.AlignmentFlag.AlignCenter)
        title.setStyleSheet("font-size: 20px; font-weight: bold; margin: 20px;")
        layout.addWidget(title)

        status = QLabel("纯原生 Qt 渲染 · 零 Chromium 依赖 · 独立游戏与 libmpv 播放器通道")
        status.setAlignment(Qt.AlignmentFlag.AlignCenter)
        status.setStyleSheet("color: #8b949e; font-size: 14px;")
        layout.addWidget(status)

if __name__ == '__main__':
    app = QApplication(sys.argv)
    win = MainWindow()
    if "--test" in sys.argv:
        print("Qt client test launch OK")
        sys.exit(0)
    win.show()
    sys.exit(app.exec())

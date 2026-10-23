@echo off
rem omniHub 便携版入口：启动服务并打开桌面客户端（服务没有控制台窗口）。
start "" "%~dp0omnihub-server.exe" --desktop

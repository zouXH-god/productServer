@echo off
setlocal
where go >nul 2>nul || (echo [error] Go is not installed or not in PATH & exit /b 1)
where npm >nul 2>nul || (echo [error] Node.js/npm is not installed or not in PATH & exit /b 1)
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\dev.ps1"
exit /b %errorlevel%

@echo off
echo Building Pterodactyl Console App...
wails build -s
if %errorlevel% neq 0 (
    echo Build failed!
    pause
    exit /b %errorlevel%
)
echo.
echo Starting app...
start build\bin\pteroclient-wails.exe

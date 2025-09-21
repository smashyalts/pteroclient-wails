@echo off
setlocal EnableDelayedExpansion

:: PteroClient Wails Build Script
:: ================================

set APP_NAME=pteroclient-wails
set VERSION=1.0.0

:: Color codes for output
set RED=[31m
set GREEN=[32m
set YELLOW=[33m
set BLUE=[34m
set RESET=[0m

echo.
echo %BLUE%===================================%RESET%
echo %BLUE%   PteroClient Build Script%RESET%
echo %BLUE%===================================%RESET%
echo.

:: Check for prerequisites
call :check_prereqs
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo %RED%Prerequisites check failed. Please install missing dependencies.%RESET%
    echo.
    pause
    goto end
)

if "%1"=="" goto menu
goto %1

:menu
echo Select build option:
echo.
echo   %GREEN%1%RESET% - Development mode (hot reload)
echo   %GREEN%2%RESET% - Build for Windows (current platform)
echo   %GREEN%3%RESET% - Build for Windows (portable)
echo   %GREEN%4%RESET% - Build for Windows only (all architectures)
echo   %GREEN%5%RESET% - Build Windows installer (NSIS)
echo   %GREEN%6%RESET% - Clean build artifacts
echo   %GREEN%7%RESET% - Update/Install dependencies
echo   %GREEN%8%RESET% - Run tests
echo   %GREEN%9%RESET% - Check/Install prerequisites
echo   %GREEN%0%RESET% - Exit
echo.
set /p choice="Enter your choice (0-9): "

if "%choice%"=="1" goto dev
if "%choice%"=="2" goto build_windows
if "%choice%"=="3" goto build_portable
if "%choice%"=="4" goto build_windows_all
if "%choice%"=="5" goto build_installer
if "%choice%"=="6" goto clean
if "%choice%"=="7" goto update_deps
if "%choice%"=="8" goto test
if "%choice%"=="9" goto install_prereqs
if "%choice%"=="0" goto end

echo %RED%Invalid choice. Please try again.%RESET%
echo.
goto menu

:dev
echo.
echo %YELLOW%Starting development server...%RESET%
wails dev
goto end

:build_windows
echo.
echo %YELLOW%Building for Windows (amd64)...%RESET%
wails build -platform windows/amd64 -clean -upx -webview2 embed
if %ERRORLEVEL% NEQ 0 (
    echo %RED%Build failed!%RESET%
    goto end
)
echo %GREEN%Build complete! Output: build\bin\%APP_NAME%.exe%RESET%
goto end

:build_portable
echo.
echo %YELLOW%Building portable Windows executable...%RESET%
wails build -platform windows/amd64 -clean -upx -webview2 embed -ldflags "-H windowsgui -s -w"
if %ERRORLEVEL% NEQ 0 (
    echo %RED%Build failed!%RESET%
    goto end
)
echo %GREEN%Portable build complete! Output: build\bin\%APP_NAME%.exe%RESET%
goto end

:build_windows_all
echo.
echo %YELLOW%Building for Windows platforms only...%RESET%
echo.
echo %BLUE%Note: Cross-compilation to Linux/macOS from Windows requires additional setup.%RESET%
echo %BLUE%Visit https://wails.io/docs/guides/crosscompiling for more info.%RESET%
echo.

:: Ensure frontend is built first
if not exist frontend\dist (
    echo %YELLOW%Building frontend first...%RESET%
    call :build_frontend
    if %ERRORLEVEL% NEQ 0 (
        echo %RED%Frontend build failed!%RESET%
        goto end
    )
)

:: Windows amd64
echo Building Windows (amd64)...
wails build -platform windows/amd64 -clean -upx -webview2 embed -o %APP_NAME%-windows-amd64.exe
if %ERRORLEVEL% NEQ 0 (
    echo %RED%Windows amd64 build failed!%RESET%
) else (
    echo %GREEN%✓ Windows amd64%RESET%
)

:: Windows arm64
echo Building Windows (arm64)...
wails build -platform windows/arm64 -clean -webview2 embed -o %APP_NAME%-windows-arm64.exe
if %ERRORLEVEL% NEQ 0 (
    echo %RED%Windows arm64 build failed!%RESET%
) else (
    echo %GREEN%✓ Windows arm64%RESET%
)

echo.
echo %GREEN%Windows builds complete! Check build\bin\ for outputs.%RESET%
goto end

:build_all
echo.
echo %YELLOW%Cross-platform builds from Windows...%RESET%
echo.
echo %RED%WARNING: Cross-compilation to Linux/macOS from Windows is not fully supported.%RESET%
echo %YELLOW%For best results, build on the target platform or use CI/CD.%RESET%
echo.
echo Continue anyway? (y/n)
set /p continue=""
if /i not "%continue%"=="y" goto menu

call :build_windows_all
goto end

:build_installer
echo.
echo %YELLOW%Building Windows installer with NSIS...%RESET%
wails build -platform windows/amd64 -nsis -clean -upx -webview2 embed
if %ERRORLEVEL% NEQ 0 (
    echo %RED%Installer build failed!%RESET%
    goto end
)
echo %GREEN%Installer build complete! Output: build\bin\%APP_NAME%-amd64-installer.exe%RESET%
goto end

:clean
echo.
echo %YELLOW%Cleaning build artifacts...%RESET%
if exist build (
    rmdir /s /q build
    echo Removed build directory
)
if exist node_modules (
    cd frontend
    if exist node_modules (
        rmdir /s /q node_modules
        echo Removed frontend\node_modules
    )
    cd ..
)
echo %GREEN%Clean complete!%RESET%
goto end

:update_deps
echo.
echo %YELLOW%Updating dependencies...%RESET%
echo.
echo Updating Go modules...
go mod download
go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo %RED%Go module update failed!%RESET%
    goto end
)
echo %GREEN%✓ Go modules updated%RESET%
echo.
echo Updating frontend dependencies...
cd frontend
call npm install
if %ERRORLEVEL% NEQ 0 (
    echo %RED%NPM install failed!%RESET%
    cd ..
    goto end
)
cd ..
echo %GREEN%✓ Frontend dependencies updated%RESET%
echo.
echo %GREEN%All dependencies updated!%RESET%
goto end

:test
echo.
echo %YELLOW%Running tests...%RESET%
echo.
echo Running Go tests...
go test ./... -v
if %ERRORLEVEL% NEQ 0 (
    echo %RED%Some tests failed!%RESET%
) else (
    echo %GREEN%✓ All Go tests passed%RESET%
)
echo.
echo Running frontend tests...
cd frontend
call npm test
if %ERRORLEVEL% NEQ 0 (
    echo %YELLOW%Frontend tests not configured or failed%RESET%
) else (
    echo %GREEN%✓ Frontend tests passed%RESET%
)
cd ..
goto end

:: Quick build shortcuts that can be called directly
:quick
wails build -clean -upx
goto end

:prod
wails build -platform windows/amd64 -clean -upx -webview2 embed -ldflags "-H windowsgui -s -w"
goto end

:debug
wails build -platform windows/amd64 -clean -debug
goto end

:check_prereqs
echo %YELLOW%Checking prerequisites...%RESET%
echo.

:: Check for Go
where go >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo %RED%✗ Go is not installed or not in PATH%RESET%
    echo   Install from: https://go.dev/dl/
    set PREREQ_FAIL=1
) else (
    for /f "tokens=3" %%i in ('go version') do set GO_VERSION=%%i
    echo %GREEN%✓ Go !GO_VERSION! found%RESET%
)

:: Check for Wails
where wails >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo %RED%✗ Wails CLI is not installed%RESET%
    echo   Install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest
    set PREREQ_FAIL=1
) else (
    for /f "tokens=3" %%i in ('wails version') do set WAILS_VERSION=%%i
    echo %GREEN%✓ Wails CLI !WAILS_VERSION! found%RESET%
)

:: Check for Node.js
where node >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo %RED%✗ Node.js is not installed or not in PATH%RESET%
    echo   Install from: https://nodejs.org/
    set PREREQ_FAIL=1
) else (
    for /f %%i in ('node -v') do set NODE_VERSION=%%i
    echo %GREEN%✓ Node.js !NODE_VERSION! found%RESET%
)

:: Check for npm
where npm >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo %RED%✗ npm is not installed or not in PATH%RESET%
    echo   npm should come with Node.js installation
    set PREREQ_FAIL=1
) else (
    for /f %%i in ('npm -v') do set NPM_VERSION=%%i
    echo %GREEN%✓ npm !NPM_VERSION! found%RESET%
)

:: Check for UPX (optional)
where upx >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo %YELLOW%⚠ UPX not found (optional - for compression)%RESET%
    echo   Install from: https://github.com/upx/upx/releases
) else (
    echo %GREEN%✓ UPX found%RESET%
)

if defined PREREQ_FAIL (
    exit /b 1
)
exit /b 0

:install_prereqs
echo.
echo %YELLOW%Installing prerequisites...%RESET%
echo.
echo This will help you install missing dependencies.
echo.

:: Check and install Node.js
where node >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo %YELLOW%Node.js is not installed.%RESET%
    echo.
    echo Would you like to download Node.js? (y/n)
    set /p install_node=""
    if /i "%install_node%"=="y" (
        echo Opening Node.js download page...
        start https://nodejs.org/en/download/
        echo Please install Node.js and then re-run this script.
    )
)

:: Check and install Wails
where wails >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo %YELLOW%Wails CLI is not installed.%RESET%
    echo.
    where go >nul 2>&1
    if %ERRORLEVEL% EQU 0 (
        echo Install Wails CLI? (y/n)
        set /p install_wails=""
        if /i "%install_wails%"=="y" (
            echo Installing Wails CLI...
            go install github.com/wailsapp/wails/v2/cmd/wails@latest
            if %ERRORLEVEL% EQU 0 (
                echo %GREEN%✓ Wails CLI installed successfully%RESET%
            ) else (
                echo %RED%Failed to install Wails CLI%RESET%
            )
        )
    ) else (
        echo %RED%Go is required to install Wails. Please install Go first.%RESET%
        echo Download from: https://go.dev/dl/
    )
)

:: Install frontend dependencies if needed
if exist frontend (
    if not exist frontend\node_modules (
        echo.
        echo %YELLOW%Frontend dependencies not installed.%RESET%
        echo Install frontend dependencies? (y/n)
        set /p install_deps=""
        if /i "%install_deps%"=="y" (
            cd frontend
            echo Installing frontend dependencies...
            call npm install
            cd ..
            if %ERRORLEVEL% EQU 0 (
                echo %GREEN%✓ Frontend dependencies installed%RESET%
            ) else (
                echo %RED%Failed to install frontend dependencies%RESET%
            )
        )
    )
)

echo.
echo %GREEN%Prerequisites check complete!%RESET%
pause
goto menu

:build_frontend
echo Building frontend...
cd frontend
call npm install
if %ERRORLEVEL% NEQ 0 (
    cd ..
    exit /b 1
)
call npm run build
if %ERRORLEVEL% NEQ 0 (
    cd ..
    exit /b 1
)
cd ..
exit /b 0

:end
echo.
endlocal

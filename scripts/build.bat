@echo off
REM build.bat — Compila todas las Lambdas Go para ARM64 en Windows
REM
REM Uso: scripts\build.bat
REM Prerequisito: Go instalado en PATH

setlocal enabledelayedexpansion

set REPO_ROOT=%~dp0..
set BIN_DIR=%REPO_ROOT%\bin

echo Compilando ws-connect...
set GOOS=linux
set GOARCH=arm64
set CGO_ENABLED=0
mkdir "%BIN_DIR%\ws-connect" 2>nul
go build -tags lambda.norpc -ldflags="-s -w" -o "%BIN_DIR%\ws-connect\bootstrap" "%REPO_ROOT%\lambdas\ws-connect\handler.go"
if %errorlevel% neq 0 (
    echo ERROR compilando ws-connect
    exit /b 1
)
echo   OK: bin\ws-connect\bootstrap

echo Compilando ws-disconnect...
mkdir "%BIN_DIR%\ws-disconnect" 2>nul
go build -tags lambda.norpc -ldflags="-s -w" -o "%BIN_DIR%\ws-disconnect\bootstrap" "%REPO_ROOT%\lambdas\ws-disconnect\handler.go"
if %errorlevel% neq 0 (
    echo ERROR compilando ws-disconnect
    exit /b 1
)
echo   OK: bin\ws-disconnect\bootstrap

echo Compilando ws-message...
mkdir "%BIN_DIR%\ws-message" 2>nul
go build -tags lambda.norpc -ldflags="-s -w" -o "%BIN_DIR%\ws-message\bootstrap" "%REPO_ROOT%\lambdas\ws-message\handler.go"
if %errorlevel% neq 0 (
    echo ERROR compilando ws-message
    exit /b 1
)
echo   OK: bin\ws-message\bootstrap

echo Compilando room-manager...
mkdir "%BIN_DIR%\room-manager" 2>nul
go build -tags lambda.norpc -ldflags="-s -w" -o "%BIN_DIR%\room-manager\bootstrap" "%REPO_ROOT%\lambdas\room-manager\handler.go"
if %errorlevel% neq 0 (
    echo ERROR compilando room-manager
    exit /b 1
)
echo   OK: bin\room-manager\bootstrap

echo Compilando broadcaster...
mkdir "%BIN_DIR%\broadcaster" 2>nul
go build -tags lambda.norpc -ldflags="-s -w" -o "%BIN_DIR%\broadcaster\bootstrap" "%REPO_ROOT%\lambdas\broadcaster\handler.go"
if %errorlevel% neq 0 (
    echo ERROR compilando broadcaster
    exit /b 1
)
echo   OK: bin\broadcaster\bootstrap

echo Compilando quiz-engine...
mkdir "%BIN_DIR%\quiz-engine" 2>nul
go build -tags lambda.norpc -ldflags="-s -w" -o "%BIN_DIR%\quiz-engine\bootstrap" "%REPO_ROOT%\lambdas\quiz-engine\handler.go"
if %errorlevel% neq 0 (
    echo ERROR compilando quiz-engine
    exit /b 1
)
echo   OK: bin\quiz-engine\bootstrap

echo Compilando round-ender...
mkdir "%BIN_DIR%\round-ender" 2>nul
go build -tags lambda.norpc -ldflags="-s -w" -o "%BIN_DIR%\round-ender\bootstrap" "%REPO_ROOT%\lambdas\round-ender\handler.go"
if %errorlevel% neq 0 (
    echo ERROR compilando round-ender
    exit /b 1
)
echo   OK: bin\round-ender\bootstrap

echo.
echo Build completo. Siguiente paso:
echo   cd infrastructure\terraform
echo   terraform init
echo   terraform apply

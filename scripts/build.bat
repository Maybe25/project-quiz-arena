@echo off
REM build.bat — Empaqueta todas las Lambdas Python para AWS Lambda (Windows)
REM
REM Copia handler.py + shared/ en bin\<name>\ para cada Lambda.
REM Terraform zipea esos directorios y los despliega.

setlocal enabledelayedexpansion

set REPO_ROOT=%~dp0..
set BIN_DIR=%REPO_ROOT%\bin
set SHARED_DIR=%REPO_ROOT%\shared

call :package_lambda ws-connect
call :package_lambda ws-disconnect
call :package_lambda ws-message
call :package_lambda room-manager
call :package_lambda broadcaster
call :package_lambda quiz-engine
call :package_lambda round-ender
call :package_lambda stats-recorder

echo.
echo Build completo. Siguiente paso:
echo   cd infrastructure\terraform
echo   terraform init
echo   terraform apply
exit /b 0

:package_lambda
set NAME=%~1
set OUT=%BIN_DIR%\%NAME%
echo Empaquetando %NAME%...
if exist "%OUT%" rmdir /s /q "%OUT%"
mkdir "%OUT%"
copy "%REPO_ROOT%\lambdas\%NAME%\handler.py" "%OUT%\handler.py" >nul
xcopy /e /i /q "%SHARED_DIR%" "%OUT%\shared" >nul
echo   OK: bin\%NAME%\
exit /b 0

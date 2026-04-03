@echo off
REM bootstrap.bat — Crea los recursos de estado de Terraform en AWS.
REM
REM Ejecutar UNA SOLA VEZ antes del primer terraform init.
REM No forma parte del pipeline CI/CD — es un paso de setup inicial.
REM
REM Prerequisitos:
REM   - AWS CLI instalado y configurado (aws configure)
REM   - Permisos para crear S3 buckets y tablas DynamoDB
REM
REM Resultado:
REM   - S3 bucket para almacenar el terraform.tfstate de forma remota
REM   - DynamoDB table para bloquear el estado (evita apply simultáneos)

setlocal enabledelayedexpansion

REM Obtener el Account ID de AWS para hacer el bucket name único globalmente
for /f "delims=" %%i in ('aws sts get-caller-identity --query Account --output text') do set ACCOUNT_ID=%%i
if "%ACCOUNT_ID%"=="" (
    echo ERROR: No se pudo obtener el Account ID. Verifica que AWS CLI este configurado.
    exit /b 1
)

set REGION=us-east-1
set BUCKET_NAME=quizarena-tfstate-%ACCOUNT_ID%
set LOCK_TABLE=quizarena-tfstate-lock

echo.
echo === QuizArena Terraform Bootstrap ===
echo Account ID : %ACCOUNT_ID%
echo Region     : %REGION%
echo S3 Bucket  : %BUCKET_NAME%
echo DynamoDB   : %LOCK_TABLE%
echo.

REM --- Crear el bucket S3 para el estado de Terraform ---
echo [1/4] Creando bucket S3...
aws s3api create-bucket ^
    --bucket %BUCKET_NAME% ^
    --region %REGION% >nul 2>&1

if %errorlevel% neq 0 (
    REM Si ya existe y es nuestro, continuamos
    echo       El bucket ya existe, continuando...
)

REM Habilitar versionado: permite recuperar estados anteriores si algo sale mal
echo [2/4] Habilitando versionado en S3...
aws s3api put-bucket-versioning ^
    --bucket %BUCKET_NAME% ^
    --versioning-configuration Status=Enabled

REM Bloquear acceso publico: el estado puede contener ARNs y datos de infraestructura
echo [3/4] Bloqueando acceso publico al bucket...
aws s3api put-public-access-block ^
    --bucket %BUCKET_NAME% ^
    --public-access-block-configuration ^
    "BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true"

REM --- Crear tabla DynamoDB para locking ---
REM Terraform usa esta tabla para evitar que dos apply corran al mismo tiempo.
REM La clave LockID es el path del estado.
echo [4/4] Creando tabla DynamoDB para locking...
aws dynamodb create-table ^
    --table-name %LOCK_TABLE% ^
    --attribute-definitions AttributeName=LockID,AttributeType=S ^
    --key-schema AttributeName=LockID,KeyType=HASH ^
    --billing-mode PAY_PER_REQUEST ^
    --region %REGION% >nul 2>&1

if %errorlevel% neq 0 (
    echo       La tabla ya existe, continuando...
)

echo.
echo === Bootstrap completo ===
echo.
echo SIGUIENTE PASO: Actualiza infrastructure\terraform\backend.tf con:
echo.
echo   bucket = "%BUCKET_NAME%"
echo   region = "%REGION%"
echo.
echo Luego ejecuta:
echo   cd infrastructure\terraform
echo   terraform init
echo.

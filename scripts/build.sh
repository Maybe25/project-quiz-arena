#!/usr/bin/env bash
# build.sh — Compila todas las Lambdas Go para ARM64 (Graviton2 / provided.al2023)
#
# Variables de entorno críticas:
#   GOOS=linux      → compilar para Linux (Lambda corre en Linux, no Windows)
#   GOARCH=arm64    → arquitectura ARM64 (Graviton2, ~20% más barato)
#   CGO_ENABLED=0   → binario estático sin dependencias del sistema
#
# El binario DEBE llamarse "bootstrap" — es lo que Lambda busca al iniciar.

set -euo pipefail # Salir si algún comando falla

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$REPO_ROOT/bin"

build_lambda() {
  local name="$1"
  local src="$REPO_ROOT/lambdas/$name"
  local out="$BIN_DIR/$name/bootstrap"

  echo "→ Compilando $name..."
  mkdir -p "$BIN_DIR/$name"

  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
    go build \
    -tags lambda.norpc \
    -ldflags="-s -w" \
    -o "$out" \
    "$src/handler.go"

  echo "  ✓ $out"
}

# Lista de Lambdas a compilar
build_lambda "ws-connect"
build_lambda "ws-disconnect"
build_lambda "ws-message"
build_lambda "room-manager"
build_lambda "broadcaster"
build_lambda "quiz-engine"
build_lambda "round-ender"
build_lambda "stats-recorder"

echo ""
echo "Build completo. Binarios en $BIN_DIR/"
echo "Siguiente paso: cd infrastructure/terraform && terraform apply"

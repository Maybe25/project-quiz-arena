#!/usr/bin/env bash
# build.sh — Empaqueta todas las Lambdas Python para AWS Lambda
#
# Estrategia:
#   Para cada Lambda, crea bin/<name>/ con:
#     - handler.py       (el handler de la Lambda)
#     - shared/          (librería compartida: dynamo, wsapi, events)
#
#   Terraform luego zipea bin/<name>/ y lo sube a Lambda.
#   No hay compilación — Python usa el runtime de Lambda directamente.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$REPO_ROOT/bin"
SHARED_DIR="$REPO_ROOT/shared"

package_lambda() {
  local name="$1"
  local out="$BIN_DIR/$name"

  echo "→ Empaquetando $name..."
  rm -rf "$out"
  mkdir -p "$out"

  cp "$REPO_ROOT/lambdas/$name/handler.py" "$out/handler.py"
  cp -r "$SHARED_DIR" "$out/shared"

  echo "  ✓ $out/"
}

package_lambda "ws-connect"
package_lambda "ws-disconnect"
package_lambda "ws-message"
package_lambda "room-manager"
package_lambda "broadcaster"
package_lambda "quiz-engine"
package_lambda "round-ender"
package_lambda "stats-recorder"

echo ""
echo "Build completo. Paquetes en $BIN_DIR/"
echo "Siguiente paso: cd infrastructure/terraform && terraform apply"

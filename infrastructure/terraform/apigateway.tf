# API Gateway WebSocket API
#
# En WebSocket hay 3 rutas especiales:
#   $connect    — cliente abre la conexión
#   $disconnect — cliente cierra la conexión (o timeout)
#   $default    — cualquier mensaje que no coincida con otras rutas
#
# M1: $connect y $disconnect
# M2: $default → ws-message (router)

resource "aws_apigatewayv2_api" "ws" {
  name                       = "${var.project_name}-${var.environment}-ws"
  protocol_type              = "WEBSOCKET"

  # route_selection_expression determina qué campo del JSON usa API GW para
  # elegir la ruta. "$request.body.action" significa: si el cliente envía
  # {"action": "JOIN_ROOM", ...}, API GW busca la ruta "JOIN_ROOM".
  route_selection_expression = "$request.body.action"
}

# --- Integraciones Lambda (una por ruta) ---
# Una "integración" conecta una ruta de API GW con una Lambda.

resource "aws_apigatewayv2_integration" "ws_connect" {
  api_id           = aws_apigatewayv2_api.ws.id
  integration_type = "AWS_PROXY" # Lambda proxy: el evento completo va a la Lambda

  # Para WebSocket usamos POST internamente (transparente para el cliente).
  integration_method = "POST"
  integration_uri    = aws_lambda_function.ws_connect.invoke_arn
}

resource "aws_apigatewayv2_integration" "ws_disconnect" {
  api_id             = aws_apigatewayv2_api.ws.id
  integration_type   = "AWS_PROXY"
  integration_method = "POST"
  integration_uri    = aws_lambda_function.ws_disconnect.invoke_arn
}

# --- Rutas ---

resource "aws_apigatewayv2_route" "connect" {
  api_id    = aws_apigatewayv2_api.ws.id
  route_key = "$connect"
  target    = "integrations/${aws_apigatewayv2_integration.ws_connect.id}"
}

resource "aws_apigatewayv2_route" "disconnect" {
  api_id    = aws_apigatewayv2_api.ws.id
  route_key = "$disconnect"
  target    = "integrations/${aws_apigatewayv2_integration.ws_disconnect.id}"
}

# $default: cualquier mensaje del cliente va a ws-message.
# ws-message lee el campo "action" y decide a qué cola SQS enviarlo.
resource "aws_apigatewayv2_integration" "ws_message" {
  api_id             = aws_apigatewayv2_api.ws.id
  integration_type   = "AWS_PROXY"
  integration_method = "POST"
  integration_uri    = aws_lambda_function.ws_message.invoke_arn
}

resource "aws_apigatewayv2_route" "default" {
  api_id    = aws_apigatewayv2_api.ws.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.ws_message.id}"
}

# --- Stage (entorno de despliegue) ---
# El stage "dev" es la URL pública. Con auto_deploy = true, cada cambio
# en las rutas se despliega automáticamente sin un paso extra.
resource "aws_apigatewayv2_stage" "dev" {
  api_id      = aws_apigatewayv2_api.ws.id
  name        = var.environment
  auto_deploy = true

  # Logging de API GW (útil para debuggear en M1).
  # Requiere un IAM role — lo simplificamos habilitando solo los logs de acceso.
  default_route_settings {
    throttling_burst_limit   = 100
    throttling_rate_limit    = 50
  }
}

# --- Permisos para que API GW invoque las Lambdas ---
# Sin esto, API GW no puede llamar a las Lambdas aunque estén en la misma cuenta.
resource "aws_lambda_permission" "ws_connect" {
  statement_id  = "AllowAPIGWInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_connect.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.ws.execution_arn}/*/*"
}

resource "aws_lambda_permission" "ws_disconnect" {
  statement_id  = "AllowAPIGWInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_disconnect.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.ws.execution_arn}/*/*"
}

# --- Outputs ---
output "websocket_url" {
  value       = aws_apigatewayv2_stage.dev.invoke_url
  description = "URL WebSocket para conectar con wscat: wscat -c <url>"
}

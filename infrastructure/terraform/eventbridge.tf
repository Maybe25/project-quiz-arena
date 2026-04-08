# EventBridge — bus de eventos custom para QuizArena (M4)
#
# ¿Por qué EventBridge y no invocar stats-recorder directamente?
# - Desacoplamiento: round-ender no necesita saber que existe stats-recorder.
# - Extensibilidad: en M5 podemos agregar más consumidores (analytics, notificaciones)
#   sin tocar round-ender.
# - Reintentos automáticos: EventBridge reintenta si stats-recorder falla.

resource "aws_cloudwatch_event_bus" "quizarena" {
  name = "${var.project_name}-${var.environment}-events"
}

# Regla: captura todos los eventos "game.ended" del bus custom.
resource "aws_cloudwatch_event_rule" "game_ended" {
  name           = "${var.project_name}-${var.environment}-game-ended"
  event_bus_name = aws_cloudwatch_event_bus.quizarena.name

  event_pattern = jsonencode({
    source      = ["quizarena"]
    detail-type = ["game.ended"]
  })
}

# Target: stats-recorder Lambda recibe los eventos game.ended.
resource "aws_cloudwatch_event_target" "stats_recorder" {
  rule           = aws_cloudwatch_event_rule.game_ended.name
  event_bus_name = aws_cloudwatch_event_bus.quizarena.name
  target_id      = "stats-recorder"
  arn            = aws_lambda_function.stats_recorder.arn
}

# Permiso para que EventBridge invoque stats-recorder.
resource "aws_lambda_permission" "eventbridge_stats_recorder" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.stats_recorder.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.game_ended.arn
}

output "eventbridge_bus_name" {
  value       = aws_cloudwatch_event_bus.quizarena.name
  description = "Nombre del bus EventBridge (para round-ender)"
}

output "eventbridge_bus_arn" {
  value       = aws_cloudwatch_event_bus.quizarena.arn
  description = "ARN del bus EventBridge"
}

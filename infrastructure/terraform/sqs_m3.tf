# SQS FIFO para quiz-engine (M3)
#
# quiz-engine maneja START_GAME y SUBMIT_ANSWER.
# Usamos FIFO para garantizar que los mensajes de un mismo jugador
# se procesen en orden (evita que SUBMIT_ANSWER llegue antes que START_GAME).

resource "aws_sqs_queue" "quiz_engine_dlq" {
  name                        = "${var.project_name}-${var.environment}-quiz-engine-dlq.fifo"
  fifo_queue                  = true
  content_based_deduplication = true
  message_retention_seconds   = 1209600 # 14 días
}

resource "aws_sqs_queue" "quiz_engine" {
  name                        = "${var.project_name}-${var.environment}-quiz-engine.fifo"
  fifo_queue                  = true
  content_based_deduplication = true
  visibility_timeout_seconds  = 60

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.quiz_engine_dlq.arn
    maxReceiveCount     = 3
  })
}

output "sqs_quiz_engine_url" {
  value       = aws_sqs_queue.quiz_engine.url
  description = "URL de la cola SQS quiz-engine (para ws-message)"
}

output "sqs_quiz_engine_arn" {
  value       = aws_sqs_queue.quiz_engine.arn
  description = "ARN de la cola SQS quiz-engine (para el trigger de quiz-engine Lambda)"
}

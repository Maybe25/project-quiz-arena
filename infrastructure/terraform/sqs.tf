# SQS FIFO para QuizArena M2
#
# Usamos FIFO (First-In-First-Out) porque el orden importa:
# si un jugador envía JOIN_ROOM y luego LEAVE_ROOM, deben procesarse en ese orden.
# Una cola Standard no garantiza orden ni deduplicación.
#
# Arquitectura de colas:
#   room-manager-queue.fifo  ← ws-message envía CREATE/JOIN/LEAVE/START
#   room-manager-dlq.fifo    ← mensajes que fallaron 3 veces (para debug)

# --- Dead Letter Queue (DLQ) ---
# Recibe mensajes que fallaron demasiadas veces.
# Sirve para debug: puedes ver qué mensajes no se pudieron procesar.
resource "aws_sqs_queue" "room_manager_dlq" {
  name                        = "${var.project_name}-${var.environment}-room-manager-dlq.fifo"
  fifo_queue                  = true
  content_based_deduplication = true
  message_retention_seconds   = 1209600 # 14 días — tiempo para investigar
}

# --- Cola principal de room-manager ---
resource "aws_sqs_queue" "room_manager" {
  name                        = "${var.project_name}-${var.environment}-room-manager.fifo"
  fifo_queue                  = true
  content_based_deduplication = true

  # Visibility timeout: cuánto tiempo el mensaje "desaparece" mientras Lambda lo procesa.
  # Debe ser >= timeout de Lambda × 6 (regla de AWS).
  # Lambda timeout = 10s → visibility = 60s
  visibility_timeout_seconds = 60

  # Redrive policy: después de 3 intentos fallidos, mover a DLQ.
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.room_manager_dlq.arn
    maxReceiveCount     = 3
  })
}

# --- Outputs ---
output "sqs_room_manager_url" {
  value       = aws_sqs_queue.room_manager.url
  description = "URL de la cola SQS room-manager (para ws-message)"
}

output "sqs_room_manager_arn" {
  value       = aws_sqs_queue.room_manager.arn
  description = "ARN de la cola SQS room-manager (para el trigger de room-manager Lambda)"
}

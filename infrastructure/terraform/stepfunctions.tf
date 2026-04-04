# Step Functions Express Workflow — Timer de rondas
#
# ¿Por qué Express y no Standard?
#   - Express: pago por duración + transiciones. Ideal para flujos cortos y frecuentes.
#   - Standard: pago por transición, estado persistido 1 año. Para flujos largos o que
#     necesitan exactly-once. Una partida de quiz dura minutos → Express es más barato.
#
# Flujo del state machine:
#
#   Input: { roomId, currentRound, totalRounds, roundDurationSeconds }
#
#   WaitForRoundEnd
#     │  (espera roundDurationSeconds segundos)
#     ▼
#   InvokeRoundEnder       ← llama a la Lambda round-ender
#     │  (retorna el nuevo estado: hasMoreRounds, currentRound actualizado)
#     ▼
#   CheckMoreRounds
#     ├── hasMoreRounds == true  → vuelve a WaitForRoundEnd (loop)
#     └── hasMoreRounds == false → GameComplete (Succeed)
#
# Este loop maneja todas las rondas de un partido con un solo state machine.

# --- IAM Role para Step Functions ---
# Step Functions necesita permisos para invocar la Lambda round-ender.
resource "aws_iam_role" "sfn_round_timer" {
  name = "${var.project_name}-${var.environment}-sfn-round-timer"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "states.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "sfn_invoke_lambda" {
  name = "invoke-round-ender"
  role = aws_iam_role.sfn_round_timer.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = aws_lambda_function.round_ender.arn
    }]
  })
}

# --- State Machine ---
# La definición usa la sintaxis Amazon States Language (ASL) en JSON.
# templatefile() inyecta el ARN de la Lambda en el JSON — sin hardcodear ARNs.
resource "aws_sfn_state_machine" "round_timer" {
  name     = "${var.project_name}-${var.environment}-round-timer"
  role_arn = aws_iam_role.sfn_round_timer.arn
  type     = "EXPRESS" # más barato para flujos cortos y frecuentes

  definition = jsonencode({
    Comment = "QuizArena round timer — espera N segundos, invoca round-ender, loop o fin"
    StartAt = "WaitForRoundEnd"
    States = {
      WaitForRoundEnd = {
        Type = "Wait"
        # SecondsPath lee el valor dinámico del estado — cada ronda puede tener
        # un tiempo diferente (ej. rondas finales más cortas).
        SecondsPath = "$.roundDurationSeconds"
        Next        = "InvokeRoundEnder"
      }
      InvokeRoundEnder = {
        Type     = "Task"
        Resource = aws_lambda_function.round_ender.arn
        # ResultPath: "$" reemplaza TODO el estado con lo que retorna round-ender.
        # Así round-ender controla completamente el estado del próximo ciclo.
        ResultPath = "$"
        Next       = "CheckMoreRounds"
        Retry = [{
          ErrorEquals     = ["Lambda.ServiceException", "Lambda.AWSLambdaException", "Lambda.SdkClientException"]
          IntervalSeconds = 2
          MaxAttempts     = 2
          BackoffRate     = 2
        }]
      }
      CheckMoreRounds = {
        Type = "Choice"
        Choices = [{
          Variable      = "$.hasMoreRounds"
          BooleanEquals = true
          Next          = "WaitForRoundEnd"
        }]
        Default = "GameComplete"
      }
      GameComplete = {
        Type = "Succeed"
      }
    }
  })
}

output "sfn_round_timer_arn" {
  value       = aws_sfn_state_machine.round_timer.arn
  description = "ARN del state machine de rondas (para quiz-engine Lambda)"
}

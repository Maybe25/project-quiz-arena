# Lambdas de M3: quiz-engine y round-ender
# También incluye los permisos IAM adicionales para los nuevos recursos.

# --- Permisos IAM adicionales para M3 ---

# ws-message también puede enviar a la cola quiz-engine.
resource "aws_iam_role_policy" "lambda_sqs_send_quiz_engine" {
  name = "sqs-send-quiz-engine"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["sqs:SendMessage"]
      Resource = [aws_sqs_queue.quiz_engine.arn]
    }]
  })
}

# quiz-engine puede consumir su propia cola.
resource "aws_iam_role_policy" "lambda_sqs_consume_quiz_engine" {
  name = "sqs-consume-quiz-engine"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes",
      ]
      Resource = [aws_sqs_queue.quiz_engine.arn]
    }]
  })
}

# quiz-engine puede leer preguntas del bucket S3.
resource "aws_iam_role_policy" "lambda_s3_questions" {
  name = "s3-read-questions"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject"]
      Resource = "${aws_s3_bucket.questions.arn}/*"
    }]
  })
}

# quiz-engine puede iniciar ejecuciones del state machine.
resource "aws_iam_role_policy" "lambda_sfn_start" {
  name = "sfn-start-execution"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["states:StartExecution"]
      Resource = aws_sfn_state_machine.round_timer.arn
    }]
  })
}

# --- ZIP de binarios M3 ---

data "archive_file" "quiz_engine" {
  type        = "zip"
  source_file = "${path.module}/../../bin/quiz-engine/bootstrap"
  output_path = "${path.module}/../../bin/quiz-engine/function.zip"
}

data "archive_file" "round_ender" {
  type        = "zip"
  source_file = "${path.module}/../../bin/round-ender/bootstrap"
  output_path = "${path.module}/../../bin/round-ender/function.zip"
}

# --- Lambda quiz-engine ---
resource "aws_lambda_function" "quiz_engine" {
  function_name    = "${var.project_name}-${var.environment}-quiz-engine"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = data.archive_file.quiz_engine.output_path
  source_code_hash = data.archive_file.quiz_engine.output_base64sha256
  memory_size      = var.lambda_memory_mb
  timeout          = var.lambda_timeout_s

  environment {
    variables = {
      DYNAMODB_TABLE       = aws_dynamodb_table.main.name
      WS_ENDPOINT          = "https://${aws_apigatewayv2_api.ws.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
      S3_QUESTIONS_BUCKET  = aws_s3_bucket.questions.bucket
      S3_QUESTIONS_KEY     = "questions/general.json"
      SFN_ROUND_TIMER_ARN  = aws_sfn_state_machine.round_timer.arn
    }
  }
}

# --- Lambda round-ender ---
# Esta Lambda NO viene de SQS — es invocada directamente por Step Functions.
# Por eso no tiene event_source_mapping, solo la función en sí.
resource "aws_lambda_function" "round_ender" {
  function_name    = "${var.project_name}-${var.environment}-round-ender"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = data.archive_file.round_ender.output_path
  source_code_hash = data.archive_file.round_ender.output_base64sha256
  memory_size      = var.lambda_memory_mb
  timeout          = 30 # round-ender puede tardar más si hay muchos jugadores

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.main.name
      WS_ENDPOINT    = "https://${aws_apigatewayv2_api.ws.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
    }
  }
}

# --- SQS Event Source Mapping — quiz-engine ---
resource "aws_lambda_event_source_mapping" "quiz_engine_sqs" {
  event_source_arn        = aws_sqs_queue.quiz_engine.arn
  function_name           = aws_lambda_function.quiz_engine.arn
  batch_size              = 10
  function_response_types = ["ReportBatchItemFailures"]
}

# --- CloudWatch Log Groups M3 ---
resource "aws_cloudwatch_log_group" "quiz_engine" {
  name              = "/aws/lambda/${aws_lambda_function.quiz_engine.function_name}"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "round_ender" {
  name              = "/aws/lambda/${aws_lambda_function.round_ender.function_name}"
  retention_in_days = 7
}

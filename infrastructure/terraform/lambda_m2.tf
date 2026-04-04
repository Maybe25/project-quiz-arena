# Lambdas de M2: ws-message, room-manager, broadcaster
# Los permisos IAM extra también están aquí.

# --- Permisos IAM adicionales para M2 ---

# ws-message necesita enviar mensajes a SQS.
resource "aws_iam_role_policy" "lambda_sqs_send" {
  name = "sqs-send"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = ["sqs:SendMessage"]
      Resource = [
        aws_sqs_queue.room_manager.arn,
      ]
    }]
  })
}

# room-manager y broadcaster necesitan leer/borrar de SQS.
resource "aws_iam_role_policy" "lambda_sqs_consume" {
  name = "sqs-consume"
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
      Resource = [
        aws_sqs_queue.room_manager.arn,
      ]
    }]
  })
}

# broadcaster necesita PostToConnection en API Gateway.
resource "aws_iam_role_policy" "lambda_apigateway_post" {
  name = "apigateway-post"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["execute-api:ManageConnections"]
      Resource = "${aws_apigatewayv2_api.ws.execution_arn}/*"
    }]
  })
}

# --- ZIP de binarios M2 ---

data "archive_file" "ws_message" {
  type        = "zip"
  source_file = "${path.module}/../../bin/ws-message/bootstrap"
  output_path = "${path.module}/../../bin/ws-message/function.zip"
}

data "archive_file" "room_manager" {
  type        = "zip"
  source_file = "${path.module}/../../bin/room-manager/bootstrap"
  output_path = "${path.module}/../../bin/room-manager/function.zip"
}

data "archive_file" "broadcaster" {
  type        = "zip"
  source_file = "${path.module}/../../bin/broadcaster/bootstrap"
  output_path = "${path.module}/../../bin/broadcaster/function.zip"
}

# --- Lambda ws-message ---
resource "aws_lambda_function" "ws_message" {
  function_name    = "${var.project_name}-${var.environment}-ws-message"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = data.archive_file.ws_message.output_path
  source_code_hash = data.archive_file.ws_message.output_base64sha256
  memory_size      = var.lambda_memory_mb
  timeout          = var.lambda_timeout_s

  environment {
    variables = {
      DYNAMODB_TABLE       = aws_dynamodb_table.main.name
      SQS_ROOM_MANAGER_URL = aws_sqs_queue.room_manager.url
      SQS_QUIZ_ENGINE_URL  = aws_sqs_queue.quiz_engine.url
    }
  }
}

# --- Lambda room-manager ---
resource "aws_lambda_function" "room_manager" {
  function_name    = "${var.project_name}-${var.environment}-room-manager"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = data.archive_file.room_manager.output_path
  source_code_hash = data.archive_file.room_manager.output_base64sha256
  memory_size      = var.lambda_memory_mb
  timeout          = var.lambda_timeout_s

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.main.name
      WS_ENDPOINT    = "https://${aws_apigatewayv2_api.ws.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
    }
  }
}

# --- Lambda broadcaster ---
resource "aws_lambda_function" "broadcaster" {
  function_name    = "${var.project_name}-${var.environment}-broadcaster"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = data.archive_file.broadcaster.output_path
  source_code_hash = data.archive_file.broadcaster.output_base64sha256
  memory_size      = var.lambda_memory_mb
  timeout          = var.lambda_timeout_s

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.main.name
      WS_ENDPOINT    = "https://${aws_apigatewayv2_api.ws.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
    }
  }
}

# --- SQS Event Source Mapping ---
# Conecta la cola SQS con room-manager: cada vez que llega un mensaje,
# Lambda se dispara automáticamente.
resource "aws_lambda_event_source_mapping" "room_manager_sqs" {
  event_source_arn                   = aws_sqs_queue.room_manager.arn
  function_name                      = aws_lambda_function.room_manager.arn
  batch_size              = 10   # procesar hasta 10 mensajes por invocación
  function_response_types = ["ReportBatchItemFailures"]
}

# --- Permisos API GW → ws-message ---
resource "aws_lambda_permission" "ws_message" {
  statement_id  = "AllowAPIGWInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_message.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.ws.execution_arn}/*/*"
}

# --- CloudWatch Log Groups M2 ---
resource "aws_cloudwatch_log_group" "ws_message" {
  name              = "/aws/lambda/${aws_lambda_function.ws_message.function_name}"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "room_manager" {
  name              = "/aws/lambda/${aws_lambda_function.room_manager.function_name}"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "broadcaster" {
  name              = "/aws/lambda/${aws_lambda_function.broadcaster.function_name}"
  retention_in_days = 7
}

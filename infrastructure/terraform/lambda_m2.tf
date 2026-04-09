# Lambdas de M2: ws-message, room-manager, broadcaster

resource "aws_iam_role_policy" "lambda_sqs_send" {
  name = "sqs-send"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["sqs:SendMessage"]
      Resource = [aws_sqs_queue.room_manager.arn]
    }]
  })
}

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
      Resource = [aws_sqs_queue.room_manager.arn]
    }]
  })
}

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

data "archive_file" "ws_message" {
  type        = "zip"
  source_dir  = "${path.module}/../../bin/ws-message"
  output_path = "${path.module}/../../bin/ws-message.zip"
}

data "archive_file" "room_manager" {
  type        = "zip"
  source_dir  = "${path.module}/../../bin/room-manager"
  output_path = "${path.module}/../../bin/room-manager.zip"
}

data "archive_file" "broadcaster" {
  type        = "zip"
  source_dir  = "${path.module}/../../bin/broadcaster"
  output_path = "${path.module}/../../bin/broadcaster.zip"
}

resource "aws_lambda_function" "ws_message" {
  function_name    = "${var.project_name}-${var.environment}-ws-message"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "python3.12"
  architectures    = ["x86_64"]
  handler          = "handler.handler"
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

resource "aws_lambda_function" "room_manager" {
  function_name    = "${var.project_name}-${var.environment}-room-manager"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "python3.12"
  architectures    = ["x86_64"]
  handler          = "handler.handler"
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

resource "aws_lambda_function" "broadcaster" {
  function_name    = "${var.project_name}-${var.environment}-broadcaster"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "python3.12"
  architectures    = ["x86_64"]
  handler          = "handler.handler"
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

resource "aws_lambda_event_source_mapping" "room_manager_sqs" {
  event_source_arn        = aws_sqs_queue.room_manager.arn
  function_name           = aws_lambda_function.room_manager.arn
  batch_size              = 10
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_permission" "ws_message" {
  statement_id  = "AllowAPIGWInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_message.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.ws.execution_arn}/*/*"
}

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

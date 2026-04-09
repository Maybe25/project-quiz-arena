# Lambdas de M4: stats-recorder

resource "aws_iam_role_policy" "lambda_stats_write" {
  name = "stats-write"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:UpdateItem",
        "dynamodb:GetItem",
        "dynamodb:Query",
      ]
      Resource = [
        aws_dynamodb_table.main.arn,
        "${aws_dynamodb_table.main.arn}/index/leaderboard-index",
      ]
    }]
  })
}

resource "aws_iam_role_policy" "lambda_eventbridge_publish" {
  name = "eventbridge-publish"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["events:PutEvents"]
      Resource = aws_cloudwatch_event_bus.quizarena.arn
    }]
  })
}

data "archive_file" "stats_recorder" {
  type        = "zip"
  source_dir  = "${path.module}/../../bin/stats-recorder"
  output_path = "${path.module}/../../bin/stats-recorder.zip"
}

resource "aws_lambda_function" "stats_recorder" {
  function_name    = "${var.project_name}-${var.environment}-stats-recorder"
  role             = aws_iam_role.lambda_exec.arn
  runtime          = "python3.12"
  architectures    = ["x86_64"]
  handler          = "handler.handler"
  filename         = data.archive_file.stats_recorder.output_path
  source_code_hash = data.archive_file.stats_recorder.output_base64sha256
  memory_size      = var.lambda_memory_mb
  timeout          = var.lambda_timeout_s

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.main.name
    }
  }
}

resource "aws_cloudwatch_log_group" "stats_recorder" {
  name              = "/aws/lambda/${aws_lambda_function.stats_recorder.function_name}"
  retention_in_days = 7
}

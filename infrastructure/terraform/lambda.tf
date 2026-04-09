resource "aws_iam_role" "lambda_exec" {
  name = "${var.project_name}-${var.environment}-lambda-exec"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "lambda_dynamodb" {
  name = "dynamodb-access"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:PutItem",
        "dynamodb:DeleteItem",
        "dynamodb:GetItem",
        "dynamodb:Query",
        "dynamodb:UpdateItem",
      ]
      Resource = [
        aws_dynamodb_table.main.arn,
        "${aws_dynamodb_table.main.arn}/index/*",
      ]
    }]
  })
}

# --- ZIP de los paquetes Lambda ---
# build.sh prepara bin/<name>/ con handler.py + shared/.
# Terraform zipea el directorio y calcula el hash automáticamente.

data "archive_file" "ws_connect" {
  type        = "zip"
  source_dir  = "${path.module}/../../bin/ws-connect"
  output_path = "${path.module}/../../bin/ws-connect.zip"
}

data "archive_file" "ws_disconnect" {
  type        = "zip"
  source_dir  = "${path.module}/../../bin/ws-disconnect"
  output_path = "${path.module}/../../bin/ws-disconnect.zip"
}

# --- Lambda ws-connect ---
resource "aws_lambda_function" "ws_connect" {
  function_name = "${var.project_name}-${var.environment}-ws-connect"
  role          = aws_iam_role.lambda_exec.arn
  runtime       = "python3.12"
  architectures = ["x86_64"]
  handler       = "handler.handler"

  filename         = data.archive_file.ws_connect.output_path
  source_code_hash = data.archive_file.ws_connect.output_base64sha256

  memory_size = var.lambda_memory_mb
  timeout     = var.lambda_timeout_s

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.main.name
    }
  }
}

# --- Lambda ws-disconnect ---
resource "aws_lambda_function" "ws_disconnect" {
  function_name = "${var.project_name}-${var.environment}-ws-disconnect"
  role          = aws_iam_role.lambda_exec.arn
  runtime       = "python3.12"
  architectures = ["x86_64"]
  handler       = "handler.handler"

  filename         = data.archive_file.ws_disconnect.output_path
  source_code_hash = data.archive_file.ws_disconnect.output_base64sha256

  memory_size = var.lambda_memory_mb
  timeout     = var.lambda_timeout_s

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.main.name
    }
  }
}

resource "aws_cloudwatch_log_group" "ws_connect" {
  name              = "/aws/lambda/${aws_lambda_function.ws_connect.function_name}"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "ws_disconnect" {
  name              = "/aws/lambda/${aws_lambda_function.ws_disconnect.function_name}"
  retention_in_days = 7
}

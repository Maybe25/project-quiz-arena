# IAM Role que todas las Lambdas asumen al ejecutarse.
# "assume_role_policy" le dice a AWS: "Lambda puede asumir este role".
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

# Política básica de Lambda: permite escribir logs en CloudWatch.
# Sin esto, no verías NADA en los logs — esto es lo mínimo indispensable.
resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Política custom: permisos sobre DynamoDB.
# Principio de mínimo privilegio: solo las acciones que realmente necesitamos.
resource "aws_iam_role_policy" "lambda_dynamodb" {
  name = "dynamodb-access"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:PutItem",      # ws-connect guarda la conexión
        "dynamodb:DeleteItem",   # ws-disconnect la elimina
        "dynamodb:GetItem",      # leer un item por PK+SK
        "dynamodb:Query",        # leer múltiples items (usaremos en M2)
        "dynamodb:UpdateItem",   # actualizar campos (usaremos en M2)
      ]
      Resource = [
        aws_dynamodb_table.main.arn,
        "${aws_dynamodb_table.main.arn}/index/*", # para los GSIs
      ]
    }]
  })
}

# --- ZIP de los binarios Lambda ---
# Terraform necesita empaquetar los binarios en ZIPs para subirlos a Lambda.
# El binario DEBE llamarse "bootstrap" para el runtime provided.al2023.

data "archive_file" "ws_connect" {
  type        = "zip"
  source_file = "${path.module}/../../bin/ws-connect/bootstrap"
  output_path = "${path.module}/../../bin/ws-connect/function.zip"
}

data "archive_file" "ws_disconnect" {
  type        = "zip"
  source_file = "${path.module}/../../bin/ws-disconnect/bootstrap"
  output_path = "${path.module}/../../bin/ws-disconnect/function.zip"
}

# --- Lambda ws-connect ---
resource "aws_lambda_function" "ws_connect" {
  function_name = "${var.project_name}-${var.environment}-ws-connect"
  role          = aws_iam_role.lambda_exec.arn

  # provided.al2023 = runtime personalizado en Amazon Linux 2023.
  # Go compila a un binario estático — no necesita nada del SO.
  runtime = "provided.al2023"

  # ARM64 = Graviton2: ~20% más barato que x86_64, mismo rendimiento para Go.
  architectures = ["arm64"]

  # El handler se ignora para provided.al2023 — Lambda ejecuta "./bootstrap" directamente.
  handler = "bootstrap"

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
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  handler       = "bootstrap"

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

# --- CloudWatch Log Groups ---
# Sin esto, Lambda crea los grupos automáticamente pero sin retención definida.
# 7 días es suficiente para dev y evita costos de logs acumulados.
resource "aws_cloudwatch_log_group" "ws_connect" {
  name              = "/aws/lambda/${aws_lambda_function.ws_connect.function_name}"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "ws_disconnect" {
  name              = "/aws/lambda/${aws_lambda_function.ws_disconnect.function_name}"
  retention_in_days = 7
}

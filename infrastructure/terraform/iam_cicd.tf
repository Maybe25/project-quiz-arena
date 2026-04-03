# IAM para CI/CD con GitHub Actions via OIDC
#
# OIDC (OpenID Connect) permite que GitHub Actions se autentique con AWS
# sin almacenar ninguna credencial. El flujo es:
#
#   1. GitHub Actions solicita un token JWT a GitHub
#   2. Presenta ese token a AWS STS
#   3. AWS verifica la firma con la clave pública de GitHub
#   4. AWS emite credenciales temporales (15 min) para el IAM Role
#
# Esto significa: CERO Access Keys en GitHub Secrets.

data "aws_caller_identity" "current" {}

# --- OIDC Provider de GitHub ---
# Registra a GitHub como proveedor de identidades de confianza en esta cuenta AWS.
# Solo existe uno por cuenta — si ya lo tienes, Terraform lo detecta con data source.
resource "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"

  # client_id_list: el "audience" que GitHub pone en el token JWT.
  # "sts.amazonaws.com" es el valor estándar para autenticación con AWS.
  client_id_list = ["sts.amazonaws.com"]

  # AWS valida el certificado TLS de GitHub automáticamente desde 2023.
  # El thumbprint ya no es crítico pero es requerido por el recurso.
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]

  lifecycle {
    # Si ya existe el provider en la cuenta (de otro proyecto), no lo reemplaces.
    ignore_changes = [thumbprint_list]
  }
}

# --- IAM Role que GitHub Actions asume ---
resource "aws_iam_role" "github_actions" {
  name = "${var.project_name}-${var.environment}-github-actions"

  # Trust policy: define QUIÉN puede asumir este role.
  # La condición limita el acceso SOLO al repo Maybe25/project-quiz-arena.
  # Cambiar "ref:refs/heads/*" por "ref:refs/heads/main" para restringir solo a main.
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        }
        StringLike = {
          # Formato: repo:<usuario>/<repo>:*
          # El * permite cualquier rama o tag. Restringe a "ref:refs/heads/main" en prod.
          "token.actions.githubusercontent.com:sub" = "repo:Maybe25/project-quiz-arena:*"
        }
      }
    }]
  })
}

# --- Política: gestión de recursos QuizArena ---
# Principio de mínimo privilegio: solo los servicios y acciones que el pipeline necesita.
resource "aws_iam_role_policy" "github_actions_deploy" {
  name = "quizarena-deploy"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [

      # Lambda: crear, actualizar y configurar funciones
      # lambda:Get* y lambda:List* cubren todas las lecturas que hace el
      # provider de Terraform durante el refresh (GetFunctionCodeSigningConfig,
      # ListVersionsByFunction, GetPolicy, GetFunctionConcurrency, etc.)
      {
        Effect = "Allow"
        Action = [
          "lambda:Get*",
          "lambda:List*",
          "lambda:CreateFunction",
          "lambda:UpdateFunctionCode",
          "lambda:UpdateFunctionConfiguration",
          "lambda:DeleteFunction",
          "lambda:AddPermission",
          "lambda:RemovePermission",
          "lambda:TagResource",
          "lambda:CreateEventSourceMapping",
          "lambda:UpdateEventSourceMapping",
          "lambda:DeleteEventSourceMapping",
        ]
        Resource = "*"
      },

      # API Gateway WebSocket
      {
        Effect = "Allow"
        Action = [
          "apigateway:GET",
          "apigateway:POST",
          "apigateway:PUT",
          "apigateway:PATCH",
          "apigateway:DELETE",
          "apigateway:TagResource",
        ]
        Resource = "arn:aws:apigateway:${var.aws_region}::*"
      },

      # DynamoDB: gestión completa de tablas del proyecto
      {
        Effect = "Allow"
        Action = [
          "dynamodb:CreateTable",
          "dynamodb:UpdateTable",
          "dynamodb:DeleteTable",
          "dynamodb:DescribeTable",
          "dynamodb:DescribeContinuousBackups",
          "dynamodb:UpdateContinuousBackups",
          "dynamodb:DescribeTimeToLive",
          "dynamodb:UpdateTimeToLive",
          "dynamodb:ListTagsOfResource",
          "dynamodb:TagResource",
          "dynamodb:UntagResource",
        ]
        Resource = "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/${var.project_name}-*"
      },

      # SQS: gestión de colas del proyecto
      {
        Effect = "Allow"
        Action = [
          "sqs:CreateQueue",
          "sqs:DeleteQueue",
          "sqs:GetQueueAttributes",
          "sqs:SetQueueAttributes",
          "sqs:ListQueues",
          "sqs:TagQueue",
          "sqs:ListQueueTags",
          "sqs:UntagQueue",
        ]
        Resource = "arn:aws:sqs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:${var.project_name}-*"
      },

      # CloudWatch Logs: crear y gestionar log groups
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:DeleteLogGroup",
          "logs:PutRetentionPolicy",
          "logs:DescribeLogGroups",
          "logs:ListTagsLogGroup",
          "logs:TagLogGroup",
        ]
        Resource = "arn:aws:logs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:log-group:/aws/lambda/${var.project_name}-*"
      },

      # IAM: gestionar el role de ejecución de Lambda
      # Scope al proyecto para evitar escalar privilegios en otros recursos
      {
        Effect = "Allow"
        Action = [
          "iam:CreateRole",
          "iam:UpdateRole",
          "iam:DeleteRole",
          "iam:GetRole",
          "iam:PassRole",
          "iam:AttachRolePolicy",
          "iam:DetachRolePolicy",
          "iam:PutRolePolicy",
          "iam:GetRolePolicy",
          "iam:DeleteRolePolicy",
          "iam:ListRolePolicies",
          "iam:ListAttachedRolePolicies",
          "iam:TagRole",
          "iam:CreateOpenIDConnectProvider",
          "iam:GetOpenIDConnectProvider",
          "iam:UpdateOpenIDConnectProviderThumbprint",
          "iam:TagOpenIDConnectProvider",
        ]
        Resource = [
          "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/${var.project_name}-*",
          "arn:aws:iam::${data.aws_caller_identity.current.account_id}:oidc-provider/token.actions.githubusercontent.com",
        ]
      },

      # S3: leer y escribir el estado de Terraform
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket",
          "s3:GetBucketVersioning",
        ]
        Resource = [
          "arn:aws:s3:::quizarena-tfstate-${data.aws_caller_identity.current.account_id}",
          "arn:aws:s3:::quizarena-tfstate-${data.aws_caller_identity.current.account_id}/*",
        ]
      },

      # DynamoDB: locking del estado de Terraform
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:DeleteItem",
          "dynamodb:DescribeTable",
        ]
        Resource = "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/quizarena-tfstate-lock"
      },

      # STS: verificar identidad (usado por Terraform internamente)
      {
        Effect   = "Allow"
        Action   = ["sts:GetCallerIdentity"]
        Resource = "*"
      }
    ]
  })
}

output "github_actions_role_arn" {
  value       = aws_iam_role.github_actions.arn
  description = "ARN del IAM Role para GitHub Actions. Agregar como secret AWS_ROLE_ARN en el repo de GitHub."
}

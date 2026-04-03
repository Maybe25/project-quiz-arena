# DynamoDB Single-Table para QuizArena.
#
# Una sola tabla para TODO el proyecto — conexiones, salas, jugadores, preguntas.
# Esto puede parecer extraño si vienes de SQL, pero en DynamoDB con acceso
# por PK+SK es el patrón de producción correcto (Rick Houlihan pattern).
#
# On-Demand (PAY_PER_REQUEST): no pagamos por capacidad reservada.
# Perfecto para dev y para tráfico variable. Más caro a escala, pero
# para budget < $10/mes es la única opción sensata.

resource "aws_dynamodb_table" "main" {
  name         = "${var.project_name}-${var.environment}"
  billing_mode = "PAY_PER_REQUEST"

  # PK y SK son las claves de la tabla.
  # TODOS los items deben tener PK. SK es opcional pero lo usamos siempre.
  hash_key  = "PK"
  range_key = "SK"

  attribute {
    name = "PK"
    type = "S" # String
  }

  attribute {
    name = "SK"
    type = "S"
  }

  # TTL: DynamoDB lee el campo "expiresAt" (Unix timestamp en segundos)
  # y borra automáticamente los items cuyo timestamp ya pasó.
  # Esto limpia conexiones WebSocket muertas sin código adicional.
  ttl {
    attribute_name = "expiresAt"
    enabled        = true
  }

  # GSI-1: para listar salas disponibles.
  # PK del índice = "STATUS#waiting", SK = roomId
  # Permite hacer: "dame todas las salas en estado waiting"
  # sin escanear toda la tabla.
  #
  # Nota: los atributos del GSI deben declararse arriba en "attribute" también.
  # Los agregamos en M2 cuando implementemos las salas.

  # Point-in-time recovery: backups automáticos — gratis en dev, vale la pena.
  point_in_time_recovery {
    enabled = true
  }

  lifecycle {
    # Previene borrado accidental de la tabla con "terraform destroy" descuidado.
    # Para borrarla intencionalmente: cambiar a false, apply, luego destroy.
    prevent_destroy = false # Cambiar a true en prod
  }
}

output "dynamodb_table_name" {
  value       = aws_dynamodb_table.main.name
  description = "Nombre de la tabla DynamoDB"
}

output "dynamodb_table_arn" {
  value       = aws_dynamodb_table.main.arn
  description = "ARN de la tabla DynamoDB (necesario para los IAM policies de Lambda)"
}

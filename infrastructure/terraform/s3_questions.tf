# S3 bucket para las preguntas del quiz.
#
# Las preguntas son archivos JSON estáticos — se suben una sola vez a mano
# con: aws s3 cp questions/general.json s3://<bucket>/questions/general.json
#
# El bucket es PRIVADO. Solo la Lambda quiz-engine puede leer desde él.
# Nunca hay acceso público — las preguntas son content del juego, no assets web.

resource "aws_s3_bucket" "questions" {
  bucket = "${var.project_name}-questions-${data.aws_caller_identity.current.account_id}"

  # Previene borrado accidental del bucket con Terraform destroy.
  # Para borrar, primero vacía el bucket manualmente.
  lifecycle {
    prevent_destroy = false # en dev lo dejamos false; en prod cambiar a true
  }
}

# Bloquear acceso público explícitamente (por defecto en cuentas nuevas,
# pero es buena práctica declararlo para que el código documente la intención).
resource "aws_s3_bucket_public_access_block" "questions" {
  bucket = aws_s3_bucket.questions.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Versionado: guarda versiones anteriores de las preguntas.
# Si subimos una versión con errores, podemos rollback sin perder el archivo.
resource "aws_s3_bucket_versioning" "questions" {
  bucket = aws_s3_bucket.questions.id
  versioning_configuration {
    status = "Enabled"
  }
}

output "s3_questions_bucket" {
  value       = aws_s3_bucket.questions.bucket
  description = "Nombre del bucket S3 con las preguntas. Sube con: aws s3 cp questions/general.json s3://<bucket>/questions/general.json"
}

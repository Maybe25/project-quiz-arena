# Backend remoto de Terraform — estado en S3
#
# El estado de Terraform contiene el mapa completo de recursos creados.
# Guardarlo en S3 permite que:
#   1. GitHub Actions pueda leer/escribir el estado en el pipeline
#   2. Varios desarrolladores compartan el mismo estado
#   3. El estado no se pierda si borras tu máquina local
#
# IMPORTANTE: Este archivo usa valores que generó scripts/bootstrap.bat.
# Reemplaza ACCOUNT_ID con tu Account ID real (el que imprimió bootstrap.bat).
#
# Las variables NO funcionan en los bloques backend — deben ser valores literales.

terraform {
  backend "s3" {
    bucket         = "quizarena-tfstate-976138221384" # <- reemplazar ACCOUNT_ID
    key            = "quizarena/dev/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "quizarena-tfstate-lock" # locking para evitar conflictos
  }
}

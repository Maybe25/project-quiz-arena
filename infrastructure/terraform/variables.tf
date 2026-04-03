variable "aws_region" {
  description = "Región AWS donde se despliega todo"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Nombre del entorno (dev, staging, prod)"
  type        = string
  default     = "dev"
}

variable "project_name" {
  description = "Nombre base del proyecto, usado como prefijo en los recursos"
  type        = string
  default     = "quizarena"
}

variable "lambda_memory_mb" {
  description = "Memoria asignada a cada Lambda en MB"
  type        = number
  default     = 128 # Mínimo posible — más que suficiente para M1
}

variable "lambda_timeout_s" {
  description = "Timeout de Lambda en segundos"
  type        = number
  default     = 10
}

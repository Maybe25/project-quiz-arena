package dynamo

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// TableName es el nombre de la tabla DynamoDB, leído de una variable de entorno.
// Las Lambdas reciben el nombre de la tabla como env var para que Terraform lo inyecte.
func TableName() string {
	return os.Getenv("DYNAMODB_TABLE")
}

// NewClient crea un cliente DynamoDB usando las credenciales del entorno de Lambda.
// En Lambda, las credenciales vienen del IAM role asignado — no necesitas configurar nada.
func NewClient(ctx context.Context) (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return dynamodb.NewFromConfig(cfg), nil
}

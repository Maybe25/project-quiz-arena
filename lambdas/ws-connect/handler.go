// Lambda ws-connect: maneja el evento $connect de API Gateway WebSocket.
//
// API Gateway llama a esta función cada vez que un cliente abre una conexión.
// Nuestro trabajo: guardar el connectionId en DynamoDB para poder enviarle
// mensajes en el futuro (PostToConnection).
//
// El TTL de 24h en DynamoDB limpia automáticamente conexiones viejas —
// no necesitamos limpiar manualmente si el $disconnect falla.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	internaldynamo "github.com/quizarena/internal/dynamo"
)

// connectionRecord es lo que guardamos en DynamoDB por cada conexión activa.
// Los tags `dynamodbav:"..."` le dicen al SDK cómo mapear los campos.
type connectionRecord struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	PlayerID  string `dynamodbav:"playerId"`
	ConnectedAt int64  `dynamodbav:"connectedAt"` // Unix timestamp
	ExpiresAt   int64  `dynamodbav:"expiresAt"`   // TTL: DynamoDB borra el item cuando este timestamp pase
}

// logger es el logger estructurado del proceso — se inicializa una vez en init().
// slog es la librería de logging estándar de Go desde 1.21.
var logger *slog.Logger

func init() {
	// JSON logging para que CloudWatch Logs Insights pueda parsear los logs fácilmente.
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// handler es la función que Lambda invoca por cada evento $connect.
//
// events.APIGatewayWebsocketProxyRequest es el tipo que el SDK de Go usa para
// representar el evento que AWS envía cuando hay una nueva conexión WebSocket.
func handler(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connectionID := req.RequestContext.ConnectionID

	logger.InfoContext(ctx, "ws-connect",
		slog.String("connectionId", connectionID),
		slog.String("sourceIp", req.RequestContext.Identity.SourceIP),
	)

	// Crear el cliente DynamoDB. En Lambda, se recomienda crearlo fuera del handler
	// para reutilizarlo entre invocaciones calientes (warm starts).
	// Por simplicidad en M1, lo creamos dentro — lo optimizaremos en M2.
	dbClient, err := internaldynamo.NewClient(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create dynamo client", slog.String("error", err.Error()))
		return serverError(), nil
	}

	// Construir el registro de conexión.
	now := time.Now().Unix()
	record := connectionRecord{
		PK:          internaldynamo.ConnectionPK(connectionID),
		SK:          internaldynamo.ConnectionSK(),
		PlayerID:    guestPlayerID(connectionID), // En M4 usaremos JWT de Cognito
		ConnectedAt: now,
		ExpiresAt:   now + 24*60*60, // TTL: 24 horas desde ahora
	}

	// Serializar el struct a formato DynamoDB.
	// attributevalue.MarshalMap convierte un struct Go → map[string]types.AttributeValue
	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal record", slog.String("error", err.Error()))
		return serverError(), nil
	}

	// Guardar en DynamoDB.
	_, err = dbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(internaldynamo.TableName()),
		Item:      item,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to put item", slog.String("error", err.Error()))
		return serverError(), nil
	}

	logger.InfoContext(ctx, "connection saved",
		slog.String("connectionId", connectionID),
		slog.String("playerId", record.PlayerID),
	)

	// API Gateway WebSocket requiere HTTP 200 para aceptar la conexión.
	// Cualquier otro código rechaza al cliente.
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

// guestPlayerID genera un ID temporal para el jugador basado en el connectionId.
// En M4 reemplazaremos esto con la validación de JWT de Cognito.
func guestPlayerID(connectionID string) string {
	return fmt.Sprintf("guest-%s", connectionID)
}

// serverError retorna un 500 — aunque en WebSocket $connect el cliente solo
// ve "conexión rechazada", es buena práctica distinguir errores del servidor.
func serverError() events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{StatusCode: 500}
}

func main() {
	// lambda.Start registra nuestro handler con el runtime de AWS Lambda.
	// Cuando AWS invoca la función, llama a handler() con el evento correcto.
	lambda.Start(handler)
}

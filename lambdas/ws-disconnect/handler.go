// Lambda ws-disconnect: maneja el evento $disconnect de API Gateway WebSocket.
//
// API Gateway llama a esta función cuando una conexión se cierra (normalmente
// o por timeout). Nuestro trabajo: eliminar el connectionId de DynamoDB.
//
// IMPORTANTE: $disconnect puede no llamarse si la conexión se corta abruptamente
// (ej. el cliente pierde WiFi). Por eso los items tienen TTL de 24h — limpian
// solos aunque este handler nunca se ejecute.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	internaldynamo "github.com/quizarena/internal/dynamo"
)

var logger *slog.Logger

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func handler(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connectionID := req.RequestContext.ConnectionID

	logger.InfoContext(ctx, "ws-disconnect",
		slog.String("connectionId", connectionID),
	)

	dbClient, err := internaldynamo.NewClient(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create dynamo client", slog.String("error", err.Error()))
		// Para $disconnect siempre retornamos 200 — API Gateway ignora el código
		// pero es buena práctica. Un error acá solo generaría ruido en los logs.
		return events.APIGatewayProxyResponse{StatusCode: 200}, nil
	}

	// DeleteItem elimina el item exacto identificado por PK+SK.
	// Si el item no existe (ya fue borrado por TTL o nunca se creó), DynamoDB
	// no retorna error — simplemente hace un no-op. Eso es lo correcto acá.
	_, err = dbClient.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(internaldynamo.TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: internaldynamo.ConnectionPK(connectionID)},
			"SK": &types.AttributeValueMemberS{Value: internaldynamo.ConnectionSK()},
		},
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to delete connection",
			slog.String("connectionId", connectionID),
			slog.String("error", err.Error()),
		)
		// Retornamos 200 de todas formas — no podemos hacer nada útil aquí.
		// El TTL limpiará el item eventualmente.
	} else {
		logger.InfoContext(ctx, "connection deleted",
			slog.String("connectionId", connectionID),
		)
	}

	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handler)
}

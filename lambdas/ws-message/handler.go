// Lambda ws-message: maneja el evento $default de API Gateway WebSocket.
//
// Esta Lambda es el "router de mensajes". API GW la llama con CUALQUIER mensaje
// que envíe el cliente. Nosotros leemos el campo "action" y encolamos el mensaje
// en la cola SQS correcta para procesamiento asíncrono.
//
// ¿Por qué SQS y no llamar directamente a room-manager?
// Lambda tiene un límite de 29s para responder a API GW. Si room-manager tarda
// (ej. DynamoDB lento), el cliente vería un error. Con SQS, ws-message responde
// 200 inmediatamente y room-manager procesa cuando puede. Es más resiliente.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	wsevents "github.com/quizarena/internal/events"
)

// sqsQueues mapea cada acción a su URL de cola SQS.
// Las URLs vienen de variables de entorno inyectadas por Terraform.
//
// CONCEPTO GO — map[KeyType]ValueType:
// Similar a dict en Python. La diferencia: el tipo de clave y valor
// se declara en la definición. No puedes mezclar tipos.
var sqsQueues map[wsevents.Action]string

var logger *slog.Logger
var sqsClient *sqs.Client

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

	sqsQueues = map[wsevents.Action]string{
		wsevents.ActionCreateRoom:   os.Getenv("SQS_ROOM_MANAGER_URL"),
		wsevents.ActionJoinRoom:     os.Getenv("SQS_ROOM_MANAGER_URL"),
		wsevents.ActionLeaveRoom:    os.Getenv("SQS_ROOM_MANAGER_URL"),
		wsevents.ActionStartGame:    os.Getenv("SQS_QUIZ_ENGINE_URL"), // M3: quiz-engine maneja el inicio
		wsevents.ActionSubmitAnswer: os.Getenv("SQS_QUIZ_ENGINE_URL"),
	}
}

// sqsMessage es lo que enviamos a SQS — el mensaje original + metadata de la conexión.
type sqsMessage struct {
	ConnectionID string          `json:"connectionId"`
	Action       wsevents.Action `json:"action"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}

func handler(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connectionID := req.RequestContext.ConnectionID

	// Parsear el envelope del mensaje para leer el "action".
	// CONCEPTO GO — json.RawMessage:
	// Parsea solo el campo "action" sin deserializar el payload completo.
	// El payload queda como []byte crudo para que room-manager lo procese.
	// En Python equivaldría a: data = json.loads(body); action = data["action"]
	var msg wsevents.InboundMessage
	if err := json.Unmarshal([]byte(req.Body), &msg); err != nil {
		logger.WarnContext(ctx, "invalid message format",
			slog.String("connectionId", connectionID),
			slog.String("body", req.Body),
		)
		return events.APIGatewayProxyResponse{StatusCode: 400}, nil
	}

	// Buscar la cola SQS para esta acción.
	queueURL, ok := sqsQueues[msg.Action]
	if !ok || queueURL == "" {
		logger.WarnContext(ctx, "unknown action",
			slog.String("action", string(msg.Action)),
			slog.String("connectionId", connectionID),
		)
		return events.APIGatewayProxyResponse{StatusCode: 400}, nil
	}

	// Construir el mensaje SQS con connectionId + acción + payload original.
	outMsg := sqsMessage{
		ConnectionID: connectionID,
		Action:       msg.Action,
		Payload:      msg.Payload,
	}

	body, err := json.Marshal(outMsg)
	if err != nil {
		logger.ErrorContext(ctx, "marshal sqs message", slog.String("error", err.Error()))
		return events.APIGatewayProxyResponse{StatusCode: 500}, nil
	}

	// Inicializar el cliente SQS si aún no existe (lazy init).
	if sqsClient == nil {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "load aws config", slog.String("error", err.Error()))
			return events.APIGatewayProxyResponse{StatusCode: 500}, nil
		}
		sqsClient = sqs.NewFromConfig(cfg)
	}

	// Enviar a SQS FIFO.
	// MessageGroupId garantiza orden FIFO dentro de un grupo.
	// Usamos connectionId para que los mensajes de un mismo cliente sean ordenados.
	_, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(queueURL),
		MessageBody:            aws.String(string(body)),
		MessageGroupId:         aws.String(connectionID),
		MessageDeduplicationId: aws.String(fmt.Sprintf("%s-%s", connectionID, req.RequestContext.RequestID)),
	})
	if err != nil {
		logger.ErrorContext(ctx, "send sqs message",
			slog.String("action", string(msg.Action)),
			slog.String("error", err.Error()),
		)
		return events.APIGatewayProxyResponse{StatusCode: 500}, nil
	}

	logger.InfoContext(ctx, "message routed",
		slog.String("connectionId", connectionID),
		slog.String("action", string(msg.Action)),
	)

	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handler)
}

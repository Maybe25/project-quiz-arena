// Lambda broadcaster: recibe un mensaje de SQS y lo envía a todos los
// jugadores de una sala usando PostToConnection en paralelo.
//
// Patrón: Fan-out con goroutines + WaitGroup
//
// ¿Por qué una Lambda separada para esto?
// room-manager sabe quién está en la sala pero no debería encargarse
// del envío masivo — eso escala mal. broadcaster es un worker especializado
// que solo hace una cosa: PostToConnection × N jugadores.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	internaldynamo "github.com/quizarena/internal/dynamo"
	"github.com/quizarena/internal/wsapi"
)

var logger *slog.Logger
var dbClient *dynamodb.Client
var wsPoster wsapi.Poster

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// broadcastRequest es lo que room-manager nos envía por SQS cuando
// quiere que le mandemos un mensaje a toda una sala.
type broadcastRequest struct {
	RoomID  string          `json:"roomId"`
	Message json.RawMessage `json:"message"` // ya serializado, lo enviamos tal cual
}

func handler(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	if err := ensureClients(ctx); err != nil {
		return events.SQSEventResponse{}, err
	}

	var failures []events.SQSBatchItemFailure

	for _, record := range sqsEvent.Records {
		if err := broadcast(ctx, record); err != nil {
			logger.ErrorContext(ctx, "broadcast failed",
				slog.String("messageId", record.MessageId),
				slog.String("error", err.Error()),
			)
			failures = append(failures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func broadcast(ctx context.Context, record events.SQSMessage) error {
	var req broadcastRequest
	if err := json.Unmarshal([]byte(record.Body), &req); err != nil {
		return fmt.Errorf("unmarshal broadcast request: %w", err)
	}

	// Leer todos los jugadores de la sala.
	players, err := internaldynamo.ListPlayersInRoom(ctx, dbClient, req.RoomID)
	if err != nil {
		return fmt.Errorf("list players: %w", err)
	}

	if len(players) == 0 {
		return nil
	}

	// CONCEPTO GO — Fan-out con goroutines y WaitGroup:
	//
	// En Python harías: asyncio.gather(*[send(p) for p in players])
	// En Go, goroutines son más livianas que threads (2KB de stack inicial).
	// Puedes tener miles sin problema.
	//
	// sync.WaitGroup funciona como un contador:
	//   wg.Add(n)  → "espero n goroutines"
	//   wg.Done()  → "esta goroutine terminó" (decrementa el contador)
	//   wg.Wait()  → bloquea hasta que el contador llegue a 0

	var wg sync.WaitGroup

	// errCh es un channel para recolectar errores de las goroutines.
	// CONCEPTO GO — channels (chan):
	// Un channel es un tubo por el que las goroutines se comunican.
	// make(chan T, capacidad) crea un channel con buffer.
	// Con buffer = len(players), cada goroutine puede escribir sin bloquear.
	errCh := make(chan error, len(players))

	for _, player := range players {
		wg.Add(1)

		// CONCEPTO GO — closure en goroutine:
		// Pasamos 'player' como argumento para evitar el bug clásico de
		// closures en loops: sin el argumento, todas las goroutines
		// capturarían la MISMA variable 'player' (la del último loop).
		// En Python tienes el mismo problema con lambda en loops.
		go func(p internaldynamo.PlayerRecord) {
			defer wg.Done() // CONCEPTO GO — defer: se ejecuta cuando la función retorna

			if err := wsPoster.PostMessage(ctx, p.ConnectionID, json.RawMessage(req.Message)); err != nil {
				logger.WarnContext(ctx, "post to connection failed",
					slog.String("connectionId", p.ConnectionID),
					slog.String("playerId", p.PlayerID),
					slog.String("error", err.Error()),
				)
				errCh <- err
			}
		}(player)
	}

	// Esperar a que todas las goroutines terminen.
	wg.Wait()
	close(errCh) // cerrar el channel para poder iterar sobre él

	// Recopilar errores.
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	// Si todos fallaron, es un error real. Si solo algunos fallaron,
	// probablemente son conexiones muertas — lo logueamos y seguimos.
	if len(errs) == len(players) {
		return fmt.Errorf("all %d PostToConnection calls failed", len(players))
	}

	if len(errs) > 0 {
		logger.WarnContext(ctx, "some PostToConnection calls failed",
			slog.Int("failed", len(errs)),
			slog.Int("total", len(players)),
		)
	}

	return nil
}

func ensureClients(ctx context.Context) error {
	if dbClient != nil {
		return nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}

	dbClient = dynamodb.NewFromConfig(cfg)

	poster, err := wsapi.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("ws client: %w", err)
	}
	wsPoster = poster

	return nil
}

func main() {
	lambda.Start(handler)
}

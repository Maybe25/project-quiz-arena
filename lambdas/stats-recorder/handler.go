// Lambda stats-recorder: actualiza estadísticas globales al terminar una partida.
//
// Trigger: EventBridge event "game.ended" publicado por round-ender.
//
// CONCEPTO GO — EventBridge vs SQS como trigger:
// EventBridge usa events.CloudWatchEvent (mismo tipo que CloudWatch Events).
// El campo Detail contiene el JSON que publicó round-ender.
// A diferencia de SQS, no hay batch — un evento = una invocación.
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
)

var (
	logger   *slog.Logger
	dbClient *dynamodb.Client
	initOnce sync.Once
	initErr  error
)

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// gameEndedDetail es el payload del evento "game.ended" que publica round-ender.
type gameEndedDetail struct {
	RoomID  string         `json:"roomId"`
	Players []playerResult `json:"players"`
}

type playerResult struct {
	PlayerID string `json:"playerId"`
	Username string `json:"username"`
	Score    int    `json:"score"`
	IsWinner bool   `json:"isWinner"`
}

func handler(ctx context.Context, event events.CloudWatchEvent) error {
	if err := ensureClients(ctx); err != nil {
		return fmt.Errorf("init clients: %w", err)
	}

	// Deserializar el detail del evento EventBridge.
	var detail gameEndedDetail
	if err := json.Unmarshal(event.Detail, &detail); err != nil {
		return fmt.Errorf("unmarshal detail: %w", err)
	}

	logger.InfoContext(ctx, "recording game stats",
		slog.String("roomId", detail.RoomID),
		slog.Int("players", len(detail.Players)),
	)

	// Actualizar stats de cada jugador en paralelo.
	var wg sync.WaitGroup
	errs := make(chan error, len(detail.Players))

	for _, p := range detail.Players {
		wg.Add(1)
		go func(pr playerResult) {
			defer wg.Done()
			if err := internaldynamo.UpdatePlayerStats(ctx, dbClient, pr.PlayerID, pr.Username, pr.Score, pr.IsWinner); err != nil {
				errs <- fmt.Errorf("update stats for %s: %w", pr.PlayerID, err)
			}
		}(p)
	}
	wg.Wait()
	close(errs)

	// Recolectar errores — si alguno falló, retornamos error para que EventBridge reintente.
	for err := range errs {
		if err != nil {
			return err
		}
	}

	logger.InfoContext(ctx, "stats recorded", slog.String("roomId", detail.RoomID))
	return nil
}

func ensureClients(ctx context.Context) error {
	initOnce.Do(func() {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			initErr = err
			return
		}
		dbClient = dynamodb.NewFromConfig(cfg)
	})
	return initErr
}

func main() {
	lambda.Start(handler)
}

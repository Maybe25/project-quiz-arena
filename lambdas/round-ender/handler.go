// Lambda round-ender: invocada por Step Functions cuando expira el timer de una ronda.
//
// CONCEPTO GO — handler sin eventos SQS:
// Esta Lambda NO viene de SQS. Step Functions la invoca directamente como un Task.
// El "evento" de entrada es el estado JSON del state machine (sfnState).
// La función devuelve un nuevo sfnState que Step Functions usa para decidir
// si continuar el loop (más rondas) o terminar (última ronda).
//
// Flujo:
//   1. Lee respuestas de la ronda que acaba de terminar
//   2. Calcula puntos por jugador (basado en velocidad de respuesta)
//   3. Actualiza scores en DynamoDB
//   4. Broadcast ROUND_END (con respuesta correcta y scores actualizados)
//   5a. Si hay más rondas: guarda la siguiente ronda, broadcast ROUND_START,
//       retorna { hasMoreRounds: true, currentRound: N+1 }
//   5b. Si es la última: actualiza sala a "finished", broadcast GAME_END,
//       retorna { hasMoreRounds: false }
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	internaldynamo "github.com/quizarena/internal/dynamo"
	wsevents "github.com/quizarena/internal/events"
	"github.com/quizarena/internal/wsapi"
)

var (
	logger   *slog.Logger
	dbClient *dynamodb.Client
	wsPoster wsapi.Poster
	initOnce sync.Once
	initErr  error
)

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// sfnState es el estado que circula dentro del Step Functions state machine.
// Step Functions pasa este struct como input a round-ender, y round-ender
// lo devuelve (posiblemente modificado) para que el Choice state decida qué hacer.
//
// CONCEPTO GO — struct como contrato de API:
// En Python usarías un dict. En Go el struct actúa como contrato tipado:
// si falta un campo o el tipo no coincide, el compilador o el runtime lo detecta.
type sfnState struct {
	RoomID               string `json:"roomId"`
	CurrentRound         int    `json:"currentRound"`
	TotalRounds          int    `json:"totalRounds"`
	RoundDurationSeconds int    `json:"roundDurationSeconds"`
	HasMoreRounds        bool   `json:"hasMoreRounds"`
}

// handler es la función principal. Step Functions espera que devuelva el nuevo estado.
func handler(ctx context.Context, state sfnState) (sfnState, error) {
	if err := ensureClients(ctx); err != nil {
		return sfnState{}, fmt.Errorf("init clients: %w", err)
	}

	logger.InfoContext(ctx, "round ending",
		slog.String("roomId", state.RoomID),
		slog.Int("round", state.CurrentRound),
		slog.Int("total", state.TotalRounds),
	)

	// Paso 1: leer los metadatos de la ronda que acaba de terminar.
	round, err := internaldynamo.GetRound(ctx, dbClient, state.RoomID, state.CurrentRound)
	if err != nil {
		return sfnState{}, fmt.Errorf("get round: %w", err)
	}
	if round == nil {
		return sfnState{}, fmt.Errorf("round %d not found in room %s", state.CurrentRound, state.RoomID)
	}

	// Paso 2: leer todas las respuestas enviadas para esta ronda.
	answers, err := internaldynamo.GetRoundAnswers(ctx, dbClient, state.RoomID, state.CurrentRound)
	if err != nil {
		return sfnState{}, fmt.Errorf("get answers: %w", err)
	}

	// Paso 3: calcular y persistir puntos por jugador.
	for _, ans := range answers {
		isCorrect := ans.Answer == round.CorrectAnswer
		points := internaldynamo.CalcPoints(isCorrect, ans.AnsweredAt, round.StartedAt, round.TimeLimitMs)
		if points > 0 {
			if err := internaldynamo.AddPlayerScore(ctx, dbClient, state.RoomID, ans.PlayerID, points); err != nil {
				// Log el error pero continúa — no queremos abortar la ronda por un jugador.
				logger.ErrorContext(ctx, "failed to add score",
					slog.String("playerId", ans.PlayerID),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	// Paso 4: leer scores actualizados para incluirlos en ROUND_END.
	players, err := internaldynamo.ListPlayersInRoom(ctx, dbClient, state.RoomID)
	if err != nil {
		return sfnState{}, fmt.Errorf("list players: %w", err)
	}

	playerInfos := toPlayerInfos(players)

	// Paso 5: broadcast ROUND_END con respuesta correcta y scores.
	roundEndMsg := wsevents.OutboundMessage{
		Type: wsevents.TypeRoundEnd,
		Payload: wsevents.RoundEndPayload{
			RoundNumber:   state.CurrentRound,
			CorrectAnswer: round.CorrectAnswer,
			Scores:        playerInfos,
		},
	}
	broadcastToAll(ctx, players, roundEndMsg)

	// Paso 6: ¿hay más rondas?
	if state.CurrentRound >= state.TotalRounds {
		// --- Última ronda: terminar el juego ---
		if err := internaldynamo.UpdateRoomStatus(ctx, dbClient, state.RoomID, "finished"); err != nil {
			logger.ErrorContext(ctx, "update room to finished", slog.String("error", err.Error()))
		}

		gameEndMsg := wsevents.OutboundMessage{
			Type: wsevents.TypeGameEnd,
			Payload: wsevents.GameEndPayload{
				Scores: playerInfos,
			},
		}
		broadcastToAll(ctx, players, gameEndMsg)

		logger.InfoContext(ctx, "game finished", slog.String("roomId", state.RoomID))

		// Step Functions: hasMoreRounds=false → el Choice state va a GameComplete (Succeed).
		return sfnState{HasMoreRounds: false}, nil
	}

	// --- Hay más rondas: preparar la siguiente ---
	nextRound := state.CurrentRound + 1

	nextRoundData, err := internaldynamo.GetRound(ctx, dbClient, state.RoomID, nextRound)
	if err != nil {
		return sfnState{}, fmt.Errorf("get next round: %w", err)
	}
	if nextRoundData == nil {
		return sfnState{}, fmt.Errorf("next round %d not found", nextRound)
	}

	// Broadcast ROUND_START con la siguiente pregunta (sin correctAnswer).
	roundStartMsg := wsevents.OutboundMessage{
		Type: wsevents.TypeRoundStart,
		Payload: wsevents.RoundStartPayload{
			RoundNumber: nextRound,
			RoundID:     fmt.Sprintf("ROUND#%03d", nextRound),
			Question: wsevents.QuestionInfo{
				QuestionID: nextRoundData.QuestionID,
				Text:       nextRoundData.Question,
				Options:    nextRoundData.Options,
			},
			TimeLimitMs: nextRoundData.TimeLimitMs,
		},
	}
	broadcastToAll(ctx, players, roundStartMsg)

	logger.InfoContext(ctx, "next round started",
		slog.String("roomId", state.RoomID),
		slog.Int("round", nextRound),
	)

	// Step Functions: hasMoreRounds=true → el Choice state vuelve a WaitForRound.
	// currentRound actualizado para que el próximo ciclo use el número correcto.
	return sfnState{
		RoomID:               state.RoomID,
		CurrentRound:         nextRound,
		TotalRounds:          state.TotalRounds,
		RoundDurationSeconds: nextRoundData.TimeLimitMs / 1000,
		HasMoreRounds:        true,
	}, nil
}

// broadcastToAll envía en paralelo a todos los jugadores.
func broadcastToAll(ctx context.Context, players []internaldynamo.PlayerRecord, msg wsevents.OutboundMessage) {
	var wg sync.WaitGroup
	for _, p := range players {
		wg.Add(1)
		go func(player internaldynamo.PlayerRecord) {
			defer wg.Done()
			if err := wsPoster.PostMessage(ctx, player.ConnectionID, msg); err != nil {
				logger.WarnContext(ctx, "broadcast failed",
					slog.String("connectionId", player.ConnectionID),
					slog.String("error", err.Error()),
				)
			}
		}(p)
	}
	wg.Wait()
}

// toPlayerInfos convierte []PlayerRecord a []PlayerInfo para los mensajes WS.
func toPlayerInfos(records []internaldynamo.PlayerRecord) []wsevents.PlayerInfo {
	infos := make([]wsevents.PlayerInfo, len(records))
	for i, r := range records {
		infos[i] = wsevents.PlayerInfo{
			PlayerID: r.PlayerID,
			Username: r.Username,
			Score:    r.Score,
		}
	}
	return infos
}

func ensureClients(ctx context.Context) error {
	initOnce.Do(func() {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			initErr = err
			return
		}
		dbClient = dynamodb.NewFromConfig(cfg)

		poster, err := wsapi.NewClient(ctx)
		if err != nil {
			initErr = fmt.Errorf("ws client: %w", err)
			return
		}
		wsPoster = poster
	})
	return initErr
}

func main() {
	lambda.Start(handler)
}

// Lambda quiz-engine: maneja START_GAME y SUBMIT_ANSWER desde SQS.
//
// START_GAME:
//   1. Verifica que quien llama es el host y la sala está en "waiting"
//   2. Carga las preguntas del bucket S3
//   3. Guarda la primera ronda en DynamoDB
//   4. Actualiza room.status = "playing"
//   5. Broadcast GAME_STARTING + ROUND_START a todos los jugadores
//   6. Inicia la ejecución de Step Functions (el timer de rondas)
//
// SUBMIT_ANSWER:
//   1. Guarda la respuesta del jugador en DynamoDB (idempotente)
//   2. El score se calcula cuando expira el timer (round-ender)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sfn"

	internaldynamo "github.com/quizarena/internal/dynamo"
	wsevents "github.com/quizarena/internal/events"
	"github.com/quizarena/internal/wsapi"
)

var (
	logger   *slog.Logger
	dbClient *dynamodb.Client
	s3Client *s3.Client
	sfnClient *sfn.Client
	wsPoster wsapi.Poster
	initOnce sync.Once
	initErr  error
)

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// sqsBody es el envelope que ws-message envía a esta cola SQS.
type sqsBody struct {
	ConnectionID string          `json:"connectionId"`
	Action       wsevents.Action `json:"action"`
	Payload      json.RawMessage `json:"payload"`
}

// s3Question es la estructura de cada pregunta en el JSON de S3.
type s3Question struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	Correct     int      `json:"correct"`
	TimeLimitMs int      `json:"timeLimitMs"`
}

// sfnInput es el estado inicial que pasamos a Step Functions.
// Step Functions lo usa como "memoria" entre estados — lo recibe el round-ender
// y lo devuelve modificado para que el loop sepa en qué ronda va.
type sfnInput struct {
	RoomID               string `json:"roomId"`
	CurrentRound         int    `json:"currentRound"`
	TotalRounds          int    `json:"totalRounds"`
	RoundDurationSeconds int    `json:"roundDurationSeconds"`
}

func handler(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	if err := ensureClients(ctx); err != nil {
		return events.SQSEventResponse{}, fmt.Errorf("init clients: %w", err)
	}

	var failures []events.SQSBatchItemFailure
	for _, record := range sqsEvent.Records {
		if err := processRecord(ctx, record); err != nil {
			logger.ErrorContext(ctx, "failed to process record",
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

func processRecord(ctx context.Context, record events.SQSMessage) error {
	var msg sqsBody
	if err := json.Unmarshal([]byte(record.Body), &msg); err != nil {
		return fmt.Errorf("unmarshal sqs body: %w", err)
	}

	switch msg.Action {
	case wsevents.ActionStartGame:
		return handleStartGame(ctx, msg)
	case wsevents.ActionSubmitAnswer:
		return handleSubmitAnswer(ctx, msg)
	default:
		logger.WarnContext(ctx, "unhandled action", slog.String("action", string(msg.Action)))
		return nil
	}
}

// handleStartGame inicia una partida: carga preguntas, guarda rondas, arranca el timer.
func handleStartGame(ctx context.Context, msg sqsBody) error {
	// Paso 1: obtener roomId del registro de conexión.
	conn, err := internaldynamo.GetConnection(ctx, dbClient, msg.ConnectionID)
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}
	if conn == nil || conn.RoomID == "" {
		return sendError(ctx, msg.ConnectionID, "NOT_IN_ROOM", "No estás en ninguna sala")
	}

	// Paso 2: verificar que quien llama es el host y la sala está en "waiting".
	room, err := internaldynamo.GetRoom(ctx, dbClient, conn.RoomID)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}
	if room == nil {
		return sendError(ctx, msg.ConnectionID, "ROOM_NOT_FOUND", "La sala no existe")
	}

	hostPlayerID := playerIDFromConn(msg.ConnectionID)
	if room.HostPlayerID != hostPlayerID {
		return sendError(ctx, msg.ConnectionID, "NOT_HOST", "Solo el host puede iniciar la partida")
	}
	if room.Status != "waiting" {
		return sendError(ctx, msg.ConnectionID, "GAME_ALREADY_STARTED", "La partida ya está en curso")
	}

	// Paso 3: cargar preguntas desde S3.
	questions, err := loadQuestionsFromS3(ctx)
	if err != nil {
		return fmt.Errorf("load questions: %w", err)
	}
	if len(questions) == 0 {
		return sendError(ctx, msg.ConnectionID, "NO_QUESTIONS", "No hay preguntas disponibles")
	}

	// Seleccionar las primeras 5 preguntas (o menos si hay pocas).
	totalRounds := 5
	if len(questions) < totalRounds {
		totalRounds = len(questions)
	}
	selected := questions[:totalRounds]

	// Paso 4: guardar todas las rondas en DynamoDB.
	for i, q := range selected {
		round := internaldynamo.RoundRecord{
			RoundNumber:   i + 1,
			TotalRounds:   totalRounds,
			QuestionID:    q.ID,
			Question:      q.Text,
			Options:       q.Options,
			CorrectAnswer: q.Correct,
			TimeLimitMs:   q.TimeLimitMs,
		}
		if err := internaldynamo.SaveRound(ctx, dbClient, conn.RoomID, round); err != nil {
			return fmt.Errorf("save round %d: %w", i+1, err)
		}
	}

	// Paso 5: actualizar status de la sala a "playing".
	if err := internaldynamo.UpdateRoomStatus(ctx, dbClient, conn.RoomID, "playing"); err != nil {
		return fmt.Errorf("update room status: %w", err)
	}

	// Paso 6: obtener jugadores para hacer broadcast.
	players, err := internaldynamo.ListPlayersInRoom(ctx, dbClient, conn.RoomID)
	if err != nil {
		return fmt.Errorf("list players: %w", err)
	}

	// Paso 7: broadcast GAME_STARTING a todos.
	gameStartingMsg := wsevents.OutboundMessage{Type: wsevents.TypeGameStarting}
	broadcastToAll(ctx, players, gameStartingMsg)

	// Paso 8: broadcast ROUND_START ronda 1 (sin revelar la respuesta correcta).
	firstQ := selected[0]
	roundStartMsg := wsevents.OutboundMessage{
		Type: wsevents.TypeRoundStart,
		Payload: wsevents.RoundStartPayload{
			RoundNumber: 1,
			RoundID:     fmt.Sprintf("ROUND#%03d", 1),
			Question: wsevents.QuestionInfo{
				QuestionID: firstQ.ID,
				Text:       firstQ.Text,
				Options:    firstQ.Options,
			},
			TimeLimitMs: firstQ.TimeLimitMs,
		},
	}
	broadcastToAll(ctx, players, roundStartMsg)

	// Paso 9: iniciar la ejecución de Step Functions.
	// Step Functions recibirá este JSON como estado inicial y lo pasará
	// a round-ender cuando expire el primer timer.
	sfnInputData := sfnInput{
		RoomID:               conn.RoomID,
		CurrentRound:         1,
		TotalRounds:          totalRounds,
		RoundDurationSeconds: firstQ.TimeLimitMs / 1000,
	}
	inputJSON, err := json.Marshal(sfnInputData)
	if err != nil {
		return fmt.Errorf("marshal sfn input: %w", err)
	}

	stateMachineArn := os.Getenv("SFN_ROUND_TIMER_ARN")
	executionName := fmt.Sprintf("%s-%d", conn.RoomID, time.Now().UnixMilli())

	_, err = sfnClient.StartExecution(ctx, &sfn.StartExecutionInput{
		StateMachineArn: aws.String(stateMachineArn),
		Name:            aws.String(executionName),
		Input:           aws.String(string(inputJSON)),
	})
	if err != nil {
		return fmt.Errorf("start sfn execution: %w", err)
	}

	logger.InfoContext(ctx, "game started",
		slog.String("roomId", conn.RoomID),
		slog.Int("totalRounds", totalRounds),
		slog.String("executionName", executionName),
	)
	return nil
}

// handleSubmitAnswer guarda la respuesta de un jugador.
// El score se calcula después cuando round-ender procesa el fin de ronda.
func handleSubmitAnswer(ctx context.Context, msg sqsBody) error {
	var payload wsevents.SubmitAnswerPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal submit payload: %w", err)
	}

	conn, err := internaldynamo.GetConnection(ctx, dbClient, msg.ConnectionID)
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}
	if conn == nil || conn.RoomID == "" {
		return nil // no está en sala, ignorar silenciosamente
	}

	playerID := playerIDFromConn(msg.ConnectionID)

	// Extraer el número de ronda del roundId (formato "ROUND#001").
	// CONCEPTO GO — Sscanf:
	// Similar a sscanf de C o re.match en Python. Extrae valores con formato.
	var roundNumber int
	if _, err := fmt.Sscanf(payload.RoundID, "ROUND#%d", &roundNumber); err != nil {
		return fmt.Errorf("parse roundId %q: %w", payload.RoundID, err)
	}

	answer := internaldynamo.AnswerRecord{
		PlayerID:    playerID,
		RoundNumber: roundNumber,
		Answer:      payload.Answer,
	}

	// SaveAnswer es idempotente: si el jugador ya respondió, no hace nada.
	if err := internaldynamo.SaveAnswer(ctx, dbClient, conn.RoomID, answer); err != nil {
		return fmt.Errorf("save answer: %w", err)
	}

	logger.InfoContext(ctx, "answer saved",
		slog.String("roomId", conn.RoomID),
		slog.String("playerId", playerID),
		slog.Int("round", roundNumber),
		slog.Int("answer", payload.Answer),
	)
	return nil
}

// loadQuestionsFromS3 descarga y parsea el JSON de preguntas del bucket S3.
func loadQuestionsFromS3(ctx context.Context) ([]s3Question, error) {
	bucket := os.Getenv("S3_QUESTIONS_BUCKET")
	key := os.Getenv("S3_QUESTIONS_KEY") // ej. "questions/general.json"

	out, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get object: %w", err)
	}
	defer out.Body.Close()

	// CONCEPTO GO — io.ReadAll:
	// Lee todo el contenido de un io.Reader a []byte.
	// Equivale a file.read() en Python.
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read s3 body: %w", err)
	}

	var questions []s3Question
	if err := json.Unmarshal(data, &questions); err != nil {
		return nil, fmt.Errorf("unmarshal questions: %w", err)
	}
	return questions, nil
}

// broadcastToAll envía un mensaje a todos los jugadores de la sala en paralelo.
// Usa el mismo patrón de goroutines + WaitGroup del broadcaster.
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

// sendError envía un mensaje de error al cliente vía WebSocket.
func sendError(ctx context.Context, connectionID, code, message string) error {
	return wsPoster.PostMessage(ctx, connectionID, wsevents.OutboundMessage{
		Type: wsevents.TypeError,
		Payload: wsevents.ErrorPayload{
			Code:    code,
			Message: message,
		},
	})
}

// playerIDFromConn genera un playerID temporal a partir del connectionId.
// Mismo patrón que room-manager — en M4 viene del JWT de Cognito.
func playerIDFromConn(connectionID string) string {
	return fmt.Sprintf("guest-%s", connectionID)
}

func ensureClients(ctx context.Context) error {
	initOnce.Do(func() {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			initErr = err
			return
		}
		dbClient = dynamodb.NewFromConfig(cfg)
		s3Client = s3.NewFromConfig(cfg)
		sfnClient = sfn.NewFromConfig(cfg)

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

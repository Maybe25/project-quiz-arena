// Lambda room-manager: procesa mensajes de sala desde SQS.
//
// Acciones que maneja:
//   CREATE_ROOM  → crea sala, agrega al host, responde con ROOM_CREATED
//   JOIN_ROOM    → agrega jugador, notifica a todos con PLAYER_JOINED
//   LEAVE_ROOM   → quita jugador, notifica a todos con PLAYER_LEFT
//
// Esta Lambda se dispara por SQS (no por API GW directamente).
// El evento es un events.SQSEvent con uno o más mensajes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	internaldynamo "github.com/quizarena/internal/dynamo"
	wsevents "github.com/quizarena/internal/events"
	"github.com/quizarena/internal/wsapi"
)

var logger *slog.Logger
var dbClient *dynamodb.Client
var sqsClient *sqs.Client
var wsPoster wsapi.Poster

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// sqsBody es la estructura del mensaje que ws-message nos envía por SQS.
type sqsBody struct {
	ConnectionID string          `json:"connectionId"`
	Action       wsevents.Action `json:"action"`
	Payload      json.RawMessage `json:"payload"`
}

// handler procesa un batch de mensajes SQS.
//
// CONCEPTO GO — events.SQSEvent:
// SQS puede enviar múltiples mensajes en un solo batch (hasta 10).
// Lambda los procesa todos en una invocación para ser eficiente.
// Si alguno falla, retornamos cuáles fallaron para que SQS los reintente.
func handler(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	// Inicializar clientes AWS la primera vez (warm start los reutiliza).
	if err := ensureClients(ctx); err != nil {
		return events.SQSEventResponse{}, fmt.Errorf("init clients: %w", err)
	}

	// CONCEPTO GO — SQSEventResponse con BatchItemFailures:
	// En vez de fallar todo el batch si un mensaje falla, reportamos
	// solo los que fallaron. SQS reintenta solo esos.
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

	// CONCEPTO GO — switch sin condición:
	// Equivale a switch(true) en otros lenguajes.
	// Cada case es una expresión booleana. Es idiomático en Go para
	// reemplazar cadenas de if/else if.
	switch msg.Action {
	case wsevents.ActionCreateRoom:
		return handleCreateRoom(ctx, msg)
	case wsevents.ActionJoinRoom:
		return handleJoinRoom(ctx, msg)
	case wsevents.ActionLeaveRoom:
		return handleLeaveRoom(ctx, msg)
	default:
		logger.WarnContext(ctx, "unhandled action", slog.String("action", string(msg.Action)))
		return nil // no es un error, simplemente ignoramos
	}
}

// handleCreateRoom crea una sala nueva y responde al creador con ROOM_CREATED.
func handleCreateRoom(ctx context.Context, msg sqsBody) error {
	roomID := newID("room")
	roomCode := newRoomCode()

	room := internaldynamo.RoomRecord{
		RoomID:       roomID,
		RoomCode:     roomCode,
		Status:       "waiting",
		HostPlayerID: playerIDFromConn(msg.ConnectionID),
		MaxPlayers:   8,
	}

	if err := internaldynamo.SaveRoom(ctx, dbClient, room); err != nil {
		return fmt.Errorf("save room: %w", err)
	}

	// Agregar al creador como primer jugador.
	player := internaldynamo.PlayerRecord{
		PlayerID:     playerIDFromConn(msg.ConnectionID),
		ConnectionID: msg.ConnectionID,
		Username:     fmt.Sprintf("Player-%s", msg.ConnectionID[:6]),
		Score:        0,
		IsReady:      false,
	}
	if err := internaldynamo.SavePlayerInRoom(ctx, dbClient, roomID, player); err != nil {
		return fmt.Errorf("save player: %w", err)
	}

	// Actualizar la conexión para que sepa en qué sala está.
	if err := internaldynamo.UpdateConnectionRoom(ctx, dbClient, msg.ConnectionID, roomID, player.PlayerID); err != nil {
		return fmt.Errorf("update connection: %w", err)
	}

	// Enviar ROOM_CREATED solo al creador.
	return wsPoster.PostMessage(ctx, msg.ConnectionID, wsevents.OutboundMessage{
		Type: wsevents.TypeRoomCreated,
		Payload: wsevents.RoomJoinedPayload{
			Room: wsevents.RoomInfo{
				RoomID:       room.RoomID,
				RoomCode:     room.RoomCode,
				Status:       room.Status,
				HostPlayerID: room.HostPlayerID,
			},
			Players: []wsevents.PlayerInfo{{
				PlayerID: player.PlayerID,
				Username: player.Username,
				Score:    0,
				IsReady:  false,
			}},
		},
	})
}

// handleJoinRoom agrega un jugador a una sala existente y notifica a todos.
func handleJoinRoom(ctx context.Context, msg sqsBody) error {
	var payload wsevents.JoinRoomPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal join payload: %w", err)
	}

	// Buscar la sala por roomCode usando el lookup inverso ROOMCODE#<code>.
	room, err := internaldynamo.GetRoomByCode(ctx, dbClient, payload.RoomCode)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}
	if room == nil {
		return wsPoster.PostMessage(ctx, msg.ConnectionID, wsevents.OutboundMessage{
			Type: wsevents.TypeError,
			Payload: wsevents.ErrorPayload{
				Code:    "ROOM_NOT_FOUND",
				Message: "La sala no existe",
			},
		})
	}
	if room.Status != "waiting" {
		return wsPoster.PostMessage(ctx, msg.ConnectionID, wsevents.OutboundMessage{
			Type: wsevents.TypeError,
			Payload: wsevents.ErrorPayload{
				Code:    "ROOM_NOT_AVAILABLE",
				Message: "La sala ya está en juego",
			},
		})
	}

	newPlayer := internaldynamo.PlayerRecord{
		PlayerID:     playerIDFromConn(msg.ConnectionID),
		ConnectionID: msg.ConnectionID,
		Username:     fmt.Sprintf("Player-%s", msg.ConnectionID[:6]),
		Score:        0,
		IsReady:      false,
	}
	if err := internaldynamo.SavePlayerInRoom(ctx, dbClient, room.RoomID, newPlayer); err != nil {
		return fmt.Errorf("save player: %w", err)
	}
	if err := internaldynamo.UpdateConnectionRoom(ctx, dbClient, msg.ConnectionID, room.RoomID, newPlayer.PlayerID); err != nil {
		return fmt.Errorf("update connection: %w", err)
	}

	// Leer todos los jugadores para enviar la lista completa al que se unió.
	players, err := internaldynamo.ListPlayersInRoom(ctx, dbClient, room.RoomID)
	if err != nil {
		return fmt.Errorf("list players: %w", err)
	}

	playerInfos := toPlayerInfos(players)

	// Enviar ROOM_JOINED al jugador que se unió (con la lista completa).
	if err := wsPoster.PostMessage(ctx, msg.ConnectionID, wsevents.OutboundMessage{
		Type: wsevents.TypeRoomJoined,
		Payload: wsevents.RoomJoinedPayload{
			Room: wsevents.RoomInfo{
				RoomID:       room.RoomID,
				RoomCode:     room.RoomCode,
				Status:       room.Status,
				HostPlayerID: room.HostPlayerID,
			},
			Players: playerInfos,
		},
	}); err != nil {
		return err
	}

	// Notificar a los demás jugadores con PLAYER_JOINED.
	// CONCEPTO GO — goroutines con fan-out:
	// Por ahora lo hacemos secuencialmente. En M3 lo paralelizamos con goroutines.
	joinedPayload := wsevents.OutboundMessage{
		Type: wsevents.TypePlayerJoined,
		Payload: wsevents.PlayerJoinedPayload{
			Player: wsevents.PlayerInfo{
				PlayerID: newPlayer.PlayerID,
				Username: newPlayer.Username,
			},
		},
	}

	for _, p := range players {
		if p.ConnectionID == msg.ConnectionID {
			continue // no notificarse a sí mismo
		}
		if err := wsPoster.PostMessage(ctx, p.ConnectionID, joinedPayload); err != nil {
			// Log el error pero continúa — no queremos fallar todo por una conexión muerta.
			logger.WarnContext(ctx, "failed to notify player",
				slog.String("targetConnection", p.ConnectionID),
				slog.String("error", err.Error()),
			)
		}
	}

	return nil
}

// handleLeaveRoom elimina al jugador de la sala y notifica a los demás.
func handleLeaveRoom(ctx context.Context, msg sqsBody) error {
	conn, err := internaldynamo.GetConnection(ctx, dbClient, msg.ConnectionID)
	if err != nil || conn == nil || conn.RoomID == "" {
		return nil // ya no está en ninguna sala, nada que hacer
	}

	playerID := playerIDFromConn(msg.ConnectionID)
	if err := internaldynamo.RemovePlayerFromRoom(ctx, dbClient, conn.RoomID, playerID); err != nil {
		return fmt.Errorf("remove player: %w", err)
	}

	// Notificar a los jugadores restantes.
	players, err := internaldynamo.ListPlayersInRoom(ctx, dbClient, conn.RoomID)
	if err != nil {
		return fmt.Errorf("list players: %w", err)
	}

	leftPayload := wsevents.OutboundMessage{
		Type:    wsevents.TypePlayerLeft,
		Payload: map[string]string{"playerId": playerID},
	}
	for _, p := range players {
		if err := wsPoster.PostMessage(ctx, p.ConnectionID, leftPayload); err != nil {
			logger.WarnContext(ctx, "failed to notify player left",
				slog.String("targetConnection", p.ConnectionID),
				slog.String("error", err.Error()),
			)
		}
	}

	return nil
}

// --- Helpers ---

// ensureClients inicializa los clientes AWS una sola vez.
// CONCEPTO GO — inicialización lazy con nil check:
// En Lambda, init() corre en cold start. Los clientes se reutilizan
// en warm starts. Este patrón es más flexible que init() cuando
// necesitamos el contexto para inicializar.
func ensureClients(ctx context.Context) error {
	if dbClient != nil {
		return nil // ya inicializado
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}

	dbClient = dynamodb.NewFromConfig(cfg)
	sqsClient = sqs.NewFromConfig(cfg)

	poster, err := wsapi.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("ws client: %w", err)
	}
	wsPoster = poster

	return nil
}

// newID genera un ID simple basado en timestamp + random.
// En producción usaríamos UUID (M3+).
func newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), rand.Intn(9999))
}

// newRoomCode genera un código de sala de 6 letras mayúsculas.
func newRoomCode() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	code := make([]byte, 6)
	for i := range code {
		code[i] = letters[rand.Intn(len(letters))]
	}
	return string(code)
}

// playerIDFromConn genera un playerID temporal a partir del connectionId.
// En M4 esto viene del JWT de Cognito.
func playerIDFromConn(connectionID string) string {
	return fmt.Sprintf("guest-%s", connectionID)
}

// toPlayerInfos convierte []PlayerRecord a []PlayerInfo para los mensajes WS.
func toPlayerInfos(records []internaldynamo.PlayerRecord) []wsevents.PlayerInfo {
	infos := make([]wsevents.PlayerInfo, len(records))
	for i, r := range records {
		infos[i] = wsevents.PlayerInfo{
			PlayerID: r.PlayerID,
			Username: r.Username,
			Score:    r.Score,
			IsReady:  r.IsReady,
		}
	}
	return infos
}

func main() {
	lambda.Start(handler)
}

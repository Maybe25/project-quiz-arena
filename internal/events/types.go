// Package events define el contrato del protocolo WebSocket de QuizArena.
//
// Mensajes INBOUND (cliente → servidor): tienen campo "action"
// Mensajes OUTBOUND (servidor → cliente): tienen campo "type"
//
// Ejemplo de flujo:
//   cliente envía:  {"action": "JOIN_ROOM", "roomCode": "QUIZ42"}
//   servidor envía: {"type": "ROOM_JOINED", "room": {...}, "players": [...]}
package events

import "encoding/json"

// --- Tipos de acciones inbound (lo que el cliente envía) ---

type Action string

const (
	ActionCreateRoom    Action = "CREATE_ROOM"
	ActionJoinRoom      Action = "JOIN_ROOM"
	ActionLeaveRoom     Action = "LEAVE_ROOM"
	ActionStartGame     Action = "START_GAME"
	ActionSubmitAnswer  Action = "SUBMIT_ANSWER"
)

// --- Tipos de mensajes outbound (lo que el servidor envía) ---

type MessageType string

const (
	TypeRoomCreated   MessageType = "ROOM_CREATED"
	TypeRoomJoined    MessageType = "ROOM_JOINED"
	TypePlayerJoined  MessageType = "PLAYER_JOINED"
	TypePlayerLeft    MessageType = "PLAYER_LEFT"
	TypeGameStarting  MessageType = "GAME_STARTING"
	TypeRoundStart    MessageType = "ROUND_START"
	TypeRoundEnd      MessageType = "ROUND_END"
	TypeGameEnd       MessageType = "GAME_END"
	TypeError         MessageType = "ERROR"
)

// --- Structs inbound ---

// InboundMessage es el envelope de todo mensaje que llega del cliente.
// El campo Action determina qué hacer con el resto del payload.
type InboundMessage struct {
	Action  Action          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"` // el resto del mensaje, sin parsear
}

// JoinRoomPayload es el payload de JOIN_ROOM.
type JoinRoomPayload struct {
	RoomCode string `json:"roomCode"`
}

// SubmitAnswerPayload es el payload de SUBMIT_ANSWER.
type SubmitAnswerPayload struct {
	Answer  int    `json:"answer"`  // índice de la opción elegida (0-3)
	RoundID string `json:"roundId"` // para detectar respuestas tardías
}

// --- Structs outbound ---

// OutboundMessage es el envelope de todo mensaje que el servidor envía.
type OutboundMessage struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// ErrorPayload se envía cuando algo falla.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PlayerInfo representa un jugador en la sala (para listar en ROOM_JOINED, etc.)
type PlayerInfo struct {
	PlayerID string `json:"playerId"`
	Username string `json:"username"`
	Score    int    `json:"score"`
	IsReady  bool   `json:"isReady"`
}

// RoomInfo representa los metadatos de una sala.
type RoomInfo struct {
	RoomID       string `json:"roomId"`
	RoomCode     string `json:"roomCode"`
	Status       string `json:"status"`
	HostPlayerID string `json:"hostPlayerId"`
}

// RoomJoinedPayload es el payload de ROOM_JOINED.
type RoomJoinedPayload struct {
	Room    RoomInfo     `json:"room"`
	Players []PlayerInfo `json:"players"`
}

// PlayerJoinedPayload notifica a los demás jugadores que alguien entró.
type PlayerJoinedPayload struct {
	Player PlayerInfo `json:"player"`
}

// QuestionInfo representa una pregunta del quiz (sin revelar la respuesta correcta).
type QuestionInfo struct {
	QuestionID string   `json:"questionId"`
	Text       string   `json:"text"`
	Options    []string `json:"options"`
}

// RoundStartPayload se envía al iniciar cada ronda.
type RoundStartPayload struct {
	RoundNumber int          `json:"roundNumber"`
	RoundID     string       `json:"roundId"`
	Question    QuestionInfo `json:"question"`
	TimeLimitMs int          `json:"timeLimitMs"` // tiempo en milisegundos
}

// RoundEndPayload se envía al terminar cada ronda.
type RoundEndPayload struct {
	RoundNumber   int          `json:"roundNumber"`
	CorrectAnswer int          `json:"correctAnswer"`
	Scores        []PlayerInfo `json:"scores"`
}

// GameEndPayload se envía cuando termina la partida completa (podio final).
type GameEndPayload struct {
	Scores []PlayerInfo `json:"scores"`
}

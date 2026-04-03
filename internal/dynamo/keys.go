// Package dynamo contiene los helpers para interactuar con DynamoDB.
// Usamos diseño single-table: una sola tabla con PK+SK para todos los tipos de datos.
package dynamo

import "fmt"

// Las constantes definen los prefijos de las claves.
// Ejemplo: "CONNECTION#abc123" es la PK de una conexión WebSocket.
const (
	prefixConnection = "CONNECTION"
	prefixRoom       = "ROOM"
	prefixRoomCode   = "ROOMCODE"
	prefixPlayer     = "PLAYER"
	prefixQuestion   = "QUESTION"
	prefixRound      = "ROUND"
	prefixAnswer     = "ANSWER"

	skMetadata = "METADATA"
)

// --- Claves para conexiones WebSocket ---

// ConnectionPK retorna la PK para una conexión: "CONNECTION#<connId>"
func ConnectionPK(connectionID string) string {
	return fmt.Sprintf("%s#%s", prefixConnection, connectionID)
}

// ConnectionSK retorna el SK para los metadatos de una conexión.
func ConnectionSK() string {
	return skMetadata
}

// --- Claves para salas (rooms) ---

// RoomPK retorna la PK para una sala: "ROOM#<roomId>"
func RoomPK(roomID string) string {
	return fmt.Sprintf("%s#%s", prefixRoom, roomID)
}

// RoomCodePK retorna la PK del índice inverso roomCode→roomId: "ROOMCODE#<code>"
func RoomCodePK(roomCode string) string {
	return fmt.Sprintf("%s#%s", prefixRoomCode, roomCode)
}

// RoomMetadataSK retorna el SK para los metadatos de la sala.
func RoomMetadataSK() string {
	return skMetadata
}

// RoomPlayerSK retorna el SK para un jugador dentro de una sala: "PLAYER#<playerId>"
func RoomPlayerSK(playerID string) string {
	return fmt.Sprintf("%s#%s", prefixPlayer, playerID)
}

// RoomRoundSK retorna el SK para una ronda: "ROUND#<n>"
// Usamos fmt.Sprintf con %03d para que "ROUND#001" < "ROUND#002" (orden lexicográfico correcto).
func RoomRoundSK(roundNumber int) string {
	return fmt.Sprintf("%s#%03d", prefixRound, roundNumber)
}

// RoomAnswerSK retorna el SK para la respuesta de un jugador en una ronda: "ROUND#<n>#ANSWER#<playerId>"
func RoomAnswerSK(roundNumber int, playerID string) string {
	return fmt.Sprintf("%s#%03d#%s#%s", prefixRound, roundNumber, prefixAnswer, playerID)
}

// --- Claves para jugadores ---

// PlayerPK retorna la PK para un jugador: "PLAYER#<playerId>"
func PlayerPK(playerID string) string {
	return fmt.Sprintf("%s#%s", prefixPlayer, playerID)
}

// PlayerProfileSK retorna el SK para el perfil de un jugador.
func PlayerProfileSK() string {
	return skMetadata
}

// --- Claves para preguntas ---

// QuestionPK retorna la PK para una pregunta: "QUESTION#<id>"
func QuestionPK(questionID string) string {
	return fmt.Sprintf("%s#%s", prefixQuestion, questionID)
}

// QuestionSK retorna el SK para los metadatos de una pregunta.
func QuestionSK() string {
	return skMetadata
}

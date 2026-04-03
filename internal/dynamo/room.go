// room.go contiene las operaciones DynamoDB relacionadas con salas y jugadores.
package dynamo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// RoomRecord son los metadatos de una sala guardados en DynamoDB.
type RoomRecord struct {
	PK           string `dynamodbav:"PK"`
	SK           string `dynamodbav:"SK"`
	RoomID       string `dynamodbav:"roomId"`
	RoomCode     string `dynamodbav:"roomCode"`
	Status       string `dynamodbav:"status"`   // "waiting" | "playing" | "finished"
	HostPlayerID string `dynamodbav:"hostPlayerId"`
	MaxPlayers   int    `dynamodbav:"maxPlayers"`
}

// PlayerRecord representa un jugador dentro de una sala.
type PlayerRecord struct {
	PK           string `dynamodbav:"PK"`
	SK           string `dynamodbav:"SK"`
	PlayerID     string `dynamodbav:"playerId"`
	ConnectionID string `dynamodbav:"connectionId"`
	Username     string `dynamodbav:"username"`
	Score        int    `dynamodbav:"score"`
	IsReady      bool   `dynamodbav:"isReady"`
}

// ConnectionRecord es el registro de una conexión WebSocket activa.
type ConnectionRecord struct {
	PK           string `dynamodbav:"PK"`
	SK           string `dynamodbav:"SK"`
	PlayerID     string `dynamodbav:"playerId"`
	RoomID       string `dynamodbav:"roomId"`
	ConnectionID string `dynamodbav:"connectionId"`
}

// roomCodeIndex es el item de lookup inverso roomCode → roomId.
type roomCodeIndex struct {
	PK     string `dynamodbav:"PK"`
	SK     string `dynamodbav:"SK"`
	RoomID string `dynamodbav:"roomId"`
}

// SaveRoom escribe los metadatos de una sala en DynamoDB.
// También guarda un item de lookup inverso ROOMCODE#<code> → roomId
// para poder buscar salas por código sin necesitar un GSI.
func SaveRoom(ctx context.Context, client *dynamodb.Client, room RoomRecord) error {
	room.PK = RoomPK(room.RoomID)
	room.SK = RoomMetadataSK()

	item, err := attributevalue.MarshalMap(room)
	if err != nil {
		return fmt.Errorf("marshal room: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableName()),
		Item:      item,
	})
	if err != nil {
		return err
	}

	// Guardar el lookup inverso: ROOMCODE#<code> → roomId
	idx := roomCodeIndex{
		PK:     RoomCodePK(room.RoomCode),
		SK:     RoomMetadataSK(),
		RoomID: room.RoomID,
	}
	idxItem, err := attributevalue.MarshalMap(idx)
	if err != nil {
		return fmt.Errorf("marshal room code index: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableName()),
		Item:      idxItem,
	})
	return err
}

// GetRoomByCode busca una sala por su roomCode (ej. "ABCXYZ").
// Primero resuelve el código a roomId usando el lookup inverso,
// luego carga los metadatos completos de la sala.
func GetRoomByCode(ctx context.Context, client *dynamodb.Client, roomCode string) (*RoomRecord, error) {
	// Paso 1: resolver roomCode → roomId
	out, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: RoomCodePK(roomCode)},
			"SK": &types.AttributeValueMemberS{Value: RoomMetadataSK()},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get room code index: %w", err)
	}
	if out.Item == nil {
		return nil, nil // código no existe
	}

	var idx roomCodeIndex
	if err := attributevalue.UnmarshalMap(out.Item, &idx); err != nil {
		return nil, fmt.Errorf("unmarshal room code index: %w", err)
	}

	// Paso 2: cargar la sala completa por roomId
	return GetRoom(ctx, client, idx.RoomID)
}

// SavePlayerInRoom agrega un jugador a una sala.
func SavePlayerInRoom(ctx context.Context, client *dynamodb.Client, roomID string, player PlayerRecord) error {
	player.PK = RoomPK(roomID)
	player.SK = RoomPlayerSK(player.PlayerID)

	item, err := attributevalue.MarshalMap(player)
	if err != nil {
		return fmt.Errorf("marshal player: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableName()),
		Item:      item,
	})
	return err
}

// RemovePlayerFromRoom elimina a un jugador de una sala.
func RemovePlayerFromRoom(ctx context.Context, client *dynamodb.Client, roomID, playerID string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: RoomPK(roomID)},
			"SK": &types.AttributeValueMemberS{Value: RoomPlayerSK(playerID)},
		},
	})
	return err
}

// GetRoom lee los metadatos de una sala.
func GetRoom(ctx context.Context, client *dynamodb.Client, roomID string) (*RoomRecord, error) {
	out, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: RoomPK(roomID)},
			"SK": &types.AttributeValueMemberS{Value: RoomMetadataSK()},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get room: %w", err)
	}
	if out.Item == nil {
		return nil, nil // sala no existe
	}

	var room RoomRecord
	if err := attributevalue.UnmarshalMap(out.Item, &room); err != nil {
		return nil, fmt.Errorf("unmarshal room: %w", err)
	}
	return &room, nil
}

// ListPlayersInRoom retorna todos los jugadores de una sala.
//
// CONCEPTO GO — Query con KeyConditionExpression:
// En DynamoDB, Query es eficiente: usa el índice para buscar todos los items
// con PK = "ROOM#<id>" cuyo SK empieza con "PLAYER#".
// Esto es O(jugadores), no O(tabla completa) como un Scan.
func ListPlayersInRoom(ctx context.Context, client *dynamodb.Client, roomID string) ([]PlayerRecord, error) {
	out, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(TableName()),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: RoomPK(roomID)},
			":prefix": &types.AttributeValueMemberS{Value: prefixPlayer + "#"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query players: %w", err)
	}

	// CONCEPTO GO — make([]Type, 0, capacidad):
	// Crea un slice vacío pero con capacidad pre-reservada.
	// Evita re-allocaciones cuando agregamos items. Equivale a list con capacidad en Python.
	players := make([]PlayerRecord, 0, len(out.Items))
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &players); err != nil {
		return nil, fmt.Errorf("unmarshal players: %w", err)
	}
	return players, nil
}

// UpdateConnectionRoom actualiza el registro de conexión para asociarlo a una sala.
func UpdateConnectionRoom(ctx context.Context, client *dynamodb.Client, connectionID, roomID, playerID string) error {
	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: ConnectionPK(connectionID)},
			"SK": &types.AttributeValueMemberS{Value: ConnectionSK()},
		},
		UpdateExpression: aws.String("SET roomId = :roomId, connectionId = :connId"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":roomId": &types.AttributeValueMemberS{Value: roomID},
			":connId": &types.AttributeValueMemberS{Value: connectionID},
		},
	})
	return err
}

// GetConnection lee el registro de una conexión activa.
func GetConnection(ctx context.Context, client *dynamodb.Client, connectionID string) (*ConnectionRecord, error) {
	out, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: ConnectionPK(connectionID)},
			"SK": &types.AttributeValueMemberS{Value: ConnectionSK()},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	var conn ConnectionRecord
	if err := attributevalue.UnmarshalMap(out.Item, &conn); err != nil {
		return nil, fmt.Errorf("unmarshal connection: %w", err)
	}
	return &conn, nil
}

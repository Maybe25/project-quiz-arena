// game.go contiene las operaciones DynamoDB para el estado del juego en curso.
//
// Patrones de keys usados:
//   ROOM#<roomId>  ROUND#001                 → metadatos de la ronda (pregunta, respuesta correcta)
//   ROOM#<roomId>  ROUND#001#ANSWER#<pid>    → respuesta de un jugador en esa ronda
package dynamo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// RoundRecord almacena una ronda en DynamoDB.
// CorrectAnswer se guarda server-side y NUNCA se envía al cliente hasta ROUND_END.
type RoundRecord struct {
	PK            string   `dynamodbav:"PK"`
	SK            string   `dynamodbav:"SK"`
	RoundNumber   int      `dynamodbav:"roundNumber"`
	TotalRounds   int      `dynamodbav:"totalRounds"`
	QuestionID    string   `dynamodbav:"questionId"`
	Question      string   `dynamodbav:"question"`
	Options       []string `dynamodbav:"options"`
	CorrectAnswer int      `dynamodbav:"correctAnswer"` // 0-3, solo server-side
	TimeLimitMs   int      `dynamodbav:"timeLimitMs"`
	StartedAt     int64    `dynamodbav:"startedAt"` // Unix ms, para calcular velocidad de respuesta
}

// AnswerRecord almacena la respuesta de un jugador a una ronda.
type AnswerRecord struct {
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	PlayerID    string `dynamodbav:"playerId"`
	RoundNumber int    `dynamodbav:"roundNumber"`
	Answer      int    `dynamodbav:"answer"`     // índice 0-3 de la opción elegida
	AnsweredAt  int64  `dynamodbav:"answeredAt"` // Unix ms, para scoring por velocidad
}

// SaveRound guarda los metadatos de una ronda en DynamoDB.
func SaveRound(ctx context.Context, client *dynamodb.Client, roomID string, round RoundRecord) error {
	round.PK = RoomPK(roomID)
	round.SK = RoomRoundSK(round.RoundNumber)
	round.StartedAt = time.Now().UnixMilli()

	item, err := attributevalue.MarshalMap(round)
	if err != nil {
		return fmt.Errorf("marshal round: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableName()),
		Item:      item,
	})
	return err
}

// GetRound lee los metadatos de una ronda específica.
func GetRound(ctx context.Context, client *dynamodb.Client, roomID string, roundNumber int) (*RoundRecord, error) {
	out, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: RoomPK(roomID)},
			"SK": &types.AttributeValueMemberS{Value: RoomRoundSK(roundNumber)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get round: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	var round RoundRecord
	if err := attributevalue.UnmarshalMap(out.Item, &round); err != nil {
		return nil, fmt.Errorf("unmarshal round: %w", err)
	}
	return &round, nil
}

// SaveAnswer guarda la respuesta de un jugador usando una condición de escritura única.
//
// CONCEPTO GO — ConditionExpression:
// "attribute_not_exists(SK)" significa: solo escribe si este item NO existe todavía.
// Si el jugador ya respondió, DynamoDB devuelve ConditionalCheckFailedException.
// Esto garantiza idempotencia: el primer SUBMIT_ANSWER gana, los duplicados se ignoran.
func SaveAnswer(ctx context.Context, client *dynamodb.Client, roomID string, answer AnswerRecord) error {
	answer.PK = RoomPK(roomID)
	answer.SK = RoomAnswerSK(answer.RoundNumber, answer.PlayerID)
	answer.AnsweredAt = time.Now().UnixMilli()

	item, err := attributevalue.MarshalMap(answer)
	if err != nil {
		return fmt.Errorf("marshal answer: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(TableName()),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(SK)"),
	})
	if err != nil {
		// Si el jugador ya respondió, no es un error — simplemente ignoramos.
		// CONCEPTO GO — errors.As:
		// En Python harías: except ConditionalCheckFailedException.
		// En Go, errors.As recorre la cadena de errores wrapeados hasta encontrar
		// el tipo concreto. Es type-safe: el compilador valida que sea un puntero.
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return nil // ya respondió antes, idempotente
		}
		return fmt.Errorf("save answer: %w", err)
	}
	return nil
}

// GetRoundAnswers obtiene todas las respuestas enviadas para una ronda.
//
// Usa Query con begins_with(SK, "ROUND#001#ANSWER#") para traer solo las respuestas
// de esa ronda sin necesidad de un GSI adicional.
func GetRoundAnswers(ctx context.Context, client *dynamodb.Client, roomID string, roundNumber int) ([]AnswerRecord, error) {
	skPrefix := fmt.Sprintf("%s#%03d#%s#", prefixRound, roundNumber, prefixAnswer)

	out, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(TableName()),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: RoomPK(roomID)},
			":prefix": &types.AttributeValueMemberS{Value: skPrefix},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query answers: %w", err)
	}

	answers := make([]AnswerRecord, 0, len(out.Items))
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &answers); err != nil {
		return nil, fmt.Errorf("unmarshal answers: %w", err)
	}
	return answers, nil
}

// UpdateRoomStatus actualiza el campo "status" de una sala (waiting → playing → finished).
func UpdateRoomStatus(ctx context.Context, client *dynamodb.Client, roomID, status string) error {
	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: RoomPK(roomID)},
			"SK": &types.AttributeValueMemberS{Value: RoomMetadataSK()},
		},
		UpdateExpression: aws.String("SET #s = :status"),
		// CONCEPTO GO — ExpressionAttributeNames:
		// "status" es palabra reservada de DynamoDB. El alias "#s" evita el conflicto.
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status": &types.AttributeValueMemberS{Value: status},
		},
	})
	return err
}

// AddPlayerScore suma puntos al score de un jugador de forma atómica.
//
// Usa ADD en vez de SET: si dos Lambdas intentan sumar puntos al mismo jugador
// simultáneamente, DynamoDB garantiza que ningún incremento se pierde.
// Con SET, la segunda escritura sobreescribiría el resultado de la primera.
func AddPlayerScore(ctx context.Context, client *dynamodb.Client, roomID, playerID string, points int) error {
	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: RoomPK(roomID)},
			"SK": &types.AttributeValueMemberS{Value: RoomPlayerSK(playerID)},
		},
		UpdateExpression: aws.String("ADD score :pts"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pts": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", points)},
		},
	})
	return err
}

// CalcPoints calcula los puntos ganados por responder correctamente.
//
// Sistema de puntuación:
//   - Respuesta incorrecta → 0 puntos
//   - Respuesta correcta instantánea → 1000 puntos
//   - Respuesta correcta al límite de tiempo → 500 puntos mínimo
//   - Entre medias: interpolación lineal
func CalcPoints(isCorrect bool, answeredAtMs, roundStartedAtMs int64, timeLimitMs int) int {
	if !isCorrect {
		return 0
	}

	elapsedMs := answeredAtMs - roundStartedAtMs
	if elapsedMs < 0 {
		elapsedMs = 0
	}

	const maxPoints = 1000
	const minPoints = 500

	ratio := float64(elapsedMs) / float64(timeLimitMs)
	if ratio > 1.0 {
		ratio = 1.0
	}
	return maxPoints - int(ratio*float64(maxPoints-minPoints))
}

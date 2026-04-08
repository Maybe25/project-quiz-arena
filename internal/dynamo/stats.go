// stats.go — estadísticas globales de jugadores (lifetime, cross-game).
//
// Patrón DynamoDB para leaderboard:
//   PK = PLAYER#<playerId>  SK = STATS
//   GSI1PK = "LEADERBOARD"  totalScore = <Number>
//
// El GSI "leaderboard-index" indexa GSI1PK + totalScore, permitiendo:
//   Query GSI1PK = "LEADERBOARD" ORDER BY totalScore DESC LIMIT 10
// sin necesitar Scan sobre toda la tabla.
package dynamo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const leaderboardPartition = "LEADERBOARD"

// PlayerStats son las estadísticas de vida del jugador (todas las partidas).
type PlayerStats struct {
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	GSI1PK      string `dynamodbav:"GSI1PK"`      // siempre "LEADERBOARD"
	PlayerID    string `dynamodbav:"playerId"`
	Username    string `dynamodbav:"username"`
	TotalScore  int    `dynamodbav:"totalScore"`  // sort key del GSI
	GamesPlayed int    `dynamodbav:"gamesPlayed"`
	Wins        int    `dynamodbav:"wins"`
}

// UpdatePlayerStats suma el score de la partida terminada a las stats globales del jugador.
// Usa ADD atómico para que múltiples actualizaciones concurrentes no se pisen.
func UpdatePlayerStats(ctx context.Context, client *dynamodb.Client, playerID, username string, score int, isWinner bool) error {
	winsAdd := 0
	if isWinner {
		winsAdd = 1
	}

	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(TableName()),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: PlayerPK(playerID)},
			"SK": &types.AttributeValueMemberS{Value: "STATS"},
		},
		// SET inicializa los campos que no existen en el primer update.
		// ADD suma a los campos numéricos existentes (o los crea en 0 si no existen).
		// CONCEPTO GO — UpdateExpression mixto SET+ADD:
		// DynamoDB permite combinar SET y ADD en un solo UpdateExpression.
		UpdateExpression: aws.String("SET GSI1PK = :gsi1pk, playerId = :pid, username = :uname ADD totalScore :score, gamesPlayed :one, wins :wins"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gsi1pk": &types.AttributeValueMemberS{Value: leaderboardPartition},
			":pid":    &types.AttributeValueMemberS{Value: playerID},
			":uname":  &types.AttributeValueMemberS{Value: username},
			":score":  &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", score)},
			":one":    &types.AttributeValueMemberN{Value: "1"},
			":wins":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", winsAdd)},
		},
	})
	return err
}

// GetTopPlayers retorna los N jugadores con más puntos totales usando el GSI.
func GetTopPlayers(ctx context.Context, client *dynamodb.Client, limit int) ([]PlayerStats, error) {
	out, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(TableName()),
		IndexName:              aws.String("leaderboard-index"),
		KeyConditionExpression: aws.String("GSI1PK = :gsi1pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gsi1pk": &types.AttributeValueMemberS{Value: leaderboardPartition},
		},
		// ScanIndexForward=false → orden descendente por totalScore (el más alto primero).
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(int32(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("query leaderboard: %w", err)
	}

	stats := make([]PlayerStats, 0, len(out.Items))
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &stats); err != nil {
		return nil, fmt.Errorf("unmarshal stats: %w", err)
	}
	return stats, nil
}

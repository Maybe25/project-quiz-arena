from boto3.dynamodb.conditions import Key
from .keys import table_name, player_pk

LEADERBOARD_PARTITION = "LEADERBOARD"


def update_player_stats(db, player_id, username, score, is_winner):
    """Actualiza stats globales con ADD atómico para evitar race conditions."""
    table = db.Table(table_name())
    table.update_item(
        Key={"PK": player_pk(player_id), "SK": "STATS"},
        UpdateExpression=(
            "SET GSI1PK = :gsi1pk, playerId = :pid, username = :uname "
            "ADD totalScore :score, gamesPlayed :one, wins :wins"
        ),
        ExpressionAttributeValues={
            ":gsi1pk": LEADERBOARD_PARTITION,
            ":pid":    player_id,
            ":uname":  username,
            ":score":  score,
            ":one":    1,
            ":wins":   1 if is_winner else 0,
        },
    )


def get_top_players(db, limit=10):
    """Retorna los N jugadores con más puntos usando el GSI leaderboard-index."""
    table = db.Table(table_name())
    resp = table.query(
        IndexName="leaderboard-index",
        KeyConditionExpression=Key("GSI1PK").eq(LEADERBOARD_PARTITION),
        ScanIndexForward=False,  # DESC por totalScore
        Limit=limit,
    )
    # Convertir Decimal a int para serialización JSON
    players = []
    for item in resp.get("Items", []):
        players.append({
            "playerId":    item.get("playerId", ""),
            "username":    item.get("username", ""),
            "totalScore":  int(item.get("totalScore", 0)),
            "gamesPlayed": int(item.get("gamesPlayed", 0)),
            "wins":        int(item.get("wins", 0)),
        })
    return players

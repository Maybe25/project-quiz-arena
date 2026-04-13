import time
from boto3.dynamodb.conditions import Key
from botocore.exceptions import ClientError
from .keys import table_name, room_pk, room_round_sk, room_answer_sk, room_ready_sk, room_round_ended_sk


def save_round(db, room_id, round_data):
    table = db.Table(table_name())
    table.put_item(Item={
        "PK":            room_pk(room_id),
        "SK":            room_round_sk(round_data["roundNumber"]),
        "roundNumber":   round_data["roundNumber"],
        "totalRounds":   round_data["totalRounds"],
        "questionId":    round_data["questionId"],
        "question":      round_data["question"],
        "options":       round_data["options"],
        "correctAnswer": round_data["correctAnswer"],
        "timeLimitMs":   round_data["timeLimitMs"],
        "startedAt":     int(time.time() * 1000),  # Unix ms
    })


def get_round(db, room_id, round_number):
    table = db.Table(table_name())
    resp = table.get_item(Key={
        "PK": room_pk(room_id),
        "SK": room_round_sk(round_number),
    })
    return resp.get("Item")


def save_answer(db, room_id, answer):
    """Guarda la respuesta de un jugador — idempotente: la primera gana."""
    table = db.Table(table_name())
    try:
        table.put_item(
            Item={
                "PK":          room_pk(room_id),
                "SK":          room_answer_sk(answer["roundNumber"], answer["playerId"]),
                "playerId":    answer["playerId"],
                "roundNumber": answer["roundNumber"],
                "answer":      answer["answer"],
                "answeredAt":  int(time.time() * 1000),
            },
            ConditionExpression="attribute_not_exists(SK)",
        )
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            return  # ya respondió, ignorar
        raise


def get_round_answers(db, room_id, round_number):
    """Retorna todas las respuestas de una ronda."""
    table = db.Table(table_name())
    prefix = f"ROUND#{round_number:03d}#ANSWER#"
    resp = table.query(
        KeyConditionExpression=Key("PK").eq(room_pk(room_id)) & Key("SK").begins_with(prefix)
    )
    return resp.get("Items", [])


def update_room_status(db, room_id, status):
    table = db.Table(table_name())
    table.update_item(
        Key={"PK": room_pk(room_id), "SK": "METADATA"},
        UpdateExpression="SET #s = :status",
        ExpressionAttributeNames={"#s": "status"},  # 'status' es palabra reservada
        ExpressionAttributeValues={":status": status},
    )


def add_player_score(db, room_id, player_id, points):
    """Suma puntos de forma atómica con ADD para evitar race conditions."""
    table = db.Table(table_name())
    table.update_item(
        Key={"PK": room_pk(room_id), "SK": f"PLAYER#{player_id}"},
        UpdateExpression="ADD score :pts",
        ExpressionAttributeValues={":pts": points},
    )


def mark_player_ready(db, room_id, round_number, player_id):
    """Registra que un jugador marcó Listo — idempotente."""
    table = db.Table(table_name())
    try:
        table.put_item(
            Item={
                "PK":          room_pk(room_id),
                "SK":          room_ready_sk(round_number, player_id),
                "playerId":    player_id,
                "roundNumber": round_number,
                "markedAt":    int(time.time() * 1000),
            },
            ConditionExpression="attribute_not_exists(SK)",
        )
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            return  # ya marcó listo, ignorar
        raise


def get_ready_count(db, room_id, round_number):
    """Retorna cuántos jugadores han marcado Listo en esta ronda."""
    table = db.Table(table_name())
    prefix = f"ROUND#{round_number:03d}#READY#"
    resp = table.query(
        KeyConditionExpression=Key("PK").eq(room_pk(room_id)) & Key("SK").begins_with(prefix),
        Select="COUNT",
    )
    return resp.get("Count", 0)


def try_mark_round_ended(db, room_id, round_number):
    """
    Intenta marcar la ronda como terminada de forma atómica.
    Retorna True si esta llamada fue la primera (puede proceder).
    Retorna False si la ronda ya fue terminada (evitar doble ROUND_END).
    """
    table = db.Table(table_name())
    try:
        table.put_item(
            Item={
                "PK":       room_pk(room_id),
                "SK":       room_round_ended_sk(round_number),
                "endedAt":  int(time.time() * 1000),
            },
            ConditionExpression="attribute_not_exists(SK)",
        )
        return True
    except ClientError as e:
        if e.response["Error"]["Code"] == "ConditionalCheckFailedException":
            return False  # ya terminó, ignorar
        raise


def calc_points(is_correct, answered_at_ms, round_started_at_ms, time_limit_ms):
    """Puntuación basada en velocidad: 500 mínimo, 1000 máximo si es correcta."""
    if not is_correct:
        return 0
    elapsed = max(0, answered_at_ms - round_started_at_ms)
    ratio = min(1.0, elapsed / time_limit_ms)
    return int(1000 - ratio * 500)

import json
import logging
import os
import random
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
import boto3

from shared.events.types import (
    ACTION_START_GAME, ACTION_SUBMIT_ANSWER, ACTION_PLAYER_READY, ACTION_GET_LEADERBOARD,
    TYPE_GAME_STARTING, TYPE_ROUND_START, TYPE_PLAYERS_READY, TYPE_LEADERBOARD, TYPE_ERROR,
    outbound, error_payload,
)
from shared.dynamo import room as dynamo_room
from shared.dynamo import game as dynamo_game
from shared.dynamo import stats as dynamo_stats
from shared.wsapi import post as wsapi

logger = logging.getLogger()
logger.setLevel(logging.INFO)

_db = None
_s3 = None
_sfn = None
_ws = None


def get_clients():
    global _db, _s3, _sfn, _ws
    if _db is None:
        _db  = boto3.resource("dynamodb")
        _s3  = boto3.client("s3")
        _sfn = boto3.client("stepfunctions")
        _ws  = wsapi.new_client()
    return _db, _s3, _sfn, _ws


def player_id_from_conn(connection_id):
    return f"guest-{connection_id}"


def broadcast_to_all(ws, players, message):
    if not players:
        return
    with ThreadPoolExecutor(max_workers=max(len(players), 1)) as executor:
        futures = {executor.submit(wsapi.post_message, ws, p["connectionId"], message): p for p in players}
        for future in as_completed(futures):
            try:
                future.result()
            except Exception as e:
                p = futures[future]
                logger.warning(json.dumps({"event": "broadcast-failed", "connectionId": p["connectionId"], "error": str(e)}))


def send_error(ws, connection_id, code, message):
    wsapi.post_message(ws, connection_id, outbound(TYPE_ERROR, error_payload(code, message)))


def load_questions_from_s3(s3):
    bucket = os.environ["S3_QUESTIONS_BUCKET"]
    key    = os.environ["S3_QUESTIONS_KEY"]
    resp   = s3.get_object(Bucket=bucket, Key=key)
    return json.loads(resp["Body"].read())


def handle_start_game(db, s3, sfn, ws, msg):
    conn = dynamo_room.get_connection(db, msg["connectionId"])
    if not conn or not conn.get("roomId"):
        send_error(ws, msg["connectionId"], "NOT_IN_ROOM", "No estás en ninguna sala")
        return

    room = dynamo_room.get_room(db, conn["roomId"])
    if not room:
        send_error(ws, msg["connectionId"], "ROOM_NOT_FOUND", "La sala no existe")
        return

    host_player_id = player_id_from_conn(msg["connectionId"])
    if room["hostPlayerId"] != host_player_id:
        send_error(ws, msg["connectionId"], "NOT_HOST", "Solo el host puede iniciar la partida")
        return
    if room["status"] != "waiting":
        send_error(ws, msg["connectionId"], "GAME_ALREADY_STARTED", "La partida ya está en curso")
        return

    questions = load_questions_from_s3(s3)
    if not questions:
        send_error(ws, msg["connectionId"], "NO_QUESTIONS", "No hay preguntas disponibles")
        return

    total_rounds = min(5, len(questions))
    selected     = random.sample(questions, total_rounds)  # orden aleatorio cada partida

    for i, q in enumerate(selected):
        dynamo_game.save_round(db, conn["roomId"], {
            "roundNumber":   i + 1,
            "totalRounds":   total_rounds,
            "questionId":    q["id"],
            "question":      q["text"],
            "options":       q["options"],
            "correctAnswer": q["correct"],
            "timeLimitMs":   q["timeLimitMs"],
        })

    dynamo_game.update_room_status(db, conn["roomId"], "playing")
    players = dynamo_room.list_players_in_room(db, conn["roomId"])

    # ROUND_START se envía ANTES de GAME_STARTING para que el estado quede
    # cargado en el shell antes de que el lobby navegue a /game/play.
    # Sin este orden, el GameComponent puede ver currentRound()=null y redirigir al lobby.
    first_q = selected[0]
    broadcast_to_all(ws, players, outbound(TYPE_ROUND_START, {
        "roundNumber": 1,
        "roundId":     "ROUND#001",
        "totalRounds": total_rounds,
        "question":    {"questionId": first_q["id"], "text": first_q["text"], "options": first_q["options"]},
        "timeLimitMs": first_q["timeLimitMs"],
    }))

    broadcast_to_all(ws, players, outbound(TYPE_GAME_STARTING))

    execution_name = f"{conn['roomId']}-{int(time.time() * 1000)}"
    sfn.start_execution(
        stateMachineArn=os.environ["SFN_ROUND_TIMER_ARN"],
        name=execution_name,
        input=json.dumps({
            "roomId":               conn["roomId"],
            "currentRound":         1,
            "totalRounds":          total_rounds,
            "roundDurationSeconds": first_q["timeLimitMs"] // 1000,
        }),
    )
    logger.info(json.dumps({"event": "game-started", "roomId": conn["roomId"], "totalRounds": total_rounds}))


def handle_submit_answer(db, msg):
    payload = msg.get("payload") or {}
    conn = dynamo_room.get_connection(db, msg["connectionId"])
    if not conn or not conn.get("roomId"):
        return

    player_id    = player_id_from_conn(msg["connectionId"])
    round_id     = payload.get("roundId", "")
    round_number = int(round_id.replace("ROUND#", ""))

    dynamo_game.save_answer(db, conn["roomId"], {
        "playerId":    player_id,
        "roundNumber": round_number,
        "answer":      payload["answer"],
    })
    logger.info(json.dumps({"event": "answer-saved", "roomId": conn["roomId"], "playerId": player_id, "round": round_number}))


def handle_player_ready(db, ws, msg):
    """
    El jugador marcó Listo tras seleccionar respuesta.
    Si todos los jugadores están listos → invoca round-ender directamente
    sin esperar que expire el timer de Step Functions.
    """
    payload      = msg.get("payload") or {}
    conn         = dynamo_room.get_connection(db, msg["connectionId"])
    if not conn or not conn.get("roomId"):
        return

    room_id      = conn["roomId"]
    player_id    = player_id_from_conn(msg["connectionId"])
    round_number = int(payload.get("roundNumber", 0))
    if not round_number:
        return

    dynamo_game.mark_player_ready(db, room_id, round_number, player_id)

    players     = dynamo_room.list_players_in_room(db, room_id)
    total_count = len(players)
    ready_count = dynamo_game.get_ready_count(db, room_id, round_number)

    broadcast_to_all(ws, players, outbound(TYPE_PLAYERS_READY, {
        "readyCount":  ready_count,
        "totalCount":  total_count,
        "roundNumber": round_number,
    }))

    logger.info(json.dumps({
        "event": "player-ready", "roomId": room_id,
        "playerId": player_id, "readyCount": ready_count, "totalCount": total_count,
    }))

    if ready_count >= total_count:
        # Todos listos: invocar round-ender inmediatamente (bypass del timer SFN)
        round_data = dynamo_game.get_round(db, room_id, round_number)
        if not round_data:
            return
        lam = boto3.client("lambda")
        lam.invoke(
            FunctionName=os.environ["ROUND_ENDER_ARN"],
            InvocationType="Event",  # asíncrono — no bloquea
            Payload=json.dumps({
                "roomId":       room_id,
                "currentRound": round_number,
                "totalRounds":  int(round_data["totalRounds"]),
            }).encode(),
        )
        logger.info(json.dumps({"event": "early-round-end-triggered", "roomId": room_id, "round": round_number}))


def handle_get_leaderboard(db, ws, msg):
    top = dynamo_stats.get_top_players(db, 10)
    wsapi.post_message(ws, msg["connectionId"], outbound(TYPE_LEADERBOARD, {"entries": top}))


def process_record(db, s3, sfn, ws, record):
    msg    = json.loads(record["body"])
    action = msg.get("action", "")

    if action == ACTION_START_GAME:
        handle_start_game(db, s3, sfn, ws, msg)
    elif action == ACTION_SUBMIT_ANSWER:
        handle_submit_answer(db, msg)
    elif action == ACTION_PLAYER_READY:
        handle_player_ready(db, ws, msg)
    elif action == ACTION_GET_LEADERBOARD:
        handle_get_leaderboard(db, ws, msg)
    else:
        logger.warning(json.dumps({"event": "unhandled-action", "action": action}))


def handler(event, context):
    db, s3, sfn, ws = get_clients()
    failures = []

    for record in event.get("Records", []):
        try:
            process_record(db, s3, sfn, ws, record)
        except Exception as e:
            logger.error(json.dumps({"event": "record-failed", "messageId": record["messageId"], "error": str(e)}))
            failures.append({"itemIdentifier": record["messageId"]})

    return {"batchItemFailures": failures}

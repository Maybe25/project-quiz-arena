import json
import logging
import os
from concurrent.futures import ThreadPoolExecutor, as_completed
import boto3

from shared.events.types import (
    TYPE_ROUND_END, TYPE_ROUND_START, TYPE_GAME_END, outbound,
)
from shared.dynamo import room as dynamo_room
from shared.dynamo import game as dynamo_game
from shared.wsapi import post as wsapi

logger = logging.getLogger()
logger.setLevel(logging.INFO)

_db = None
_eb = None
_ws = None


def get_clients():
    global _db, _eb, _ws
    if _db is None:
        _db = boto3.resource("dynamodb")
        _eb = boto3.client("events")
        _ws = wsapi.new_client()
    return _db, _eb, _ws


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


def to_player_infos(players):
    return [{"playerId": p["playerId"], "username": p["username"], "score": int(p.get("score", 0))}
            for p in players]


def publish_game_ended(eb, room_id, players):
    max_score = max((int(p.get("score", 0)) for p in players), default=-1)
    results = [
        {"playerId": p["playerId"], "username": p["username"],
         "score": int(p.get("score", 0)), "isWinner": int(p.get("score", 0)) == max_score and max_score >= 0}
        for p in players
    ]
    detail = json.dumps({"roomId": room_id, "players": results})
    eb.put_events(Entries=[{
        "EventBusName": os.environ["EVENTBRIDGE_BUS_NAME"],
        "Source":       "quizarena",
        "DetailType":   "game.ended",
        "Detail":       detail,
    }])


def handler(event, context):
    """
    Invocada directamente por Step Functions.
    event  = el estado JSON del state machine (sfnState).
    return = nuevo sfnState que el Choice state evalúa.
    """
    db, eb, ws = get_clients()

    room_id       = event["roomId"]
    current_round = event["currentRound"]
    total_rounds  = event["totalRounds"]

    logger.info(json.dumps({"event": "round-ending", "roomId": room_id, "round": current_round, "total": total_rounds}))

    # Idempotency guard: si ya fue procesada (por el trigger temprano), salir sin hacer nada.
    if not dynamo_game.try_mark_round_ended(db, room_id, current_round):
        logger.info(json.dumps({"event": "round-already-ended", "roomId": room_id, "round": current_round}))
        # Devolver el estado que el Choice state necesita para continuar o terminar.
        if current_round >= total_rounds:
            return {"hasMoreRounds": False}
        next_round = current_round + 1
        next_data  = dynamo_game.get_round(db, room_id, next_round)
        if not next_data:
            return {"hasMoreRounds": False}
        return {
            "roomId":               room_id,
            "currentRound":         next_round,
            "totalRounds":          total_rounds,
            "roundDurationSeconds": int(next_data["timeLimitMs"]) // 1000,
            "hasMoreRounds":        True,
        }

    round_data = dynamo_game.get_round(db, room_id, current_round)
    if not round_data:
        raise RuntimeError(f"Ronda {current_round} no encontrada en sala {room_id}")

    answers = dynamo_game.get_round_answers(db, room_id, current_round)

    for ans in answers:
        is_correct = int(ans["answer"]) == int(round_data["correctAnswer"])
        points = dynamo_game.calc_points(
            is_correct,
            int(ans["answeredAt"]),
            int(round_data["startedAt"]),
            int(round_data["timeLimitMs"]),
        )
        if points > 0:
            try:
                dynamo_game.add_player_score(db, room_id, ans["playerId"], points)
            except Exception as e:
                logger.error(json.dumps({"event": "add-score-failed", "playerId": ans["playerId"], "error": str(e)}))

    players      = dynamo_room.list_players_in_room(db, room_id)
    player_infos = to_player_infos(players)

    broadcast_to_all(ws, players, outbound(TYPE_ROUND_END, {
        "roundNumber":   current_round,
        "correctAnswer": int(round_data["correctAnswer"]),
        "scores":        player_infos,
    }))

    if current_round >= total_rounds:
        # Última ronda: terminar el juego
        dynamo_game.update_room_status(db, room_id, "finished")
        broadcast_to_all(ws, players, outbound(TYPE_GAME_END, {"scores": player_infos}))
        logger.info(json.dumps({"event": "game-finished", "roomId": room_id}))

        try:
            publish_game_ended(eb, room_id, players)
        except Exception as e:
            logger.error(json.dumps({"event": "publish-game-ended-failed", "error": str(e)}))

        return {"hasMoreRounds": False}

    next_round = current_round + 1
    next_data  = dynamo_game.get_round(db, room_id, next_round)
    if not next_data:
        raise RuntimeError(f"Siguiente ronda {next_round} no encontrada")

    broadcast_to_all(ws, players, outbound(TYPE_ROUND_START, {
        "roundNumber": next_round,
        "roundId":     f"ROUND#{next_round:03d}",
        "question":    {"questionId": next_data["questionId"], "text": next_data["question"], "options": next_data["options"]},
        "timeLimitMs": int(next_data["timeLimitMs"]),
    }))

    logger.info(json.dumps({"event": "next-round-started", "roomId": room_id, "round": next_round}))

    return {
        "roomId":               room_id,
        "currentRound":         next_round,
        "totalRounds":          total_rounds,
        "roundDurationSeconds": int(next_data["timeLimitMs"]) // 1000,
        "hasMoreRounds":        True,
    }

import json
import logging
import random
import string
import time
import boto3

from shared.events.types import (
    ACTION_CREATE_ROOM, ACTION_JOIN_ROOM, ACTION_LEAVE_ROOM,
    TYPE_ROOM_CREATED, TYPE_ROOM_JOINED, TYPE_PLAYER_JOINED, TYPE_PLAYER_LEFT,
    TYPE_ERROR, outbound, error_payload,
)
from shared.dynamo import room as dynamo_room
from shared.wsapi import post as wsapi

logger = logging.getLogger()
logger.setLevel(logging.INFO)

_db = None
_ws = None


def get_clients():
    global _db, _ws
    if _db is None:
        _db = boto3.resource("dynamodb")
        _ws = wsapi.new_client()
    return _db, _ws


def new_id(prefix):
    return f"{prefix}-{int(time.time() * 1000)}-{random.randint(0, 9999)}"


def new_room_code():
    return "".join(random.choices(string.ascii_uppercase, k=6))


def player_id_from_conn(connection_id):
    return f"guest-{connection_id}"


def to_player_infos(players):
    return [{"playerId": p["playerId"], "username": p["username"],
             "score": int(p.get("score", 0)), "isReady": p.get("isReady", False)}
            for p in players]


def handle_create_room(db, ws, msg):
    room_id   = new_id("room")
    room_code = new_room_code()
    player_id = player_id_from_conn(msg["connectionId"])

    room = {"roomId": room_id, "roomCode": room_code, "status": "waiting",
            "hostPlayerId": player_id, "maxPlayers": 8}
    dynamo_room.save_room(db, room)

    player = {"playerId": player_id, "connectionId": msg["connectionId"],
              "username": f"Player-{msg['connectionId'][:6]}", "score": 0, "isReady": False}
    dynamo_room.save_player_in_room(db, room_id, player)
    dynamo_room.update_connection_room(db, msg["connectionId"], room_id, player_id)

    wsapi.post_message(ws, msg["connectionId"], outbound(TYPE_ROOM_CREATED, {
        "room": {"roomId": room_id, "roomCode": room_code, "status": "waiting", "hostPlayerId": player_id},
        "players": [{"playerId": player_id, "username": player["username"], "score": 0, "isReady": False}],
    }))


def handle_join_room(db, ws, msg):
    payload = msg.get("payload") or {}
    room_code = payload.get("roomCode", "")

    room = dynamo_room.get_room_by_code(db, room_code)
    if not room:
        wsapi.post_message(ws, msg["connectionId"], outbound(TYPE_ERROR, error_payload("ROOM_NOT_FOUND", "La sala no existe")))
        return
    if room["status"] != "waiting":
        wsapi.post_message(ws, msg["connectionId"], outbound(TYPE_ERROR, error_payload("ROOM_NOT_AVAILABLE", "La sala ya está en juego")))
        return

    player_id = player_id_from_conn(msg["connectionId"])
    player = {"playerId": player_id, "connectionId": msg["connectionId"],
              "username": f"Player-{msg['connectionId'][:6]}", "score": 0, "isReady": False}
    dynamo_room.save_player_in_room(db, room["roomId"], player)
    dynamo_room.update_connection_room(db, msg["connectionId"], room["roomId"], player_id)

    players = dynamo_room.list_players_in_room(db, room["roomId"])

    wsapi.post_message(ws, msg["connectionId"], outbound(TYPE_ROOM_JOINED, {
        "room": {"roomId": room["roomId"], "roomCode": room["roomCode"],
                 "status": room["status"], "hostPlayerId": room["hostPlayerId"]},
        "players": to_player_infos(players),
    }))

    joined_msg = outbound(TYPE_PLAYER_JOINED, {"player": {"playerId": player_id, "username": player["username"]}})
    for p in players:
        if p["connectionId"] != msg["connectionId"]:
            try:
                wsapi.post_message(ws, p["connectionId"], joined_msg)
            except Exception as e:
                logger.warning(json.dumps({"event": "notify-failed", "targetConn": p["connectionId"], "error": str(e)}))


def handle_leave_room(db, ws, msg):
    conn = dynamo_room.get_connection(db, msg["connectionId"])
    if not conn or not conn.get("roomId"):
        return

    player_id = player_id_from_conn(msg["connectionId"])
    dynamo_room.remove_player_from_room(db, conn["roomId"], player_id)

    players = dynamo_room.list_players_in_room(db, conn["roomId"])
    left_msg = outbound(TYPE_PLAYER_LEFT, {"playerId": player_id})
    for p in players:
        try:
            wsapi.post_message(ws, p["connectionId"], left_msg)
        except Exception as e:
            logger.warning(json.dumps({"event": "notify-failed", "targetConn": p["connectionId"], "error": str(e)}))


HANDLERS = {
    ACTION_CREATE_ROOM: handle_create_room,
    ACTION_JOIN_ROOM:   handle_join_room,
    ACTION_LEAVE_ROOM:  handle_leave_room,
}


def process_record(db, ws, record):
    msg = json.loads(record["body"])
    action = msg.get("action", "")
    fn = HANDLERS.get(action)
    if fn:
        fn(db, ws, msg)
    else:
        logger.warning(json.dumps({"event": "unhandled-action", "action": action}))


def handler(event, context):
    db, ws = get_clients()
    failures = []

    for record in event.get("Records", []):
        try:
            process_record(db, ws, record)
        except Exception as e:
            logger.error(json.dumps({"event": "record-failed", "messageId": record["messageId"], "error": str(e)}))
            failures.append({"itemIdentifier": record["messageId"]})

    return {"batchItemFailures": failures}

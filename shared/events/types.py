import json
import os

# --- Acciones inbound (cliente → servidor) ---
ACTION_CREATE_ROOM    = "CREATE_ROOM"
ACTION_JOIN_ROOM      = "JOIN_ROOM"
ACTION_LEAVE_ROOM     = "LEAVE_ROOM"
ACTION_START_GAME     = "START_GAME"
ACTION_SUBMIT_ANSWER  = "SUBMIT_ANSWER"
ACTION_PLAYER_READY   = "PLAYER_READY"
ACTION_GET_LEADERBOARD = "GET_LEADERBOARD"

# --- Tipos outbound (servidor → cliente) ---
TYPE_ROOM_CREATED  = "ROOM_CREATED"
TYPE_ROOM_JOINED   = "ROOM_JOINED"
TYPE_PLAYER_JOINED = "PLAYER_JOINED"
TYPE_PLAYER_LEFT   = "PLAYER_LEFT"
TYPE_GAME_STARTING = "GAME_STARTING"
TYPE_ROUND_START   = "ROUND_START"
TYPE_ROUND_END     = "ROUND_END"
TYPE_GAME_END      = "GAME_END"
TYPE_PLAYERS_READY = "PLAYERS_READY"
TYPE_LEADERBOARD   = "LEADERBOARD"
TYPE_ERROR         = "ERROR"


def outbound(msg_type, payload=None):
    """Construye el dict de un mensaje outbound."""
    msg = {"type": msg_type}
    if payload is not None:
        msg["payload"] = payload
    return msg


def error_payload(code, message):
    return {"code": code, "message": message}

import json
import logging
from concurrent.futures import ThreadPoolExecutor, as_completed
import boto3

from shared.dynamo import stats as dynamo_stats

logger = logging.getLogger()
logger.setLevel(logging.INFO)

_db = None


def get_db():
    global _db
    if _db is None:
        _db = boto3.resource("dynamodb")
    return _db


def handler(event, context):
    """
    Trigger: EventBridge event "game.ended" publicado por round-ender.
    En Python, event['detail'] ya es un dict (no es raw JSON como en Go).
    """
    detail  = event["detail"]
    room_id = detail["roomId"]
    players = detail["players"]

    logger.info(json.dumps({"event": "recording-stats", "roomId": room_id, "players": len(players)}))

    db     = get_db()
    errors = []

    with ThreadPoolExecutor(max_workers=max(len(players), 1)) as executor:
        futures = {
            executor.submit(
                dynamo_stats.update_player_stats,
                db,
                p["playerId"],
                p["username"],
                p["score"],
                p["isWinner"],
            ): p
            for p in players
        }
        for future in as_completed(futures):
            try:
                future.result()
            except Exception as e:
                p = futures[future]
                logger.error(json.dumps({"event": "update-stats-failed", "playerId": p["playerId"], "error": str(e)}))
                errors.append(e)

    if errors:
        # Retornar error para que EventBridge reintente con backoff.
        raise RuntimeError(f"Fallaron {len(errors)} actualizaciones de stats")

    logger.info(json.dumps({"event": "stats-recorded", "roomId": room_id}))

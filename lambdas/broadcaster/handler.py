import json
import logging
from concurrent.futures import ThreadPoolExecutor, as_completed
import boto3

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


def broadcast(db, ws, record):
    req = json.loads(record["body"])
    room_id = req["roomId"]
    message = req["message"]  # ya serializado como dict

    players = dynamo_room.list_players_in_room(db, room_id)
    if not players:
        return

    errors = []
    with ThreadPoolExecutor(max_workers=len(players)) as executor:
        futures = {
            executor.submit(wsapi.post_message, ws, p["connectionId"], message): p
            for p in players
        }
        for future in as_completed(futures):
            try:
                future.result()
            except Exception as e:
                p = futures[future]
                logger.warning(json.dumps({"event": "post-failed", "connectionId": p["connectionId"], "error": str(e)}))
                errors.append(e)

    if len(errors) == len(players):
        raise RuntimeError(f"Todos los {len(players)} PostToConnection fallaron")

    if errors:
        logger.warning(json.dumps({"event": "some-posts-failed", "failed": len(errors), "total": len(players)}))


def handler(event, context):
    db, ws = get_clients()
    failures = []

    for record in event.get("Records", []):
        try:
            broadcast(db, ws, record)
        except Exception as e:
            logger.error(json.dumps({"event": "broadcast-failed", "messageId": record["messageId"], "error": str(e)}))
            failures.append({"itemIdentifier": record["messageId"]})

    return {"batchItemFailures": failures}

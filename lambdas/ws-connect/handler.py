import json
import logging
import os
import time
import boto3

logger = logging.getLogger()
logger.setLevel(logging.INFO)

# Clientes inicializados a nivel de módulo para reutilizarlos en warm starts.
_db = None


def get_db():
    global _db
    if _db is None:
        _db = boto3.resource("dynamodb")
    return _db


def guest_player_id(connection_id):
    return f"guest-{connection_id}"


def handler(event, context):
    connection_id = event["requestContext"]["connectionId"]
    source_ip = event["requestContext"].get("identity", {}).get("sourceIp", "")

    logger.info(json.dumps({"event": "ws-connect", "connectionId": connection_id, "sourceIp": source_ip}))

    now = int(time.time())
    table_name = os.environ["DYNAMODB_TABLE"]

    try:
        get_db().Table(table_name).put_item(Item={
            "PK":          f"CONNECTION#{connection_id}",
            "SK":          "METADATA",
            "playerId":    guest_player_id(connection_id),
            "connectedAt": now,
            "expiresAt":   now + 24 * 60 * 60,  # TTL: 24h
        })
    except Exception as e:
        logger.error(json.dumps({"event": "ws-connect-error", "error": str(e)}))
        return {"statusCode": 500}

    logger.info(json.dumps({"event": "connection-saved", "connectionId": connection_id}))
    return {"statusCode": 200}

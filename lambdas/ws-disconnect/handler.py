import json
import logging
import os
import boto3

logger = logging.getLogger()
logger.setLevel(logging.INFO)

_db = None


def get_db():
    global _db
    if _db is None:
        _db = boto3.resource("dynamodb")
    return _db


def handler(event, context):
    connection_id = event["requestContext"]["connectionId"]
    logger.info(json.dumps({"event": "ws-disconnect", "connectionId": connection_id}))

    table_name = os.environ["DYNAMODB_TABLE"]
    try:
        get_db().Table(table_name).delete_item(Key={
            "PK": f"CONNECTION#{connection_id}",
            "SK": "METADATA",
        })
        logger.info(json.dumps({"event": "connection-deleted", "connectionId": connection_id}))
    except Exception as e:
        # Siempre retornamos 200 — API GW ignora el código en $disconnect.
        logger.error(json.dumps({"event": "ws-disconnect-error", "error": str(e)}))

    return {"statusCode": 200}

import json
import logging
import os
import boto3
from shared.events.types import (
    ACTION_CREATE_ROOM, ACTION_JOIN_ROOM, ACTION_LEAVE_ROOM,
    ACTION_START_GAME, ACTION_SUBMIT_ANSWER, ACTION_GET_LEADERBOARD,
)

logger = logging.getLogger()
logger.setLevel(logging.INFO)

_sqs = None

# Mapa acción → URL de cola SQS (inyectadas por Terraform como env vars)
SQS_QUEUES = None


def get_queues():
    global SQS_QUEUES
    if SQS_QUEUES is None:
        SQS_QUEUES = {
            ACTION_CREATE_ROOM:     os.environ["SQS_ROOM_MANAGER_URL"],
            ACTION_JOIN_ROOM:       os.environ["SQS_ROOM_MANAGER_URL"],
            ACTION_LEAVE_ROOM:      os.environ["SQS_ROOM_MANAGER_URL"],
            ACTION_START_GAME:      os.environ["SQS_QUIZ_ENGINE_URL"],
            ACTION_SUBMIT_ANSWER:   os.environ["SQS_QUIZ_ENGINE_URL"],
            ACTION_GET_LEADERBOARD: os.environ["SQS_QUIZ_ENGINE_URL"],
        }
    return SQS_QUEUES


def get_sqs():
    global _sqs
    if _sqs is None:
        _sqs = boto3.client("sqs")
    return _sqs


def handler(event, context):
    connection_id = event["requestContext"]["connectionId"]
    request_id    = event["requestContext"]["requestId"]
    body          = event.get("body", "")

    try:
        msg = json.loads(body)
    except (json.JSONDecodeError, TypeError):
        logger.warning(json.dumps({"event": "invalid-message", "connectionId": connection_id, "body": body}))
        return {"statusCode": 400}

    action = msg.get("action", "")
    queue_url = get_queues().get(action)

    if not queue_url:
        logger.warning(json.dumps({"event": "unknown-action", "action": action, "connectionId": connection_id}))
        return {"statusCode": 400}

    sqs_msg = json.dumps({
        "connectionId": connection_id,
        "action":       action,
        "payload":      msg.get("payload"),
    })

    get_sqs().send_message(
        QueueUrl=queue_url,
        MessageBody=sqs_msg,
        MessageGroupId=connection_id,
        MessageDeduplicationId=f"{connection_id}-{request_id}",
    )

    logger.info(json.dumps({"event": "message-routed", "action": action, "connectionId": connection_id}))
    return {"statusCode": 200}

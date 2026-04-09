import json
import os
import boto3


def new_client():
    endpoint = os.environ["WS_ENDPOINT"]
    return boto3.client("apigatewaymanagementapi", endpoint_url=endpoint)


def post_message(client, connection_id, payload):
    """Serializa payload a JSON y lo envía al connectionId dado."""
    data = json.dumps(payload, default=str).encode()
    client.post_to_connection(ConnectionId=connection_id, Data=data)

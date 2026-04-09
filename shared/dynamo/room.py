from boto3.dynamodb.conditions import Key
from .keys import (
    table_name, connection_pk, connection_sk,
    room_pk, room_code_pk, room_metadata_sk, room_player_sk,
)


def save_room(db, room):
    """Guarda los metadatos de la sala + lookup inverso roomCode → roomId."""
    table = db.Table(table_name())
    table.put_item(Item={
        "PK":           room_pk(room["roomId"]),
        "SK":           room_metadata_sk(),
        "roomId":       room["roomId"],
        "roomCode":     room["roomCode"],
        "status":       room["status"],
        "hostPlayerId": room["hostPlayerId"],
        "maxPlayers":   room.get("maxPlayers", 8),
    })
    # Lookup inverso: ROOMCODE#<code> → roomId
    table.put_item(Item={
        "PK":     room_code_pk(room["roomCode"]),
        "SK":     room_metadata_sk(),
        "roomId": room["roomId"],
    })


def get_room(db, room_id):
    table = db.Table(table_name())
    resp = table.get_item(Key={"PK": room_pk(room_id), "SK": room_metadata_sk()})
    return resp.get("Item")


def get_room_by_code(db, room_code):
    """Resuelve roomCode → roomId y luego carga la sala completa."""
    table = db.Table(table_name())
    idx = table.get_item(Key={"PK": room_code_pk(room_code), "SK": room_metadata_sk()})
    item = idx.get("Item")
    if not item:
        return None
    return get_room(db, item["roomId"])


def save_player_in_room(db, room_id, player):
    table = db.Table(table_name())
    table.put_item(Item={
        "PK":           room_pk(room_id),
        "SK":           room_player_sk(player["playerId"]),
        "playerId":     player["playerId"],
        "connectionId": player["connectionId"],
        "username":     player["username"],
        "score":        player.get("score", 0),
        "isReady":      player.get("isReady", False),
    })


def remove_player_from_room(db, room_id, player_id):
    table = db.Table(table_name())
    table.delete_item(Key={
        "PK": room_pk(room_id),
        "SK": room_player_sk(player_id),
    })


def list_players_in_room(db, room_id):
    """Retorna todos los jugadores de una sala usando Query + begins_with."""
    table = db.Table(table_name())
    resp = table.query(
        KeyConditionExpression=Key("PK").eq(room_pk(room_id)) & Key("SK").begins_with("PLAYER#")
    )
    return resp.get("Items", [])


def update_connection_room(db, connection_id, room_id, player_id):
    table = db.Table(table_name())
    table.update_item(
        Key={"PK": connection_pk(connection_id), "SK": connection_sk()},
        UpdateExpression="SET roomId = :rid, connectionId = :cid",
        ExpressionAttributeValues={":rid": room_id, ":cid": connection_id},
    )


def get_connection(db, connection_id):
    table = db.Table(table_name())
    resp = table.get_item(Key={"PK": connection_pk(connection_id), "SK": connection_sk()})
    return resp.get("Item")

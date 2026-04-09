import os


def table_name():
    return os.environ["DYNAMODB_TABLE"]


# --- Conexiones ---
def connection_pk(connection_id):  return f"CONNECTION#{connection_id}"
def connection_sk():               return "METADATA"

# --- Salas ---
def room_pk(room_id):              return f"ROOM#{room_id}"
def room_code_pk(room_code):       return f"ROOMCODE#{room_code}"
def room_metadata_sk():            return "METADATA"
def room_player_sk(player_id):     return f"PLAYER#{player_id}"
def room_round_sk(round_number):   return f"ROUND#{round_number:03d}"
def room_answer_sk(round_number, player_id):
    return f"ROUND#{round_number:03d}#ANSWER#{player_id}"

# --- Jugadores ---
def player_pk(player_id):          return f"PLAYER#{player_id}"
def player_profile_sk():           return "METADATA"

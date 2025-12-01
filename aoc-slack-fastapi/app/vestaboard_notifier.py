import os
import requests
from loguru import logger

VESTABOARD_API_KEY = os.getenv("VESTABOARD_API_KEY")
VESTABOARD_URL = "https://rw.vestaboard.com/"

# Example: send a message (list of lists)
def send_vestaboard_message(message):
    if not VESTABOARD_API_KEY:
        print("No Vestaboard API key configured.")
        return
    headers = {
        "X-Vestaboard-Read-Write-Key": VESTABOARD_API_KEY,
        "Content-Type": "application/json"
    }
    # logger.info(f"Sending Vestaboard message: {message}")

    resp = requests.post(VESTABOARD_URL, json=message, headers=headers)
    resp.raise_for_status()
    

import os
import requests
from loguru import logger

SLACK_WEBHOOK_URL = os.getenv("AOC_SLACK_WEBHOOK_URL")

def send_slack_message(message: str):
    if not SLACK_WEBHOOK_URL:
        print("No Slack webhook configured.")
        return
    payload = {"text": message}

    logger.info(f"Sending Vestaboard message: {payload}")
    # resp = requests.post(SLACK_WEBHOOK_URL, json=payload)
    # resp.raise_for_status()

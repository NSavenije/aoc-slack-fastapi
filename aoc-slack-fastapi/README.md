# Advent of Code Slack & Vestaboard Notifier (Python FastAPI)

This app fetches Advent of Code leaderboard data, sends daily leaderboard updates to Slack, and posts star updates and Vestaboard updates every 15 minutes.

## Features
- Modular structure: AoC API client, Slack notifier, Vestaboard notifier, scheduler
- FastAPI for web interface and health checks
- Docker & docker-compose support
- Configuration via environment variables

## Environment Variables
- AOC_SLACK_LEADERBOARD_ID
- AOC_SLACK_SESSION
- AOC_SLACK_WEBHOOK_URL
- VESTABOARD_API_KEY

## Usage
- Build: `docker build -t aoc-slack-fastapi .`
- Run: `docker-compose up`

## Endpoints
- `/health` - Health check

## Scheduling
- Leaderboard to Slack: daily
- Star updates & Vestaboard: every 15 minutes

---
Replace placeholder values in `.env` or `docker-compose.yml` before running.

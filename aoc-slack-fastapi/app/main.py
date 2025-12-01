from fastapi import FastAPI
from app.scheduler import start_scheduler

app = FastAPI()

@app.get("/health")
def health():
    return {"status": "ok"}

# Start background scheduler
start_scheduler()

from fastapi import FastAPI, Request
from fastapi.responses import HTMLResponse, FileResponse
from fastapi.staticfiles import StaticFiles
import os

app = FastAPI()

# Health endpoint remains for monitoring
@app.get("/health")
def health():
    return {"status": "healthy"}

# Portfolio static directory
UI_DIR = os.path.join(os.path.dirname(__file__), "ui")

# Mount CSS/JS for portfolio
app.mount("/portfolio-static", StaticFiles(directory=UI_DIR), name="portfolio-static")

# Serve portfolio HTML
@app.get("/portfolio", response_class=HTMLResponse)
async def get_portfolio():
    file_path = os.path.join(UI_DIR, "portfolio.html")
    if not os.path.exists(file_path):
        return HTMLResponse("<h1>Portfolio Not Found</h1>", status_code=404)
    with open(file_path, encoding="utf-8") as f:
        return HTMLResponse(f.read())

import pytest
from httpx import AsyncClient
from src.agentic_dev_agent.__init__ import app
import os

@pytest.mark.asyncio
async def test_portfolio_page_renders():
    async with AsyncClient(app=app, base_url="http://test") as ac:
        resp = await ac.get("/portfolio")
    assert resp.status_code == 200
    html = resp.text
    assert "Generative AI Developer Portfolio" in html
    assert "Skills & Expertise" in html
    assert "Selected Projects" in html
    assert "Contact" in html

@pytest.mark.asyncio
async def test_portfolio_static_assets_exist():
    """
    Basic checks that static assets (CSS/JS) return 200
    """
    async with AsyncClient(app=app, base_url="http://test") as ac:
        css = await ac.get("/portfolio-static/portfolio.css")
        js = await ac.get("/portfolio-static/portfolio.js")
    assert css.status_code == 200
    assert js.status_code == 200
    # Sanity: check CSS and JS have identifying strings
    assert b"body {" in css.content
    assert b"DOMContentLoaded" in js.content

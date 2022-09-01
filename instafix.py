import asyncio
import json
import os
import re
from typing import Optional

import aioredis
import httpx
import pyvips
import sentry_sdk
from fastapi import FastAPI, Request
from fastapi.responses import FileResponse, HTMLResponse, RedirectResponse
from fastapi.templating import Jinja2Templates

pyvips.cache_set_max(0)
pyvips.cache_set_max_mem(0)
pyvips.cache_set_max_files(0)
os.makedirs("static", exist_ok=True)
if "SENTRY_DSN" in os.environ:
    sentry_sdk.init(
        dsn=os.environ["SENTRY_DSN"],
    )
    print("Sentry initialized.")

app = FastAPI()
templates = Jinja2Templates(directory="templates")

headers = {
    "accept": "*/*",
    "accept-language": "en-US,en;q=0.9",
    "origin": "https://www.instagram.com",
    "referer": "https://www.instagram.com/",
    "sec-fetch-dest": "empty",
    "sec-fetch-mode": "cors",
    "sec-fetch-site": "same-site",
    "user-agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 13_2_3 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.3 Mobile/15E148 Safari/604.1",
    "x-ig-app-id": "1217981644879628",
}

async def get_data(request: Request, post_id: str) -> Optional[dict]:
    r = request.app.state.redis
    client = app.state.client

    api_resp = await r.get(post_id)
    if api_resp is None:
        api_resp = (
            await client.get(
                f"https://www.instagram.com/p/{post_id}/embed/captioned",
            )
        ).text
        await r.set(post_id, api_resp, ex=24 * 3600)
    data = re.findall(
        r"window\.__additionalDataLoaded\('extra',(.*)\);<\/script>", api_resp
    )
    return json.loads(data[0])


@app.on_event("startup")
async def startup():
    app.state.redis = await aioredis.from_url(
        "redis://localhost:6379", encoding="utf-8", decode_responses=True
    )
    app.state.client = httpx.AsyncClient(
        headers=headers, follow_redirects=True, timeout=60.0
    )


@app.on_event("shutdown")
async def shutdown():
    await app.state.redis.close()
    await app.state.client.aclose()


@app.get("/", response_class=HTMLResponse)
def root():
    with open("templates/home.html") as f:
        html = f.read()
    return HTMLResponse(content=html)


@app.get("/p/{post_id}", response_class=HTMLResponse)
@app.get("/p/{post_id}/{num}", response_class=HTMLResponse)
@app.get("/reel/{post_id}", response_class=HTMLResponse)
@app.get("/tv/{post_id}", response_class=HTMLResponse)
async def read_item(request: Request, post_id: str, num: Optional[int] = None):
    post_url = f"https://instagram.com/p/{post_id}"
    if not re.search(
        r"bot|facebook|embed|got|firefox\/92|curl|wget",
        request.headers.get("User-Agent", "").lower(),
    ):
        return RedirectResponse(post_url)

    data = await get_data(request, post_id)
    item = data["shortcode_media"]

    media_lst = item["edge_sidecar_to_children"]["edges"]
    media = (media_lst[num - 1] if num else media_lst[0])["node"]

    description = item["edge_media_to_caption"]["edges"][0]["node"]["text"]
    username = item["owner"]["username"]

    ctx = {
        "request": request,
        "url": post_url,
        "description": description,
        "username": username,
    }

    if num is None and media["__typename"] == "GraphImage":
        ctx["image"] = f"/grid/{post_id}"
        ctx["card"] = "summary_large_image"
    elif media["__typename"] == "GraphVideo":
        num = num if num else 1
        video = media["video_versions"][0]
        ctx["video"] = f"/videos/{post_id}/{num}"
        ctx["width"] = video["width"]
        ctx["height"] = video["height"]
        ctx["card"] = "player"
    elif media["__typename"] == "GraphImage":
        num = num if num else 1
        image = media["image_versions2"]["candidates"][0]
        ctx["image"] = f"/images/{post_id}/{num}"
        ctx["width"] = image["width"]
        ctx["height"] = image["height"]
        ctx["card"] = "summary_large_image"
    return templates.TemplateResponse("base.html", ctx)


@app.get("/videos/{post_id}/{num}")
async def videos(request: Request, post_id: str, num: int):
    data = await get_data(request, post_id)
    item = data["shortcode_media"]

    media_lst = item["edge_sidecar_to_children"]["edges"]
    media = (media_lst[num - 1] if num else media_lst[0])["node"]

    video_url = media["display_url"]
    video_url = re.sub(
        r"https:\/\/.*?\/v\/", "https://scontent.cdninstagram.com/v/", video_url
    )
    return RedirectResponse(video_url)


@app.get("/images/{post_id}/{num}")
async def images(request: Request, post_id: str, num: int):
    data = await get_data(request, post_id)
    item = data["shortcode_media"]

    media_lst = item["edge_sidecar_to_children"]["edges"]
    media = (media_lst[num - 1] if num else media_lst[0])["node"]

    image_url = media["display_url"]
    image_url = re.sub(
        r"https:\/\/.*?\/v\/", "https://scontent.cdninstagram.com/v/", image_url
    )
    return RedirectResponse(image_url)


@app.get("/grid/{post_id}")
async def grid(request: Request, post_id: str):
    client = request.app.state.client
    if os.path.exists(f"static/grid:{post_id}.jpg"):
        return FileResponse(
            f"static/grid:{post_id}.jpg",
            media_type="image/jpeg",
            headers={"Cache-Control": "public, max-age=31536000"},
        )

    async def download_image(url):
        resp = await client.get(url)
        return resp.content

    data = await get_data(request, post_id)
    item = data["shortcode_media"]

    media_lst = item["edge_sidecar_to_children"]["edges"]

    # Limit to 4 images, Discord only show 4 images originally
    media_urls = [
        m["node"]["display_url"] for m in media_lst if m["__typename"] == "GraphImage"
    ][:4]

    # Download images and merge them into a single image
    media_imgs = await asyncio.gather(*[download_image(url) for url in media_urls])
    media_vips = [
        pyvips.Image.new_from_buffer(img, "", access="sequential") for img in media_imgs
    ]
    accross = min(len(media_imgs), 2)
    grid_img = pyvips.Image.arrayjoin(media_vips, across=accross, shim=10)
    grid_img.write_to_file(f"static/grid:{post_id}.jpg")

    return FileResponse(
        f"static/grid:{post_id}.jpg",
        media_type="image/jpeg",
        headers={"Cache-Control": "public, max-age=31536000"},
    )

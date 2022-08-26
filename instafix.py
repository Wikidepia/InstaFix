import asyncio
import json
import os
import re
from http.cookiejar import MozillaCookieJar
from typing import Optional

import aioredis
import httpx
import pyvips
import sentry_sdk
from fastapi import FastAPI, Request
from fastapi.responses import FileResponse, HTMLResponse, RedirectResponse
from fastapi.templating import Jinja2Templates

SAFE_ERROR = ["Media not found", "Invalid media_id", "geoblock_required"]

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

cookies = MozillaCookieJar("cookies.txt")
cookies.load()

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

# Thanks to @gerbz! https://stackoverflow.com/a/37246231
def shortcode_to_mediaid(shortcode):
    alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
    mediaid = 0
    for letter in shortcode:
        mediaid = (mediaid * 64) + alphabet.index(letter)
    return mediaid


async def get_data(request: Request, post_id: str) -> Optional[dict]:
    r = request.app.state.redis
    client = app.state.client

    data = await r.get(post_id)
    missed = data is None
    if missed:
        media_id = shortcode_to_mediaid(post_id)
        for _ in range(3):
            api_resp = await client.get(
                f"https://i.instagram.com/api/v1/media/{media_id}/info/",
            )
            data = api_resp.text
            if data != "":
                break
            await asyncio.sleep(0.1)
    data_dict = json.loads(data)
    message = data_dict.get("message")
    if message and all(err not in message for err in SAFE_ERROR):
        raise Exception(message)
    if missed and message is None:
        await r.set(post_id, data, ex=24 * 3600)
    return data_dict


@app.on_event("startup")
async def startup():
    app.state.redis = await aioredis.from_url(
        "redis://localhost:6379", encoding="utf-8", decode_responses=True
    )
    app.state.client = httpx.AsyncClient(
        headers=headers, cookies=cookies, follow_redirects=True, timeout=60.0
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
    if "items" not in data:
        return
    item = data["items"][0]

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1] if num else media_lst[0]

    description = item["caption"]["text"] if item["caption"] else ""
    full_name = item["user"]["full_name"]
    username = item["user"]["username"]

    ctx = {
        "request": request,
        "url": post_url,
        "description": description,
        "full_name": full_name,
        "username": username,
    }

    if num is None and "video_versions" not in media:
        ctx["image"] = f"/grid/{post_id}"
        ctx["card"] = "summary_large_image"
    elif "video_versions" in media:
        num = num if num else 1
        video = media["video_versions"][0]
        ctx["video"] = f"/videos/{post_id}/{num}"
        ctx["width"] = video["width"]
        ctx["height"] = video["height"]
        ctx["card"] = "player"
    elif "image_versions2" in media:
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
    if "items" not in data:
        return
    item = data["items"][0]

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1]
    video_url = media["video_versions"][0]["url"]
    video_url = re.sub(
        r"https:\/\/.*?\/v\/", "https://scontent.cdninstagram.com/v/", video_url
    )
    return RedirectResponse(video_url)


@app.get("/images/{post_id}/{num}")
async def images(request: Request, post_id: str, num: int):
    data = await get_data(request, post_id)
    if "items" not in data:
        return
    item = data["items"][0]

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1]
    image_url = media["image_versions2"]["candidates"][0]["url"]
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
    if "items" not in data:
        return
    item = data["items"][0]

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    # Limit to 4 images, Discord only show 4 images originally
    media_urls = [
        m["image_versions2"]["candidates"][0]["url"]
        for m in media_lst
        if "image_versions2" in m and "video_versions" not in m
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

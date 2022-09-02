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
from selectolax.parser import HTMLParser

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
    "accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9",
    "accept-language": "en-US,en;q=0.9",
    "cache-control": "max-age=0",
    "sec-ch-prefers-color-scheme": "dark",
    "sec-ch-ua": '" Not A;Brand";v="99", "Chromium";v="100"',
    "sec-ch-ua-mobile": "?1",
    "sec-ch-ua-platform": '"Android"',
    "sec-fetch-dest": "document",
    "sec-fetch-mode": "navigate",
    "sec-fetch-site": "none",
    "sec-fetch-user": "?1",
    "upgrade-insecure-requests": "1",
    "user-agent": "Mozilla/5.0 (Linux; Android 6.0; Nexus 5 Build/MRA58N) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.60 Mobile Safari/537.36",
    "viewport-width": "1280",
}


async def get_data(request: Request, post_id: str) -> Optional[dict]:
    r = request.app.state.redis
    client = app.state.client

    api_resp = await r.get(post_id)
    if api_resp is None:
        api_resp = (
            await client.get(
                f"https://www.instagram.com/reel/{post_id}/embed/captioned",
            )
        ).text
        await r.set(post_id, api_resp, ex=24 * 3600)
    data = re.findall(
        r"window\.__additionalDataLoaded\('extra',(.*)\);<\/script>", api_resp
    )
    data = json.loads(data[0])
    if data is None:
        data = parse_embed(api_resp)
    return data


def parse_embed(html: str) -> dict:
    tree = HTMLParser(html)
    typename = "GraphImage"
    display_url = tree.css_first(".EmbeddedMediaImage")
    if not display_url:
        typename = "GraphVideo"
        display_url = tree.css_first("video")
    display_url = display_url.attrs["src"]
    username = tree.css_first(".UsernameText").text()
    # Remove div class CaptionComments, CaptionUsername
    tree.css_first(".CaptionComments").remove()
    tree.css_first(".CaptionUsername").remove()
    caption = tree.css_first(".Caption")
    for node in caption.css("br"):
        node.replace_with("\n")
    caption_text = caption.text().strip()
    return {
        "shortcode_media": {
            "__typename": typename,
            "owner": {"username": username},
            "node": {"display_url": display_url},
            "edge_media_to_caption": {"edges": [{"node": {"text": caption_text}}]},
            "dimensions": {"height": None, "width": None},
        }
    }


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
    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        media = (media_lst[num - 1] if num else media_lst[0])["node"]
    else:
        media = item

    description = item["edge_media_to_caption"]["edges"][0]["node"]["text"]
    username = item["owner"]["username"]
    ctx = {
        "request": request,
        "url": post_url,
        "description": description,
        "username": username,
        "width": media["dimensions"]["width"],
        "height": media["dimensions"]["height"],
    }

    if num is None and media["__typename"] == "GraphImage":
        ctx["image"] = f"/grid/{post_id}"
        ctx["card"] = "summary_large_image"
    elif media["__typename"] == "GraphVideo":
        num = num if num else 1
        ctx["video"] = f"/videos/{post_id}/{num}"
        ctx["card"] = "player"
    elif media["__typename"] == "GraphImage":
        num = num if num else 1
        ctx["image"] = f"/images/{post_id}/{num}"
        ctx["card"] = "summary_large_image"
    return templates.TemplateResponse("base.html", ctx)


@app.get("/videos/{post_id}/{num}")
async def videos(request: Request, post_id: str, num: int):
    data = await get_data(request, post_id)
    item = data["shortcode_media"]
    with open("embed.json", "w") as f:
        json.dump(item, f, indent=2)
    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        media = (media_lst[num - 1] if num else media_lst[0])
    else:
        media = item

    if "node" in media:
        media = media["node"]
    video_url = media.get("video_url") or media.get("display_url")
    return RedirectResponse(video_url)


@app.get("/images/{post_id}/{num}")
async def images(request: Request, post_id: str, num: int):
    data = await get_data(request, post_id)
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        media = (media_lst[num - 1] if num else media_lst[0])
    else:
        media = item

    if "node" in media:
        media = media["node"]
    image_url = media["display_url"]
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
    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
    else:
        media_lst = [item]

    # Limit to 4 images, Discord only show 4 images originally
    media_urls = [
        m["node"]["display_url"]
        for m in media_lst
        if m.get("node", m)["__typename"] == "GraphImage"
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

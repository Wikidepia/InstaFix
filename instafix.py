import asyncio
import json
import os
import re
from typing import Optional

import aioredis
import httpx
import pyvips
import sentry_sdk
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import (FileResponse, HTMLResponse, RedirectResponse,
                               StreamingResponse)
from fastapi.templating import Jinja2Templates
from selectolax.parser import HTMLParser
from starlette.background import BackgroundTask

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
    "cache-control": "no-cache",
    "pragma": "no-cache",
    "referer": "https://www.instagram.com/",
    "user-agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.60 Safari/537.36",
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
    if not display_url:
        return {"error": "Not found"}
    display_url = display_url.attrs["src"]
    username = tree.css_first(".UsernameText").text()

    # Remove div class CaptionComments, CaptionUsername
    caption_comments = tree.css_first(".CaptionComments")
    if caption_comments:
        caption_comments.remove()
    caption_username = tree.css_first(".CaptionUsername")
    if caption_username:
        caption_username.remove()

    caption_text = ""
    caption = tree.css_first(".Caption")
    if caption:
        for node in caption.css("br"):
            node.replace_with("\n")
        caption_text = caption.text().strip()

    return {
        "shortcode_media": {
            "owner": {"username": username},
            "node": {"__typename": typename, "display_url": display_url},
            "edge_media_to_caption": {"edges": [{"node": {"text": caption_text}}]},
            "dimensions": {"height": None, "width": None},
        }
    }


def mediaid_to_code(media_id):
    alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
    short_code = ""
    while media_id > 0:
        media_id, remainder = divmod(media_id, 64)
        short_code = alphabet[remainder] + short_code
    return short_code


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


@app.get("/p/{post_id}")
@app.get("/p/{post_id}/{num}")
@app.get("/reel/{post_id}")
@app.get("/tv/{post_id}")
async def read_item(request: Request, post_id: str, num: Optional[int] = None):
    post_url = f"https://instagram.com/p/{post_id}"
    if not re.search(
        r"bot|facebook|embed|got|firefox\/92|curl|wget",
        request.headers.get("User-Agent", "").lower(),
    ):
        return RedirectResponse(post_url)

    data = await get_data(request, post_id)
    if "error" in data:
        return HTTPException(status_code=404, detail="Post not found")
    item = data["shortcode_media"]
    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        media = (media_lst[num - 1 if num else 0])["node"]
    else:
        media = item

    typename = media.get("node", media)["__typename"]
    description = item["edge_media_to_caption"]["edges"]
    if description != []:
        description = description[0]["node"]["text"]
    username = item["owner"]["username"]

    ctx = {
        "request": request,
        "url": post_url,
        "description": description,
        "username": username,
        "width": media["dimensions"]["width"],
        "height": media["dimensions"]["height"],
    }

    is_image = typename in ["GraphImage", "StoryImage", "StoryVideo"]
    if num is None and is_image:
        ctx["image"] = f"/grid/{post_id}"
        ctx["card"] = "summary_large_image"
    elif typename == "GraphVideo":
        num = num if num else 1
        ctx["video"] = f"/videos/{post_id}/{num}"
        ctx["card"] = "player"
    elif is_image:
        num = num if num else 1
        ctx["image"] = f"/images/{post_id}/{num}"
        ctx["card"] = "summary_large_image"
    return templates.TemplateResponse("base.html", ctx)


@app.get("/stories/{username}/{post_id}")
async def stories(username: str, post_id: str):
    post_code = mediaid_to_code(int(post_id))
    return RedirectResponse(f"/p/{post_code}")


@app.get("/videos/{post_id}/{num}")
async def videos(request: Request, post_id: str, num: int):
    client = app.state.client
    data = await get_data(request, post_id)
    if "error" in data:
        return HTTPException(status_code=404, detail="Post not found")
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        media = media_lst[num - 1] if num else media_lst[0]
    else:
        media = item

    media = media.get("node", media)
    video_url = media.get("video_url", media["display_url"])

    # # Proxy video because Instagram speed limit
    # req = client.build_request("GET", video_url)
    # stream = await client.send(req, stream=True)
    # return StreamingResponse(
    #     stream.aiter_bytes(1024 * 4),
    #     media_type=stream.headers["Content-Type"],
    #     headers={"Content-Length": stream.headers["Content-Length"]},
    #     background=BackgroundTask(stream.aclose),
    # )
    return RedirectResponse(video_url, status_code=302)

@app.get("/images/{post_id}/{num}")
async def images(request: Request, post_id: str, num: int):
    data = await get_data(request, post_id)
    if "error" in data:
        return HTTPException(status_code=404, detail="Post not found")
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        media = media_lst[num - 1] if num else media_lst[0]
    else:
        media = item

    media = media.get("node", media)
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
    if "error" in data:
        return HTTPException(status_code=404, detail="Post not found")
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
    else:
        media_lst = [item]

    is_image = lambda x: x in ["GraphImage", "StoryImage", "StoryVideo"]
    # Limit to 4 images, Discord only show 4 images originally
    media_urls = [
        m.get("node", m)["display_url"]
        for m in media_lst
        if is_image(m.get("node", m)["__typename"])
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

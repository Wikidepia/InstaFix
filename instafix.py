import asyncio
import json
import os
import random
import re
import time
from typing import Optional
from urllib.parse import urlencode, urljoin

import esprima
import httpx
import pyvips
import sentry_sdk
import tenacity
from fastapi import FastAPI, Request
from fastapi.responses import (FileResponse, HTMLResponse, JSONResponse,
                               RedirectResponse)
from fastapi.templating import Jinja2Templates
from selectolax.parser import HTMLParser
from sqlitedict import SqliteDict

pyvips.cache_set_max(0)
pyvips.cache_set_max_mem(0)
pyvips.cache_set_max_files(0)
os.makedirs("static", exist_ok=True)

if "SENTRY_DSN" in os.environ:
    sentry_sdk.init(
        dsn=os.environ["SENTRY_DSN"],
        sample_rate=0.5,
    )
    print("Sentry initialized.")
if "EMBED_PROXY" in os.environ:
    print("Using proxy:", os.environ["EMBED_PROXY"])
if "GRAPHQL_PROXY" in os.environ:
    print("Using GraphQL proxy:", os.environ["GRAPHQL_PROXY"])
if "WORKER_PROXY" not in os.environ:
    raise Exception("WORKER_PROXY not set")
print("Using worker proxy:", os.environ["WORKER_PROXY"])

app = FastAPI()
templates = Jinja2Templates(directory="templates")

headers = {
    "authority": "www.instagram.com",
    "accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9",
    "accept-language": "en-US,en;q=0.9",
    "cache-control": "max-age=0",
    "sec-fetch-mode": "navigate",
    "upgrade-insecure-requests": "1",
    "referer": "https://www.instagram.com/",
    "user-agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.60 Safari/537.36",
    "viewport-width": "1280",
}


async def get_data(post_id: str) -> Optional[dict]:
    c = app.state.cache

    data = c.get(post_id)
    if not data or data["expire"] < time.time():
        data = await _get_data(post_id)
        c[post_id] = {"content": data, "expire": time.time() + (24 * 60 * 60)}
    else:
        data = data["content"]
    data = data.get("data", data)
    return data


@tenacity.retry(stop=tenacity.stop_after_attempt(3), wait=tenacity.wait_fixed(1))
async def _get_data(post_id: str) -> Optional[dict]:
    client = app.state.client

    api_resp = (
        await client.get(
            f"https://www.instagram.com/p/{post_id}/embed/captioned",
        )
    ).text

    # additionalDataLoaded
    data = re.findall(
        r"window\.__additionalDataLoaded\('extra',(.*)\);<\/script>", api_resp
    )
    if data:
        gql_data = json.loads(data[0])
        if gql_data and gql_data.get("shortcode_media"):
            return gql_data

    # TimeSliceImpl
    data = re.findall(r'<script>(requireLazy\(\["TimeSliceImpl".*)<\/script>', api_resp)
    if data and "shortcode_media" in data[0]:
        tokenized = esprima.tokenize(data[0])
        for token in tokenized:
            if "shortcode_media" in token.value:
                # json.loads to unescape the JSON
                return json.loads(json.loads(token.value))["gql_data"]

    # Get data from HTML
    embed_data = parse_embed(api_resp)
    if "error" not in embed_data and not embed_data["shortcode_media"]["video_blocked"]:
        return embed_data

    # Get data from JSON-LD if video is blocked
    ld_data = await parse_json_ld(post_id)
    if "error" not in ld_data:
        return ld_data

    # Query data from GraphQL, if video is blocked
    if "GRAPHQL_PROXY" in os.environ:
        gql_data = await query_gql(post_id)
        if gql_data.get("status") == "ok":
            return gql_data["data"]
    return embed_data


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
            "video_blocked": "WatchOnInstagram" in html,
        }
    }


async def parse_json_ld(post_id: str) -> dict:
    client = app.state.client

    resp = await client.get(f"https://www.instagram.com/p/{post_id}/")
    if resp.status_code != 200:
        return {"error": "Not found"}

    tree = HTMLParser(resp.text)
    json_ld = tree.css_first("script[type='application/ld+json']")
    if not json_ld:
        return {"error": "Server is blocked from Instagram"}

    json_ld = json.loads(json_ld.text())
    if isinstance(json_ld, list):
        json_ld = json_ld[0]

    # Get embed_data from JSON-LD if embed is blocked
    username = json_ld["author"]["identifier"]["value"]
    caption = json_ld.get("articleBody", "")

    ld_data = {
        "shortcode_media": {
            "owner": {"username": username},
            "edge_media_to_caption": {"edges": [{"node": {"text": caption}}]},
        }
    }

    media_edges = []
    video_data = json_ld.get("video", [])
    for video in video_data:
        media_edges.append(
            {
                "node": {
                    "__typename": "GraphVideo",
                    "display_url": video["contentUrl"],
                    "dimensions": {
                        "height": video.get("height"),
                        "width": video.get("width"),
                    },
                }
            }
        )

    image_data = json_ld.get("image", [])
    for image in image_data:
        media_edges.append(
            {
                "node": {
                    "__typename": "GraphImage",
                    "display_url": image["url"],
                    "dimensions": {
                        "height": image.get("height"),
                        "width": image.get("width"),
                    },
                }
            }
        )

    ld_data["shortcode_media"]["edge_sidecar_to_children"] = {"edges": media_edges}
    return ld_data


async def query_gql(post_id: str) -> dict:
    client = app.state.gql_client
    params = {
        "query_hash": "b3055c01b4b222b8a47dc12b090e4e64",
        "variables": json.dumps({"shortcode": post_id}),
    }
    try:
        response = await client.get(
            "https://www.instagram.com/graphql/query/", params=params
        )
        return response.json()
    except httpx.ReadTimeout:
        return {"status": "fail"}


def mediaid_to_code(media_id: int):
    alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
    short_code = ""
    while media_id > 0:
        media_id, remainder = divmod(media_id, 64)
        short_code = alphabet[remainder] + short_code
    return short_code


@app.on_event("startup")
async def startup():
    app.state.cache = SqliteDict("cache.sqlite", autocommit=True)
    limits = httpx.Limits(max_keepalive_connections=None, max_connections=None)
    app.state.client = httpx.AsyncClient(
        headers=headers,
        follow_redirects=True,
        timeout=120.0,
        limits=limits,
        proxies={"all://www.instagram.com": os.environ.get("EMBED_PROXY")},
    )
    # GraphQL are constantly blocked,
    # it needs to use a different proxy (residential preferred)
    app.state.gql_client = httpx.AsyncClient(
        headers=headers,
        follow_redirects=True,
        timeout=5.0,
        limits=limits,
        proxies={"all://www.instagram.com": os.environ.get("GRAPHQL_PROXY")},
    )


@app.on_event("shutdown")
async def shutdown():
    await app.state.client.aclose()


@app.get("/", response_class=HTMLResponse)
def root():
    with open("templates/home.html") as f:
        html = f.read()
    return HTMLResponse(content=html)


@app.get("/p/{post_id}")
@app.get("/p/{post_id}/{num}")
@app.get("/reel/{post_id}")
@app.get("/reels/{post_id}")
@app.get("/tv/{post_id}")
@app.get("/stories/{username}/{post_id}")
async def read_item(request: Request, post_id: str, num: Optional[int] = None):
    if "/stories/" in request.url.path:
        if not post_id.isdigit():
            return FileResponse("templates/404.html", status_code=404)
        post_id = mediaid_to_code(int(post_id))

    post_url = urljoin("https://www.instagram.com/", request.url.path)
    if not re.search(
        r"bot|facebook|embed|got|firefox\/92|firefox\/38|curl|wget|go-http|yahoo|generator|whatsapp|preview|link|proxy|vkshare|images|analyzer|index|crawl|spider|python|cfnetwork|node",
        request.headers.get("User-Agent", "").lower(),
    ):
        return RedirectResponse(post_url)

    data = await get_data(post_id)
    if "error" in data:
        ctx = {
            "request": request,
            "title": "InstaFix",
            "url": post_url,
            "description": "Sorry, this post isn't available.",
        }
        return templates.TemplateResponse("base.html", ctx, status_code=404)

    item = data["shortcode_media"]
    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        media = (media_lst[num - 1 if num else 0])["node"]
    else:
        media = item

    typename = media.get("node", media)["__typename"]
    description = item["edge_media_to_caption"]["edges"] or [{"node": {"text": ""}}]
    description = description[0]["node"]["text"]
    description = description[:200] + "..." if len(description) > 200 else description

    ctx = {
        "request": request,
        "url": post_url,
        "description": description,
        "post_id": post_id,
        "title": f"@{item['owner']['username']}",
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


@app.get("/videos/{post_id}/{num}")
async def videos(request: Request, post_id: str, num: int):
    data = await get_data(post_id)
    if "error" in data:
        return FileResponse("templates/404.html", status_code=404)
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        if num > len(media_lst):
            return FileResponse("templates/404.html", status_code=404)
        media = media_lst[num - 1] if num else media_lst[0]
    else:
        media = item

    media = media.get("node", media)
    video_url = media.get("video_url", media["display_url"])

    # Proxy video via CF worker because Instagram speed limit
    params = urlencode({"url": video_url, "referer": "https://instagram.com/"})
    wproxy = random.choice(os.environ["WORKER_PROXY"].split(","))
    return RedirectResponse(f"{wproxy}?{params}")


@app.get("/images/{post_id}/{num}")
async def images(request: Request, post_id: str, num: int):
    data = await get_data(post_id)
    if "error" in data:
        return FileResponse("templates/404.html", status_code=404)
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        if num > len(media_lst):
            return FileResponse("templates/404.html", status_code=404)
        media = media_lst[num - 1] if num else media_lst[0]
    else:
        media = item

    media = media.get("node", media)
    image_url = media["display_url"]
    return RedirectResponse(image_url)


@app.get("/oembed.json")
async def oembed(request: Request, post_id: str):
    data = await get_data(post_id)
    if "error" in data:
        return FileResponse("templates/404.html", status_code=404)
    item = data["shortcode_media"]
    description = item["edge_media_to_caption"]["edges"] or [{"node": {"text": ""}}]
    description = description[0]["node"]["text"]
    description = description[:200] + "..." if len(description) > 200 else description
    return JSONResponse(
        {
            "author_name": description,
            "author_url": f"https://instagram.com/p/{post_id}",
            "provider_name": "InstaFix - Embed Instagram videos and images",
            "provider_url": "https://github.com/Wikidepia/InstaFix",
            "title": "Instagram",
            "type": "link",
            "version": "1.0",
        }
    )


@app.get("/grid/{post_id}")
async def grid(request: Request, post_id: str):
    client = request.app.state.client
    if os.path.exists(f"static/grid:{post_id}.webp"):
        return FileResponse(
            f"static/grid:{post_id}.webp",
            media_type="image/webp",
        )

    async def download_image(url):
        return (await client.get(url)).content

    data = await get_data(post_id)
    if "error" in data:
        return FileResponse("templates/404.html", status_code=404)
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
    if media_imgs == []:
        return FileResponse("templates/404.html", status_code=404)
    media_vips = [
        pyvips.Image.new_from_buffer(img, "", access="sequential") for img in media_imgs
    ]
    accross = min(len(media_imgs), 2)
    grid_img = pyvips.Image.arrayjoin(media_vips, across=accross, shim=10)
    grid_img.write_to_file(f"static/grid:{post_id}.webp")

    return FileResponse(
        f"static/grid:{post_id}.webp",
        media_type="image/webp",
    )

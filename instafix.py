import asyncio
import os
import random
import re
from typing import Optional
from urllib.parse import urljoin

import esprima
import httpx
import orjson
import pyvips
import sentry_sdk
import tenacity
from blacksheep import Application, Request, file, html, json, redirect
from blacksheep.server.templating import use_templates
from diskcache import Cache
from jinja2 import FileSystemLoader
from selectolax.lexbor import LexborHTMLParser

pyvips.cache_set_max(0)
pyvips.cache_set_max_mem(0)
pyvips.cache_set_max_files(0)
os.makedirs("static", exist_ok=True)

NoneType = type(None)
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
if "WORKER_PROXY" in os.environ:
    print("Using worker proxy:", os.environ["WORKER_PROXY"])

app = Application()
view = use_templates(app, loader=FileSystemLoader("templates"))


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
    data = cache.get(post_id)
    if not data:
        data = await _get_data(post_id)
        cache.set(post_id, data, expire=24 * 60 * 60)
    data = data.get("data", data)
    return data


@tenacity.retry(stop=tenacity.stop_after_attempt(3), wait=tenacity.wait_fixed(1))
async def _get_data(post_id: str) -> Optional[dict]:
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
        gql_data = orjson.loads(data[0])
        if gql_data and gql_data.get("shortcode_media"):
            return gql_data

    # TimeSliceImpl
    data = re.findall(r'<script>(requireLazy\(\["TimeSliceImpl".*)<\/script>', api_resp)
    if data and "shortcode_media" in data[0]:
        tokenized = esprima.tokenize(data[0])
        for token in tokenized:
            if "shortcode_media" in token.value:
                # orjson.loads to unescape the JSON
                return orjson.loads(orjson.loads(token.value))["gql_data"]

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
    tree = LexborHTMLParser(html)
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
    resp = await client.get(f"https://www.instagram.com/p/{post_id}/")
    if resp.status_code != 200:
        return {"error": "Not found"}

    tree = LexborHTMLParser(resp.text)
    json_ld = tree.css_first("script[type='application/ld+json']")
    if not json_ld:
        return {"error": "Server is blocked from Instagram"}

    json_ld = orjson.loads(json_ld.text())
    if isinstance(json_ld, list):
        json_ld = json_ld[0]

    # Weird bug, need to use isinstance :shrug:
    username = "unknown"
    if isinstance(json_ld["author"], NoneType):
        username = json_ld["author"].get("identifier", {}).get("value", "unknown")

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
    params = {
        "query_hash": "b3055c01b4b222b8a47dc12b090e4e64",
        "variables": orjson.dumps({"shortcode": post_id}).decode(),
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


@app.on_start
async def startup(_):
    global cache, client, gql_client
    cache = Cache(
        "cache", size_limit=int(5e9), eviction_policy="least-recently-used"
    )  # Limit cache to 5GB
    limits = httpx.Limits(max_keepalive_connections=None, max_connections=None)
    client = httpx.AsyncClient(
        headers=headers,
        follow_redirects=True,
        timeout=120.0,
        limits=limits,
        proxies={"all://www.instagram.com": os.environ.get("EMBED_PROXY")},
    )
    # GraphQL are constantly blocked,
    # it needs to use a different proxy (residential preferred)
    gql_client = httpx.AsyncClient(
        headers=headers,
        follow_redirects=True,
        timeout=5.0,
        limits=limits,
        proxies={"all://www.instagram.com": os.environ.get("GRAPHQL_PROXY")},
    )


@app.on_stop
async def shutdown(_):
    await client.aclose()
    await gql_client.aclose()


@app.route("/")
def root():
    with open("templates/home.html") as f:
        home_html = f.read()
    return html(value=home_html)


@app.route("/p/{post_id}")
@app.route("/p/{post_id}/{num}")
@app.route("/reel/{post_id}")
@app.route("/reels/{post_id}")
@app.route("/tv/{post_id}")
@app.route("/stories/{username}/{post_id}")
async def read_item(request: Request, post_id: str, num: Optional[int] = None):
    if b"/stories/" in request.url.path:
        if not post_id.isdigit():
            return file("templates/404.html")
        post_id = mediaid_to_code(int(post_id))

    user_agent = request.headers.get_first(b"User-Agent") or b""
    post_url = urljoin("https://www.instagram.com/", request.url.path.decode())
    if not re.search(
        rb"bot|facebook|embed|got|firefox\/92|firefox\/38|curl|wget|go-http|yahoo|generator|whatsapp|preview|link|proxy|vkshare|images|analyzer|index|crawl|spider|python|cfnetwork|node",
        user_agent.lower(),
    ):
        return redirect(post_url)

    data = await get_data(post_id)
    if "error" in data:
        ctx = {
            "title": "InstaFix",
            "url": post_url,
            "description": "Sorry, this post isn't available.",
        }
        return view("base", ctx)

    item = data["shortcode_media"]
    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        if isinstance(num, int) and num > len(media_lst):
            return file("templates/404.html")
        media = (media_lst[num - 1 if num else 0])["node"]
    else:
        media = item

    typename = media.get("node", media)["__typename"]
    description = item["edge_media_to_caption"]["edges"] or [{"node": {"text": ""}}]
    description = description[0]["node"]["text"]
    description = description[:200] + "..." if len(description) > 200 else description

    ctx = {
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
    return view("base", ctx)


@app.route("/videos/{post_id}/{num}")
async def videos(post_id: str, num: int):
    data = await get_data(post_id)
    if "error" in data:
        return file("templates/404.html")
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        if num > len(media_lst):
            return file("templates/404.html")
        media = media_lst[num - 1] if num else media_lst[0]
    else:
        media = item

    media = media.get("node", media)
    video_url = media.get("video_url", media["display_url"])

    # Proxy video via CF worker because Instagram speed limit
    worker_proxy = os.environ.get("WORKER_PROXY")
    if worker_proxy:
        wproxy = random.choice(worker_proxy.split(","))
        video_url = urljoin(wproxy, video_url)
    return redirect(video_url)


@app.route("/images/{post_id}/{num}")
async def images(post_id: str, num: int):
    data = await get_data(post_id)
    if "error" in data:
        return file("templates/404.html")
    item = data["shortcode_media"]

    if "edge_sidecar_to_children" in item:
        media_lst = item["edge_sidecar_to_children"]["edges"]
        if num > len(media_lst):
            return file("templates/404.html")
        media = media_lst[num - 1] if num else media_lst[0]
    else:
        media = item

    media = media.get("node", media)
    image_url = media["display_url"]
    return redirect(image_url)


@app.route("/oembed.json")
async def oembed(post_id: str):
    data = await get_data(post_id)
    if "error" in data:
        return file("templates/404.html")
    item = data["shortcode_media"]
    description = item["edge_media_to_caption"]["edges"] or [{"node": {"text": ""}}]
    description = description[0]["node"]["text"]
    description = description[:200] + "..." if len(description) > 200 else description
    return json(
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


@app.route("/grid/{post_id}")
async def grid(post_id: str):
    if os.path.exists(f"static/grid:{post_id}.webp"):
        return file(
            f"static/grid:{post_id}.webp",
            content_type="image/webp",
        )

    async def download_image(url):
        return (await client.get(url)).content

    data = await get_data(post_id)
    if "error" in data:
        return file("templates/404.html")
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

    if len(media_urls) == 1:
        return redirect(media_urls[0])

    # Download images and merge them into a single image
    media_imgs = await asyncio.gather(*[download_image(url) for url in media_urls])
    if media_imgs == []:
        return file("templates/404.html")
    media_vips = [
        pyvips.Image.new_from_buffer(img, "", access="sequential") for img in media_imgs
    ]
    accross = min(len(media_imgs), 2)
    grid_img = pyvips.Image.arrayjoin(media_vips, across=accross, shim=10)
    grid_img.write_to_file(f"static/grid:{post_id}.webp")

    return file(
        f"static/grid:{post_id}.webp",
        content_type="image/webp",
    )


@app.route("/health")
def healthcheck():
    return "200"

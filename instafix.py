import re
from http.cookiejar import MozillaCookieJar
from typing import Optional

import orjson
import redis
import requests
from fastapi import FastAPI, Request
from fastapi.responses import HTMLResponse, RedirectResponse
from fastapi.templating import Jinja2Templates
from lxml import html

app = FastAPI()
templates = Jinja2Templates(directory="templates")

r = redis.Redis(decode_responses=True)
cookies = MozillaCookieJar("cookies.txt")
cookies.load()

CRAWLER_UA = {
    "facebookexternalhit/1.1",
    "TelegramBot (like TwitterBot)",
    "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)",
    "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)",
    "Mozilla/5.0 (compatible; January/1.0; +https://gitlab.insrt.uk/revolt/january)",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.10; rv:38.0) Gecko/20100101 Firefox/38.0",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_1) AppleWebKit/601.2.4 (KHTML, like Gecko) Version/9.0.1 Safari/601.2.4 facebookexternalhit/1.1 Facebot Twitterbot/1.0",
}


headers = {
    "authority": "www.instagram.com",
    "accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9",
    "accept-language": "en-US,en;q=0.9",
    "cache-control": "no-cache",
    "pragma": "no-cache",
    "sec-ch-ua": '" Not A;Brand";v="99", "Chromium";v="100"',
    "sec-ch-ua-mobile": "?0",
    "sec-ch-ua-platform": '"macOS"',
    "sec-fetch-dest": "document",
    "sec-fetch-mode": "navigate",
    "sec-fetch-site": "none",
    "sec-fetch-user": "?1",
    "referer": "https://www.instagram.com/",
    "upgrade-insecure-requests": "1",
    "user-agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.60 Safari/537.36",
}


def get_data(url):
    response = requests.get(url, headers=headers, cookies=cookies)
    tree = html.fromstring(response.text)
    for script in tree.xpath("//script"):
        text = script.text or ""
        if "device_timestamp" not in text:
            continue
        # Remove window.__additionalDataLoaded
        data = re.sub(r"window.__additionalDataLoaded\('/p/.*/',", "", text)
        # Remove trailing ');'
        data = data.rstrip(");")
        break
    data = orjson.loads(data)
    return data


@app.get("/p/{post_id}", response_class=HTMLResponse)
@app.get("/p/{post_id}/{num}", response_class=HTMLResponse)
@app.get("/reel/{post_id}", response_class=HTMLResponse)
@app.get("/tv/{post_id}", response_class=HTMLResponse)
def read_item(request: Request, post_id: str, num: Optional[int] = 1):
    post_url = f"https://instagram.com/p/{post_id}"
    if request.headers.get("User-Agent") not in CRAWLER_UA:
        return RedirectResponse(post_url, status_code=302)

    item = r.get(post_id)
    if item is not None:
        item = orjson.loads(item)
    else:
        data = get_data(post_url)
        item = data["items"][0]
        r.set(post_id, orjson.dumps(item), ex=3600)

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1]
    image = media["image_versions2"]["candidates"][0]
    image_url = image["url"]

    description = item["caption"]["text"]
    full_name = item["user"]["full_name"]
    username = item["user"]["username"]

    ctx = {
        "request": request,
        "url": post_url,
        "description": description,
        "full_name": full_name,
        "username": username,
    }

    if "video_versions" in media:
        video = media["video_versions"][-1]
        ctx["video"] = f"/videos/{post_id}/{num}"
        ctx["width"] = video["width"]
        ctx["height"] = video["height"]
        ctx["card"] = "player"
    else:
        ctx["image"] = image_url
        ctx["width"] = image["width"]
        ctx["height"] = image["height"]
        ctx["card"] = "summary_large_image"

    return templates.TemplateResponse("base.html", ctx)


@app.get("/videos/{post_id}/{num}")
def videos(post_id: str, num: int):
    item = orjson.loads(r.get(post_id))
    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1]
    video_url = media["video_versions"][-1]["url"]
    return RedirectResponse(video_url, status_code=302)

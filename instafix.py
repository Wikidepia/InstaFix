import re
from http.cookiejar import MozillaCookieJar
from typing import Optional

import orjson
import redis
import requests
from fastapi import FastAPI, Request
from fastapi.responses import HTMLResponse, RedirectResponse
from fastapi.templating import Jinja2Templates

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


def get_data(post_id):
    post_url = f"https://instagram.com/p/{post_id}"
    data = r.get(post_id)
    if data is None:
        post_resp = requests.get(post_url, cookies=cookies, headers=headers)
        media_id = re.search(r'"media_id":"(\d+)"', post_resp.text).group(1)
        api_resp = requests.get(
            f"https://i.instagram.com/api/v1/media/{media_id}/info/",
            cookies=cookies,
            headers=headers,
        )
        data = api_resp.text
        r.set(post_id, data, ex=12 * 3600)

    data = orjson.loads(data)
    return data


@app.get("/", response_class=HTMLResponse)
def root():
    with open("templates/home.html") as f:
        html = f.read()
    return HTMLResponse(content=html)


@app.get("/p/{post_id}", response_class=HTMLResponse)
@app.get("/p/{post_id}/{num}", response_class=HTMLResponse)
@app.get("/reel/{post_id}", response_class=HTMLResponse)
@app.get("/tv/{post_id}", response_class=HTMLResponse)
def read_item(request: Request, post_id: str, num: Optional[int] = 1):
    post_url = f"https://instagram.com/p/{post_id}"
    if request.headers.get("User-Agent") not in CRAWLER_UA:
        return RedirectResponse(post_url, status_code=302)

    data = get_data(post_id)
    item = data["items"][0]

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1]

    description = item["caption"]["text"] if item["caption"] != None else ""
    full_name = item["user"]["full_name"]
    username = item["user"]["username"]

    ctx = {
        "request": request,
        "url": post_url,
        "description": description,
        "full_name": full_name,
        "username": username,
        "media_num": num,
        "media_total": len(media_lst),
    }

    if "video_versions" in media:
        video = media["video_versions"][-1]
        ctx["video"] = f"/videos/{post_id}/{num}"
        ctx["width"] = video["width"]
        ctx["height"] = video["height"]
        ctx["card"] = "player"
    else:
        image = media["image_versions2"]["candidates"][0]
        ctx["image"] = f"/images/{post_id}/{num}"
        ctx["width"] = image["width"]
        ctx["height"] = image["height"]
        ctx["card"] = "summary_large_image"

    return templates.TemplateResponse("base.html", ctx)


@app.get("/videos/{post_id}/{num}")
def videos(post_id: str, num: int):
    data = get_data(post_id)
    item = data["items"][0]

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1]
    video_url = media["video_versions"][0]["url"]
    return RedirectResponse(video_url, status_code=302)


@app.get("/images/{post_id}/{num}")
def images(post_id: str, num: int):
    data = get_data(post_id)
    item = data["items"][0]

    media_lst = item["carousel_media"] if "carousel_media" in item else [item]
    media = media_lst[num - 1]
    image_url = media["image_versions2"]["candidates"][0]["url"]
    return RedirectResponse(image_url, status_code=302)

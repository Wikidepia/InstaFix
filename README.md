# InstaFix

**STATUS: Currently Broken**

> Instagram is a trademark of Instagram, Inc. This app is not affiliated with Instagram, Inc.

InstaFix serves fixed Instagram image and video embeds. Heavily inspired by [fxtwitter.com](https://github.com/robinuniverse/TwitFix).

## How to use

Add `dd` before `instagram.com` to show Instagram embeds.

## Deploy InstaFix yourself

1. Clone the repository.
2. Install [Poetry](https://python-poetry.org/docs/).
3. Run `poetry install` in the root directory.
4. Deploy with gunicorn or similar server. See [FastAPI Server Workers](https://fastapi.tiangolo.com/deployment/server-workers/).

## Report a bug

You could open an [issue](https://github.com/Wikidepia/InstaFix/issues).

## TODO

- [x] Find a way to get data without cookies & rate limit.
- [x] Add support for stories.
- [ ] Bypass "Watch on Instagram" button.
- [ ] Add multiple third-party website to download videos

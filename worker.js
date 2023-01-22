addEventListener("fetch", event => {
    event.respondWith(stream(event.request))
});

async function stream(request) {
    const {
        searchParams
    } = new URL(request.url)
    let url = searchParams.get('url')
    let referer = searchParams.get('referer')

    // Fetch from origin server.
    let response = await fetch(url, {
        "headers": {
            "accept": "*/*",
            "accept-language": "id-ID,id",
            "range": "bytes=0-",
            "referer": referer,
            "sec-ch-ua": "\" Not;A Brand\";v=\"99\", \"Google Chrome\";v=\"109\", \"Chromium\";v=\"109\"",
            "sec-ch-ua-mobile": "?0",
            "sec-ch-ua-platform": "\"macOS\"",
            "sec-fetch-dest": "video",
            "sec-fetch-mode": "no-cors",
            "sec-fetch-site": "cross-site"
        },
        "body": null,
        "method": "GET",
    });

    const responseInit = {
        headers: {
            'Content-Type': 'video/mp4',
            'Content-Disposition': 'attachment; filename="video.mp4"'
        }
    };

    return new Response(response.body, responseInit)
}
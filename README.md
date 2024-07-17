# InstaFix

> Instagram is a trademark of Instagram, Inc. This app is not affiliated with Instagram, Inc.

InstaFix serves fixed Instagram image and video embeds. Heavily inspired by [fxtwitter.com](https://fxtwitter.com).

## How to use

Add `dd` before `instagram.com` to show Instagram embeds, or

<img src=".github/assets/orig_embed.jpg" width="450">

### Embed Media Only

Add `d.dd` before `instagram.com` to show only the media.

<img src=".github/assets/media_only.jpg" width="450">

### Gallery View

Add `g.dd` before `instagram.com` to show only the author and the media, without any caption.

<img src=".github/assets/no_caption.jpg" width="450">

## Deploy InstaFix yourself (locally)

1. Clone the repository.
2. Install [Go](https://golang.org/doc/install).
3. Run `go build`.
4. Run `./instafix`.

## Deploy InstaFix yourself (cloud)

1. Pull the latest container image from GHCR and run it.  
   `docker pull ghcr.io/wikidepia/instafix:main`
2. Run the pulled image with Docker (bound on port 3000):  
    `docker run -d --restart=always -p 3000:3000 ghcr.io/wikidepia/instafix:main`
3. Optional: Use the Docker Compose file in [./scripts/docker-compose.yml](./scripts/docker-compose.yml).
4. Optional: Use a [Kubernetes Deployment file](./scripts/k8s/instafix-deployment.yaml) and a [Kubernetes Ingress configuration file](./scripts/k8s/instafix-ingress.yaml) to deploy to a Kubernetes cluster (with 10 replicas) by issuing `kubectl apply -f .` over the `./scripts/k8s/` folder. [TODO: CockroachDB is not shared between replicas at application level, extract Cockroach into its own Service and allow replicas to communicate to it].

## Using iOS shortcut (contributed by @JohnMcAnearney)
You can use the iOS shortcut found here: [Embed in Discord](https://www.icloud.com/shortcuts/3412a4c344fd4c6f99924e525dd3c0a2), in order to quickly embed content using InstaFix. The shortcut works by grabbing the url of the Instagram content you're trying to share, automatically appends 'dd' to where it needs to be, copies this to your device's clipboard and opens Discord. 

Note: Once you've downloaded the shortcut, you will need to: 
1. Update the value of YOUR_SERVER_URL_HERE by opening the shortcut in the Shortcuts App. The value for this can be a direct message URL or a server URL.
2. The shortcut will already be available in your share sheet. To test, go to Instagram -> share -> Tap on "Embed in Discord".
3. This will open Discord and now you simply paste the text into the custom chat you've setup!
4. Now edit your sharesheet to make it even quicker to use! Simply open your share sheet by sharing something from Instagram, click "Edit Actions..." and click the "+" button on "Embed in Discord"

<p float="left">
<img src=".github/assets/Step1_image.jpg" width="140">
<img src=".github/assets/Step2_image.jpg" width="140">
<img src=".github/assets/Step4_image_a.jpg" width="140">
<img src=".github/assets/Step4_image_b.jpg" width="140">
</p>

## Report a bug

You could open an [issue](https://github.com/Wikidepia/InstaFix/issues).

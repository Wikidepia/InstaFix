# InstaFix

> Instagram is a trademark of Instagram, Inc. This app is not affiliated with Instagram, Inc.

InstaFix serves fixed Instagram image and video embeds. Heavily inspired by [fxtwitter.com](https://fxtwitter.com).

## How to use

Add `dd` before `instagram.com` to show Instagram embeds.

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

## Report a bug

You could open an [issue](https://github.com/Wikidepia/InstaFix/issues).

# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.23 as app-builder

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/engine/reference/builder/#copy
COPY *.go ./
COPY handlers/ ./handlers/
COPY handlers/scraper/ ./handlers/scraper/
# NO-OP in case handlers/scraper/ was already copied previously
RUN true
COPY utils/ ./utils/
COPY views/ ./views/

# This is the architecture you’re building for, which is passed in by the builder.
# Placing it here allows the previous steps to be cached across architectures.
ARG TARGETARCH

# Build
RUN GOOS=linux GOARCH=$TARGETARCH go build -tags netgo,osusergo -ldflags '-extldflags "-static"'

# Run in scratch container
FROM scratch
# the test program:
COPY --from=app-builder /app/instafix /instafix
# the tls certificates:
# NB: this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Optional:
# To bind to a TCP port, runtime parameters must be supplied to the docker command.
# But we can document in the Dockerfile what ports
# the application is going to listen on by default.
# https://docs.docker.com/engine/reference/builder/#expose
EXPOSE 3000

# Run the app
ENTRYPOINT ["/instafix"]

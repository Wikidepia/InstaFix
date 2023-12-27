# syntax=docker/dockerfile:1

FROM golang:alpine as app-builder

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/engine/reference/builder/#copy
COPY *.go ./
COPY handlers/ ./handlers/
COPY handlers/data/ ./handlers/data/
# NO-OP in case handlers/data/ was already copied previously
RUN true
COPY utils/ ./utils/
COPY views/ ./views/

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags '-extldflags "-static"'

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

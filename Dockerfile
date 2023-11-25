# syntax=docker/dockerfile:1

FROM golang:1.21

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
RUN CGO_ENABLED=0 GOOS=linux go build

# Optional:
# To bind to a TCP port, runtime parameters must be supplied to the docker command.
# But we can document in the Dockerfile what ports
# the application is going to listen on by default.
# https://docs.docker.com/engine/reference/builder/#expose
EXPOSE 3000

# Run
ENTRYPOINT ["/app/instafix"]

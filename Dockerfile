# --- build stage ---
FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gitshelf ./cmd/gitshelf

# --- runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates && adduser -D -u 10001 gitshelf
COPY --from=build /out/gitshelf /usr/local/bin/gitshelf
USER gitshelf
EXPOSE 8888
# Mount your mirrors read-only and provide a config:
#   docker run -p 8888:8888 \
#     -v /path/to/mirrors:/mirrors:ro \
#     -v /path/to/meta:/meta:ro \
#     -v /path/to/gitshelf.toml:/etc/gitshelf.toml:ro \
#     ghcr.io/loping151/gitshelf -config /etc/gitshelf.toml -bind 0.0.0.0:8888
ENTRYPOINT ["gitshelf"]
CMD ["-config", "/etc/gitshelf.toml", "-bind", "0.0.0.0:8888"]

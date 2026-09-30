# One image with the api and worker binaries plus goose for migrations.
# The compose file picks the command.
FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/... \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goose github.com/pressly/goose/v3/cmd/goose

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -u 10001 app
COPY --from=build /out/ /app/
COPY migrations /app/migrations
USER app
WORKDIR /app
EXPOSE 8080
CMD ["/app/api"]

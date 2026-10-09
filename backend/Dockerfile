# The api binary plus goose and the migrations, for Cloud Run. The api listens on $PORT
# when it is set.
FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goose github.com/pressly/goose/v3/cmd/goose

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 app
COPY --from=build /out/ /app/
COPY migrations /app/migrations
USER app
WORKDIR /app
EXPOSE 8080
CMD ["/app/api"]

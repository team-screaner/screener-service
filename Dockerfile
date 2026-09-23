FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/screener ./cmd/screener && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S screener && adduser -S -G screener screener
WORKDIR /app
COPY --from=build /out/screener /out/migrate ./
COPY --from=build /src/migrations ./migrations
USER screener
ENV HTTP_ADDR=:8080 MIGRATIONS_DIR=/app/migrations
EXPOSE 8080
CMD ["/app/screener"]

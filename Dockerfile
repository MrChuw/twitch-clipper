# Estágio 1: Compilação (Builder)
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /twitch-clipper .

FROM alpine:3.21

WORKDIR /app

RUN apk add --no-cache ffmpeg

COPY --from=builder /twitch-clipper .

CMD ["./twitch-clipper"]
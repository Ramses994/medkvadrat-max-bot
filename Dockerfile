FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# modernc.org/sqlite — pure Go, поэтому CGO_ENABLED=0 работает
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o medkvadrat-max-bot .

FROM alpine:latest

RUN apk --no-cache add tzdata ca-certificates
ENV TZ=Europe/Moscow

WORKDIR /app

COPY --from=builder /app/medkvadrat-max-bot .

# Том для SQLite-файла, чтобы переживал рестарты контейнера
RUN mkdir -p /app/data && chown nobody:nobody /app/data
VOLUME ["/app/data"]

USER nobody:nobody

CMD ["./medkvadrat-max-bot"]

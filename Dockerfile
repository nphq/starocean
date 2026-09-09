# syntax=docker/dockerfile:1

FROM golang:1.26.3-alpine AS builder

RUN apk add --no-cache git nodejs npm

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY package.json ./
RUN npm install

COPY . .

RUN npx @tailwindcss/cli -i ./public/css/tailwind.css -o ./public/css/output.css --minify

RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020 && templ generate
RUN CGO_ENABLED=0 go build -o starocean .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/starocean .

# 单机 Turso 文件库默认数据目录（755 目录需可写，避免 root 属主导致建库失败）
RUN mkdir -p /data && chown 65532:65532 /data
ENV DATABASE_URL="sqlite:/data/starocean.db"

USER 65532:65532

EXPOSE 8080
ENTRYPOINT ["./starocean"]
CMD ["serve"]

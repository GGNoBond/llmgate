FROM  golang:1.27-alpine AS builder
WORKDIR /build

COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/bin/llmgate ./src


FROM alpine:3.20
RUN apk add  --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /build/bin/llmgate /app/llmgate
COPY config/ /app/config/

ENV LLMGATE_ROOT=/app
ENV LLMGATE_CONFIG=config/config.docker.yaml
EXPOSE 8080
ENTRYPOINT ["/app/llmgate"]

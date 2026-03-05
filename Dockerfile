# 構建階段
FROM golang:1.25-alpine AS builder

WORKDIR /build

# 複製 go mod 文件
COPY go.mod go.sum ./
RUN go mod download

# 複製源代碼和配置文件
COPY . .


# 構建應用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# 運行階段
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

# 從構建階段複製二進制文件
COPY --from=builder /build/main .

# 複製配置文件目錄（重要！）
COPY --from=builder /build/config ./config

# 暴露端口
EXPOSE 8002

# 運行應用
CMD ["./main"]


FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /stubr ./cmd/stubr

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /stubr /usr/local/bin/stubr
EXPOSE 8080
ENTRYPOINT ["stubr"]

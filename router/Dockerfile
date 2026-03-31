# Build stage
FROM golang:1.25 AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o router main.go

# Runtime stage
FROM gcr.io/distroless/base-debian12

WORKDIR /
COPY --from=builder /app/router /router

EXPOSE 9000

ENTRYPOINT ["/router"]
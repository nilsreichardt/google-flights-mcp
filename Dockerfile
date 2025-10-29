# syntax=docker/dockerfile:1

FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /workspace/mcp-server ./cmd/mcp-server

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /srv

COPY --from=builder /workspace/mcp-server /srv/mcp-server

EXPOSE 8080

ENV HOST=0.0.0.0 PORT=8080

ENTRYPOINT ["/srv/mcp-server"]

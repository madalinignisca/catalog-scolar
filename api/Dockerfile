FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /catalogro-api ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /catalogro-api /catalogro-api

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/catalogro-api"]

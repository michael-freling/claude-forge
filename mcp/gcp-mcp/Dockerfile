# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gcp-mcp .

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /gcp-mcp /usr/local/bin/gcp-mcp
RUN adduser -D -s /bin/sh user
USER user
ENTRYPOINT ["gcp-mcp"]

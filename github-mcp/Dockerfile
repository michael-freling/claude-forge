# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /github-mcp .

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache bash
COPY --from=build /github-mcp /usr/local/bin/github-mcp
RUN adduser -D -s /bin/bash user
USER user
ENTRYPOINT ["github-mcp"]

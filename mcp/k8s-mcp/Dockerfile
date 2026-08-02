# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /k8s-mcp .

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /k8s-mcp /usr/local/bin/k8s-mcp
RUN adduser -D -s /bin/sh user
USER user
ENTRYPOINT ["k8s-mcp"]

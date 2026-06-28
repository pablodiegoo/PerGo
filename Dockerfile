# Stage 1: Build the Go binary
FROM golang:1.26.4-alpine AS builder

WORKDIR /app

# Install git, certificates, and templ CLI
RUN apk add --no-cache git ca-certificates
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020

# Copy all files (needed first due to local go.mod replaces)
COPY . .

RUN go mod download

# Generate templ files
RUN templ generate ./...

# Build the CGO-free static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o pergo ./cmd/pergo

# Stage 2: Minimal runtime image
FROM gcr.io/distroless/static-debian12:latest

WORKDIR /app

# Copy root CA certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary
COPY --from=builder /app/pergo .

# Copy static assets (needed for admin UI)
COPY --from=builder /app/static ./static

# Expose port
EXPOSE 8080

# Command to run the application
ENTRYPOINT ["/app/pergo"]

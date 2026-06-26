# Stage 1: Build the Go binary
FROM golang:1.26.4-alpine AS builder

WORKDIR /app

# Install git and certificates
RUN apk add --no-cache git ca-certificates

# Copy dependency files and download modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code (we assume templ files are already pre-generated and checked in)
COPY . .

# Build the CGO-free static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o omnigo ./cmd/omnigo

# Stage 2: Minimal runtime image
FROM gcr.io/distroless/static-debian12:latest

WORKDIR /app

# Copy root CA certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary
COPY --from=builder /app/omnigo .

# Copy static assets (needed for admin UI)
COPY --from=builder /app/static ./static

# Expose port
EXPOSE 8080

# Command to run the application
ENTRYPOINT ["/app/omnigo"]

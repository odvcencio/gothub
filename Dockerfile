# Stage 1: Build frontend
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build WASM + Go binary
FROM golang:1.24-alpine AS builder
RUN apk add --no-cache make
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy built frontend
COPY --from=frontend /app/frontend/dist ./frontend/dist
# Build WASM
RUN GOOS=js GOARCH=wasm go build -o frontend/dist/gothub.wasm ./wasm/
RUN cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" frontend/dist/wasm_exec.js
# Copy dist into embed location
RUN rm -rf internal/web/dist && cp -r frontend/dist internal/web/dist
# Build binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /gothub ./cmd/gothub

# Stage 3: Minimal runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /gothub /usr/local/bin/gothub
EXPOSE 3000
ENTRYPOINT ["gothub"]
CMD ["serve"]

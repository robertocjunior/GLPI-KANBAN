# Estágio de Compilação (Builder)
FROM golang:1.25-alpine AS builder

RUN apk update && apk upgrade --no-cache && \
    apk add --no-cache git build-base ca-certificates

WORKDIR /app

# Copia dependências primeiro
COPY go.mod go.sum* ./

# Download dos módulos
RUN CGO_ENABLED=0 go mod download || true

# Copia todo o código fonte
COPY . .

# Garante dependências resolvidas e compila binário estático
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o glpi-kanban ./cmd/server/main.go

# ---------------------------------------------------------

# Estágio Final (Runner leve e seguro)
FROM alpine:3.20

RUN apk update && apk upgrade --no-cache && \
    apk --no-cache add ca-certificates tzdata

ENV TZ=America/Sao_Paulo

WORKDIR /app

# Copia o binário compilado
COPY --from=builder /app/glpi-kanban .

# Cria diretórios de dados e logs
RUN mkdir -p data logs

EXPOSE 8081

ENTRYPOINT ["./glpi-kanban"]

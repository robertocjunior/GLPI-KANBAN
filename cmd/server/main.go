package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"glpi-kanban/internal/config"
	"glpi-kanban/internal/db"
	"glpi-kanban/internal/glpi"
	"glpi-kanban/internal/handler"
	"glpi-kanban/internal/logger"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrappedWriter := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrappedWriter, r)

		if r.URL.Path == "/api/health" {
			return
		}

		duration := time.Since(start)
		logLevel := slog.LevelInfo
		if wrappedWriter.status >= 500 {
			logLevel = slog.LevelError
		} else if wrappedWriter.status >= 400 {
			logLevel = slog.LevelWarn
		}

		slog.Log(r.Context(), logLevel, "HTTP Request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrappedWriter.status),
			slog.String("duration", duration.String()),
		)
	})
}

func main() {
	cfg := config.Load()
	logger.Init(cfg)

	slog.Info("Inicializando GLPI Kanban",
		"porta", cfg.Port,
		"glpi_url", cfg.GlpiURL,
		"tech_group", cfg.GlpiTechGroup,
		"my_user_id", cfg.MyUserID,
		"my_user_name", cfg.MyUserName,
		"db_path", cfg.DBPath,
	)

	// 1. Inicializa SQLite local
	store, err := db.Init(cfg.DBPath)
	if err != nil {
		slog.Error("Erro fatal ao inicializar banco de dados SQLite", "erro", err)
		os.Exit(1)
	}
	defer store.Close()

	// 2. Inicializa GLPI client (somente consulta, sem escritas no GLPI)
	glpiClient := glpi.NewClient(cfg)

	// 3. Inicializa Handler e rotas
	h := handler.New(cfg, store, glpiClient)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	finalHandler := loggingMiddleware(securityMiddleware(mux))

	serverAddr := ":" + cfg.Port
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: finalHandler,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		fmt.Printf("\n======================================================\n")
		fmt.Printf("   GLPI KANBAN (Sistema com SQLite Local)             \n")
		fmt.Printf("   Acesse em: http://localhost:%s                    \n", cfg.Port)
		fmt.Printf("   GLPI URL : %s                                     \n", cfg.GlpiURL)
		fmt.Printf("   Usuário  : %s (ID %d)                             \n", cfg.MyUserName, cfg.MyUserID)
		fmt.Printf("======================================================\n\n")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Erro no servidor HTTP", "erro", err)
			os.Exit(1)
		}
	}()

	sig := <-stopChan
	slog.Info("Sinal de encerramento recebido", "sinal", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	slog.Info("Encerrando servidor...")
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Erro ao encerrar servidor", "erro", err)
	}
	slog.Info("Servidor encerrado com sucesso.")
}

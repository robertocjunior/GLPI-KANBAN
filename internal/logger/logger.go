package logger

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"glpi-kanban/internal/config"

	"github.com/lmittmann/tint"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Init inicializa o logger global
func Init(cfg *config.Config) {
	logDir := "logs"
	_ = os.MkdirAll(logDir, 0755)

	maxSize := 50
	if cfg.LogMaxSize > 0 {
		maxSize = cfg.LogMaxSize
	}

	maxAge := 30
	if cfg.LogMaxAge > 0 {
		maxAge = cfg.LogMaxAge
	}

	fileWriter := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "glpi-kanban.log"),
		MaxSize:    maxSize,
		MaxAge:     maxAge,
		MaxBackups: 5,
		Compress:   true,
		LocalTime:  true,
	}

	fileHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{
					Key:   "time",
					Value: slog.StringValue(a.Value.Time().Format(time.RFC3339)),
				}
			}
			return a
		},
	})

	consoleHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.TimeOnly,
		AddSource:  false,
	})

	multiHandler := NewFanoutHandler(consoleHandler, fileHandler)
	slog.SetDefault(slog.New(multiHandler))

	slog.Info("Sistema de logs inicializado", "dir", logDir)
}

type FanoutHandler struct {
	handlers []slog.Handler
}

func NewFanoutHandler(handlers ...slog.Handler) *FanoutHandler {
	return &FanoutHandler{handlers: handlers}
}

func (h *FanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *FanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			_ = handler.Handle(ctx, r)
		}
	}
	return nil
}

func (h *FanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithAttrs(attrs)
	}
	return NewFanoutHandler(newHandlers...)
}

func (h *FanoutHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithGroup(name)
	}
	return NewFanoutHandler(newHandlers...)
}

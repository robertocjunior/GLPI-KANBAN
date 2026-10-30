package handler

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"glpi-kanban/internal/config"
	"glpi-kanban/internal/db"
	"glpi-kanban/internal/glpi"
	"glpi-kanban/web"
)

type ColumnDef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Color string `json:"color"`
}

var defaultColumns = []ColumnDef{
	{ID: "queue_sistemas", Title: "Fila Sistemas (Não Atribuídos)", Color: "#d83b01"},
	{ID: "assigned_me", Title: "Meus Chamados (Atribuídos)", Color: "#0078d4"},
	{ID: "in_progress", Title: "Em Andamento (Foco)", Color: "#008272"},
	{ID: "waiting", Title: "Aguardando / Bloqueado", Color: "#ffb900"},
	{ID: "testing", Title: "Validação / Testes", Color: "#8764b8"},
	{ID: "done", Title: "Concluído no Quadro", Color: "#107c41"},
}

type KanbanCard struct {
	glpi.Ticket
	ColumnID    string `json:"column_id"`
	Position    int    `json:"position"`
	CustomNotes string `json:"custom_notes"`
	Pinned      bool   `json:"pinned"`
	IsManual    bool   `json:"is_manual"`
}

type KanbanColumn struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Color string       `json:"color"`
	Count int          `json:"count"`
	Cards []KanbanCard `json:"cards"`
}

type BoardResponse struct {
	Columns            []KanbanColumn `json:"columns"`
	TotalTickets       int            `json:"total_tickets"`
	MyTicketsCount     int            `json:"my_tickets_count"`
	QueueCount         int            `json:"queue_count"`
	MyUserName         string         `json:"my_user_name"`
	RefreshIntervalSec int            `json:"refresh_interval_sec"`
	LastSync           string         `json:"last_sync"`
}

type Handler struct {
	cfg        *config.Config
	store      *db.Store
	glpiClient *glpi.Client
	cacheMu    sync.RWMutex
	lastBoard  *BoardResponse
	lastSync   time.Time
}

func New(cfg *config.Config, store *db.Store, glpiClient *glpi.Client) *Handler {
	return &Handler{
		cfg:        cfg,
		store:      store,
		glpiClient: glpiClient,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/board", h.handleGetBoard)
	mux.HandleFunc("POST /api/card/move", h.handleMoveCard)
	mux.HandleFunc("POST /api/card/notes", h.handleUpdateNotes)
	mux.HandleFunc("POST /api/card/reset", h.handleResetCard)
	mux.HandleFunc("POST /api/card/pin", h.handleTogglePin)
	mux.HandleFunc("GET /api/health", h.handleHealth)

	// Embedded Static UI
	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		slog.Error("Erro ao carregar staticFS embutido", "erro", err)
	} else {
		fileServer := http.FileServer(http.FS(staticFS))
		mux.Handle("/", fileServer)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "app": "glpi-kanban"})
}

func (h *Handler) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	board, err := h.loadBoard(ctx)
	if err != nil {
		slog.Error("Erro ao montar quadro kanban", "erro", err)
		http.Error(w, `{"error":"Falha ao consultar chamados no GLPI"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(board)
}

func (h *Handler) loadBoard(ctx context.Context) (*BoardResponse, error) {
	// 1. Busca chamados no GLPI (somente leitura)
	tickets, err := h.glpiClient.FetchAllRelevantTickets(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Busca estados locais do SQLite
	states, err := h.store.GetAllCardStates()
	if err != nil {
		slog.Warn("Erro ao buscar estados locais do SQLite, continuando sem persistência", "erro", err)
		states = make(map[int]*db.CardState)
	}

	// 3. Monta Colunas
	colMap := make(map[string]*KanbanColumn)
	columns := make([]KanbanColumn, 0, len(defaultColumns))

	for _, d := range defaultColumns {
		col := KanbanColumn{
			ID:    d.ID,
			Title: d.Title,
			Color: d.Color,
			Cards: make([]KanbanCard, 0),
		}
		columns = append(columns, col)
	}

	for i := range columns {
		colMap[columns[i].ID] = &columns[i]
	}

	myCount := 0
	queueCount := 0

	for _, t := range tickets {
		card := KanbanCard{
			Ticket: t,
		}

		if t.IsAssignedToMe {
			myCount++
		} else {
			queueCount++
		}

		// Checa persistência local no SQLite
		if cs, exists := states[t.ID]; exists && cs.ColumnID != "" {
			card.ColumnID = cs.ColumnID
			card.Position = cs.Position
			card.CustomNotes = cs.CustomNotes
			card.Pinned = cs.Pinned
			card.IsManual = true
		} else {
			// Regra padrão de colunas automáticas
			if t.IsAssignedToMe {
				card.ColumnID = "assigned_me"
			} else {
				card.ColumnID = "queue_sistemas"
			}
			card.IsManual = false
		}

		// Adiciona à coluna de destino
		targetCol, found := colMap[card.ColumnID]
		if !found {
			targetCol = colMap["queue_sistemas"]
			card.ColumnID = "queue_sistemas"
		}
		targetCol.Cards = append(targetCol.Cards, card)
	}

	// Ordena cards de cada coluna: fixados primeiro, depois por posição ou data de modificação
	for i := range columns {
		c := &columns[i]
		sort.SliceStable(c.Cards, func(a, b int) bool {
			// Fixados têm precedência
			if c.Cards[a].Pinned != c.Cards[b].Pinned {
				return c.Cards[a].Pinned
			}
			// Se ambos são manuais e têm posições diferentes
			if c.Cards[a].IsManual && c.Cards[b].IsManual && c.Cards[a].Position != c.Cards[b].Position {
				return c.Cards[a].Position < c.Cards[b].Position
			}
			// Fallback: mais recentes primeiro
			return c.Cards[a].DateMod > c.Cards[b].DateMod
		})
		c.Count = len(c.Cards)
	}

	now := time.Now().Format("02/01/2006 15:04:05")
	board := &BoardResponse{
		Columns:            columns,
		TotalTickets:       len(tickets),
		MyTicketsCount:     myCount,
		QueueCount:         queueCount,
		MyUserName:         h.cfg.MyUserName,
		RefreshIntervalSec: h.cfg.RefreshIntervalSec,
		LastSync:           now,
	}

	h.cacheMu.Lock()
	h.lastBoard = board
	h.lastSync = time.Now()
	h.cacheMu.Unlock()

	return board, nil
}

func (h *Handler) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketID int    `json:"ticket_id"`
		ColumnID string `json:"column_id"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Payload inválido"}`, http.StatusBadRequest)
		return
	}

	if req.TicketID <= 0 || strings.TrimSpace(req.ColumnID) == "" {
		http.Error(w, `{"error":"ticket_id e column_id obrigatórios"}`, http.StatusBadRequest)
		return
	}

	// Salva única e exclusivamente no SQLite local. NÃO altera nada no GLPI!
	if err := h.store.MoveCard(req.TicketID, req.ColumnID, req.Position); err != nil {
		slog.Error("Erro ao mover card no SQLite", "ticket_id", req.TicketID, "coluna", req.ColumnID, "erro", err)
		http.Error(w, `{"error":"Falha ao salvar posição no banco"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("Card movido localmente no Kanban", "ticket_id", req.TicketID, "nova_coluna", req.ColumnID, "pos", req.Position)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"ticket_id": req.TicketID,
		"column_id": req.ColumnID,
	})
}

func (h *Handler) handleUpdateNotes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketID int    `json:"ticket_id"`
		Notes    string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Payload inválido"}`, http.StatusBadRequest)
		return
	}

	if req.TicketID <= 0 {
		http.Error(w, `{"error":"ticket_id obrigatório"}`, http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateNotes(req.TicketID, req.Notes); err != nil {
		slog.Error("Erro ao salvar anotação do card no SQLite", "ticket_id", req.TicketID, "erro", err)
		http.Error(w, `{"error":"Falha ao atualizar anotação"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (h *Handler) handleResetCard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketID int `json:"ticket_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Payload inválido"}`, http.StatusBadRequest)
		return
	}

	if req.TicketID <= 0 {
		http.Error(w, `{"error":"ticket_id obrigatório"}`, http.StatusBadRequest)
		return
	}

	if err := h.store.ResetCard(req.TicketID); err != nil {
		slog.Error("Erro ao resetar card no SQLite", "ticket_id", req.TicketID, "erro", err)
		http.Error(w, `{"error":"Falha ao resetar card"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("Card resetado para posição automática do GLPI", "ticket_id", req.TicketID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (h *Handler) handleTogglePin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TicketID int `json:"ticket_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Payload inválido"}`, http.StatusBadRequest)
		return
	}

	pinned, err := h.store.TogglePin(req.TicketID)
	if err != nil {
		slog.Error("Erro ao alternar pin no SQLite", "ticket_id", req.TicketID, "erro", err)
		http.Error(w, `{"error":"Falha ao alternar pin"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "pinned": pinned})
}

package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

type CardState struct {
	TicketID    int       `json:"ticket_id"`
	ColumnID    string    `json:"column_id"`
	Position    int       `json:"position"`
	CustomNotes string    `json:"custom_notes"`
	Pinned      bool      `json:"pinned"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

func Init(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("falha ao criar diretório para banco SQLite: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar no SQLite: %w", err)
	}

	// Schema
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS card_states (
		ticket_id INTEGER PRIMARY KEY,
		column_id TEXT NOT NULL,
		position INTEGER NOT NULL DEFAULT 0,
		custom_notes TEXT DEFAULT '',
		pinned INTEGER DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_card_column ON card_states(column_id, position);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("falha ao inicializar schema do SQLite: %w", err)
	}

	slog.Info("Banco de dados SQLite inicializado com sucesso", "caminho", dbPath)
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) GetAllCardStates() (map[int]*CardState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT ticket_id, column_id, position, custom_notes, pinned, updated_at FROM card_states`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[int]*CardState)
	for rows.Next() {
		var cs CardState
		var pinnedInt int
		var updatedAtStr string

		if err := rows.Scan(&cs.TicketID, &cs.ColumnID, &cs.Position, &cs.CustomNotes, &pinnedInt, &updatedAtStr); err != nil {
			return nil, err
		}
		cs.Pinned = pinnedInt != 0
		if t, err := time.Parse("2006-01-02 15:04:05", updatedAtStr); err == nil {
			cs.UpdatedAt = t
		} else {
			cs.UpdatedAt = time.Now()
		}
		states[cs.TicketID] = &cs
	}
	return states, nil
}

func (s *Store) MoveCard(ticketID int, targetColumnID string, position int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO card_states (ticket_id, column_id, position, updated_at)
	VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(ticket_id) DO UPDATE SET
		column_id = excluded.column_id,
		position = excluded.position,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := s.db.Exec(query, ticketID, targetColumnID, position)
	return err
}

func (s *Store) UpdateNotes(ticketID int, notes string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO card_states (ticket_id, column_id, custom_notes, updated_at)
	VALUES (?, 'assigned_me', ?, CURRENT_TIMESTAMP)
	ON CONFLICT(ticket_id) DO UPDATE SET
		custom_notes = excluded.custom_notes,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := s.db.Exec(query, ticketID, notes)
	return err
}

func (s *Store) ResetCard(ticketID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM card_states WHERE ticket_id = ?`, ticketID)
	return err
}

func (s *Store) TogglePin(ticketID int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var currentPinned int
	err := s.db.QueryRow(`SELECT pinned FROM card_states WHERE ticket_id = ?`, ticketID).Scan(&currentPinned)
	newPinned := 1
	if err == nil && currentPinned == 1 {
		newPinned = 0
	}

	query := `
	INSERT INTO card_states (ticket_id, column_id, pinned, updated_at)
	VALUES (?, 'assigned_me', ?, CURRENT_TIMESTAMP)
	ON CONFLICT(ticket_id) DO UPDATE SET
		pinned = excluded.pinned,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err = s.db.Exec(query, ticketID, newPinned)
	return newPinned == 1, err
}

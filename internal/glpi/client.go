package glpi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"glpi-kanban/internal/config"
)

var statusMap = map[int]string{
	1: "Novo",
	2: "Atribuído",
	3: "Planejado",
	4: "Pendente",
	5: "Solucionado",
	6: "Fechado",
}

var priorityMap = map[int]string{
	1: "Muito baixa",
	2: "Baixa",
	3: "Média",
	4: "Alta",
	5: "Muito alta",
	6: "Maior",
}

type Ticket struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	StatusID       int    `json:"status_id"`
	Status         string `json:"status"`
	AssignedUserID int    `json:"assigned_user_id"`
	AssignedUser   string `json:"assigned_user"`
	IsAssignedToMe bool   `json:"is_assigned_to_me"`
	PriorityID     int    `json:"priority_id"`
	Priority       string `json:"priority"`
	UrgencyID      int    `json:"urgency_id"`
	Category       string `json:"category"`
	CategoryFull   string `json:"category_full"`
	TechnicalGroup string `json:"technical_group"`
	RequesterID    string `json:"requester_id"`
	RequesterUser  string `json:"requester_user"`
	RequesterGroup string `json:"requester_group"`
	DateCreated    string `json:"date_created"`
	DateCreatedBR  string `json:"date_created_br"`
	DateMod        string `json:"date_mod"`
	URL            string `json:"url"`
}

type Client struct {
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.Mutex
	userCache  map[string]string
}

func NewClient(cfg *config.Config) *Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   25 * time.Second,
		},
		userCache: make(map[string]string),
	}
}

// FetchAllRelevantTickets busca chamados da fila de Sistemas e chamados atribuídos ao usuário (somente leitura)
func (c *Client) FetchAllRelevantTickets(ctx context.Context) ([]Ticket, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiURL := fmt.Sprintf("%s/apirest.php", c.cfg.GlpiURL)

	// 1. Iniciar Sessão
	initReq, err := http.NewRequestWithContext(ctx, "GET", apiURL+"/initSession", nil)
	if err != nil {
		return nil, fmt.Errorf("erro criando requisição initSession: %w", err)
	}
	initReq.Header.Set("App-Token", c.cfg.GlpiAppToken)
	initReq.Header.Set("Authorization", fmt.Sprintf("user_token %s", c.cfg.GlpiUserToken))

	resp, err := c.httpClient.Do(initReq)
	if err != nil {
		return nil, fmt.Errorf("falha ao autenticar no GLPI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GLPI initSession falhou com status %d: %s", resp.StatusCode, string(body))
	}

	var sessionResp struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return nil, fmt.Errorf("erro decodificando token de sessão GLPI: %w", err)
	}
	sessionToken := sessionResp.SessionToken

	defer func() {
		killReq, _ := http.NewRequestWithContext(context.Background(), "GET", apiURL+"/killSession", nil)
		if killReq != nil {
			killReq.Header.Set("App-Token", c.cfg.GlpiAppToken)
			killReq.Header.Set("Session-Token", sessionToken)
			killResp, killErr := c.httpClient.Do(killReq)
			if killErr == nil {
				_ = killResp.Body.Close()
			}
		}
	}()

	// 2. Busca 1: Fila de Sistemas (filtro da URL do usuário)
	// field 12 == notold, field 8 == 1 (Sistemas), field 7 notcontains Holmes, Metadados, Movimentação Funcional
	qSystems := url.Values{}
	qSystems.Set("criteria[0][field]", "12")
	qSystems.Set("criteria[0][searchtype]", "equals")
	qSystems.Set("criteria[0][value]", "notold")

	qSystems.Set("criteria[1][field]", "8")
	qSystems.Set("criteria[1][searchtype]", "equals")
	qSystems.Set("criteria[1][value]", c.cfg.GlpiTechGroup)

	qSystems.Set("criteria[2][field]", "7")
	qSystems.Set("criteria[2][searchtype]", "notcontains")
	qSystems.Set("criteria[2][value]", "Holmes")

	qSystems.Set("criteria[3][field]", "7")
	qSystems.Set("criteria[3][searchtype]", "notcontains")
	qSystems.Set("criteria[3][value]", "Metadados")

	qSystems.Set("criteria[4][field]", "7")
	qSystems.Set("criteria[4][searchtype]", "notcontains")
	qSystems.Set("criteria[4][value]", "Movimentação Funcional")

	qSystems.Set("sort", "19") // Data de modificação
	qSystems.Set("order", "DESC")
	qSystems.Set("range", "0-200")

	forcedFields := []string{"1", "2", "3", "4", "5", "7", "8", "10", "12", "15", "19", "22", "71"}
	for i, f := range forcedFields {
		qSystems.Set(fmt.Sprintf("forcedisplay[%d]", i), f)
	}

	ticketsSystems, err := c.doSearch(ctx, apiURL, sessionToken, qSystems)
	if err != nil {
		slog.Warn("Erro ao buscar chamados da fila de Sistemas", "erro", err)
	}

	// 3. Busca 2: Chamados atribuídos a mim (caso haja algum fora da fila de sistemas ou filtros)
	qAssigned := url.Values{}
	qAssigned.Set("criteria[0][field]", "12")
	qAssigned.Set("criteria[0][searchtype]", "equals")
	qAssigned.Set("criteria[0][value]", "notold")

	qAssigned.Set("criteria[1][field]", "5") // Técnico atribuído
	qAssigned.Set("criteria[1][searchtype]", "equals")
	qAssigned.Set("criteria[1][value]", strconv.Itoa(c.cfg.MyUserID))

	qAssigned.Set("sort", "19")
	qAssigned.Set("order", "DESC")
	qAssigned.Set("range", "0-100")

	for i, f := range forcedFields {
		qAssigned.Set(fmt.Sprintf("forcedisplay[%d]", i), f)
	}

	ticketsAssigned, err := c.doSearch(ctx, apiURL, sessionToken, qAssigned)
	if err != nil {
		slog.Warn("Erro ao buscar chamados atribuídos ao usuário", "erro", err)
	}

	// Deduplica por ID
	ticketMap := make(map[int]Ticket)
	for _, t := range ticketsSystems {
		ticketMap[t.ID] = t
	}
	for _, t := range ticketsAssigned {
		ticketMap[t.ID] = t
	}

	all := make([]Ticket, 0, len(ticketMap))
	for _, t := range ticketMap {
		all = append(all, t)
	}

	// Ordena por data de modificação / ID decrescente
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].DateMod != all[j].DateMod {
			return all[i].DateMod > all[j].DateMod
		}
		return all[i].ID > all[j].ID
	})

	return all, nil
}

func (c *Client) doSearch(ctx context.Context, apiURL, sessionToken string, q url.Values) ([]Ticket, error) {
	searchEndpoint := fmt.Sprintf("%s/search/Ticket?%s", apiURL, q.Encode())
	searchReq, err := http.NewRequestWithContext(ctx, "GET", searchEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("erro criando requisição search: %w", err)
	}
	searchReq.Header.Set("App-Token", c.cfg.GlpiAppToken)
	searchReq.Header.Set("Session-Token", sessionToken)

	searchResp, err := c.httpClient.Do(searchReq)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar chamados GLPI: %w", err)
	}
	defer searchResp.Body.Close()

	if searchResp.StatusCode != http.StatusOK && searchResp.StatusCode != 206 {
		body, _ := io.ReadAll(searchResp.Body)
		return nil, fmt.Errorf("GLPI search retornou status %d: %s", searchResp.StatusCode, string(body))
	}

	var rawResult struct {
		TotalCount int                      `json:"totalcount"`
		Count      int                      `json:"count"`
		Data       []map[string]interface{} `json:"data"`
	}

	if err := json.NewDecoder(searchResp.Body).Decode(&rawResult); err != nil {
		return nil, fmt.Errorf("erro decodificando resposta: %w", err)
	}

	// Popula cache de emails/usuários
	for _, item := range rawResult.Data {
		uID := parseString(item["4"])
		uMail := parseString(item["22"])
		if uID != "" && uMail != "" {
			if _, exists := c.userCache[uID]; !exists {
				c.userCache[uID] = formatDisplayName(uMail)
			}
		}
	}

	tickets := make([]Ticket, 0, len(rawResult.Data))
	for _, item := range rawResult.Data {
		idVal := parseNumber(item["2"])
		statusVal := parseNumber(item["12"])
		priorityVal := parseNumber(item["3"])
		urgencyVal := parseNumber(item["10"])

		cat := decodeHTMLEntities(parseString(item["7"]))
		reqGrp := decodeHTMLEntities(parseString(item["71"]))
		catClean := extractLastCategory(cat)
		reqGrpClean := strings.TrimPrefix(reqGrp, "Departamentos > ")

		rawDateCreated := parseString(item["15"])
		dateCreatedBR := formatDateBR(rawDateCreated)

		reqMail := parseString(item["22"])
		reqID := parseString(item["4"])
		reqUserDisplayName := "-"
		if reqMail != "" {
			reqUserDisplayName = formatDisplayName(reqMail)
			if reqID != "" {
				c.userCache[reqID] = reqUserDisplayName
			}
		} else if reqID != "" {
			reqUserDisplayName = c.getUserName(ctx, sessionToken, reqID)
		}

		// Extrai primeiro ID de técnico atribuído (field 5)
		firstTechIDStr := parseFirstTechID(item["5"])
		firstTechID, _ := strconv.Atoi(firstTechIDStr)
		assignedUserName := "-"
		if firstTechIDStr != "" {
			assignedUserName = c.getUserName(ctx, sessionToken, firstTechIDStr)
		}

		isAssignedToMe := false
		if firstTechID == c.cfg.MyUserID {
			isAssignedToMe = true
		} else if c.cfg.MyUserName != "" && strings.Contains(strings.ToLower(assignedUserName), strings.ToLower(c.cfg.MyUserName)) {
			isAssignedToMe = true
		}

		ticketURL := fmt.Sprintf("%s/front/ticket.form.php?id=%d", c.cfg.GlpiURL, idVal)

		tickets = append(tickets, Ticket{
			ID:             idVal,
			Title:          decodeHTMLEntities(parseString(item["1"])),
			StatusID:       statusVal,
			Status:         mapStatus(statusVal),
			AssignedUserID: firstTechID,
			AssignedUser:   assignedUserName,
			IsAssignedToMe: isAssignedToMe,
			PriorityID:     priorityVal,
			Priority:       mapPriority(priorityVal),
			UrgencyID:      urgencyVal,
			Category:       catClean,
			CategoryFull:   cat,
			TechnicalGroup: parseString(item["8"]),
			RequesterID:    reqID,
			RequesterUser:  reqUserDisplayName,
			RequesterGroup: reqGrpClean,
			DateCreated:    rawDateCreated,
			DateCreatedBR:  dateCreatedBR,
			DateMod:        parseString(item["19"]),
			URL:            ticketURL,
		})
	}

	return tickets, nil
}

func mapStatus(status int) string {
	if s, ok := statusMap[status]; ok {
		return s
	}
	return fmt.Sprintf("Status %d", status)
}

func mapPriority(priority int) string {
	if p, ok := priorityMap[priority]; ok {
		return p
	}
	return fmt.Sprintf("Prioridade %d", priority)
}

func parseString(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseNumber(val interface{}) int {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func decodeHTMLEntities(s string) string {
	for i := 0; i < 3 && strings.Contains(s, "&"); i++ {
		decoded := html.UnescapeString(s)
		if decoded == s {
			break
		}
		s = decoded
	}
	return s
}

func extractLastCategory(cat string) string {
	cat = strings.TrimSpace(cat)
	if cat == "" {
		return ""
	}
	parts := strings.Split(cat, ">")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part != "" {
			return part
		}
	}
	return cat
}

func formatDateBR(dateStr string) string {
	if dateStr == "" {
		return ""
	}
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.Parse(l, dateStr); err == nil {
			return t.Format("02/01/2006")
		}
	}
	parts := strings.Split(strings.Split(dateStr, " ")[0], "-")
	if len(parts) == 3 {
		return fmt.Sprintf("%s/%s/%s", parts[2], parts[1], parts[0])
	}
	return dateStr
}

func formatDisplayName(raw string) string {
	if raw == "" {
		return "-"
	}
	val := strings.TrimSpace(raw)
	if atIdx := strings.Index(val, "@"); atIdx != -1 {
		val = val[:atIdx]
	}
	val = strings.ReplaceAll(val, ".", " ")
	val = strings.ReplaceAll(val, "_", " ")
	val = strings.ReplaceAll(val, "-", " ")

	parts := strings.Fields(val)
	if len(parts) == 0 {
		return raw
	}
	for i, p := range parts {
		r := []rune(strings.ToLower(p))
		if len(r) > 0 {
			r[0] = []rune(strings.ToUpper(string(r[0])))[0]
			parts[i] = string(r)
		}
	}
	return strings.Join(parts, " ")
}

func parseFirstTechID(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	case []interface{}:
		if len(v) > 0 {
			return parseString(v[0])
		}
	}
	return ""
}

func (c *Client) getUserName(ctx context.Context, sessionToken, userID string) string {
	if userID == "" || userID == "-" || userID == "0" {
		return "-"
	}
	if name, ok := c.userCache[userID]; ok && name != "" {
		return name
	}
	fetched := c.fetchUserName(ctx, sessionToken, userID)
	if fetched != "" && fetched != userID {
		c.userCache[userID] = fetched
		return fetched
	}
	formatted := formatDisplayName(userID)
	c.userCache[userID] = formatted
	return formatted
}

func (c *Client) fetchUserName(ctx context.Context, sessionToken, userID string) string {
	if userID == "" || userID == "-" || userID == "0" {
		return "-"
	}
	apiURL := fmt.Sprintf("%s/apirest.php/User/%s", c.cfg.GlpiURL, userID)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return userID
	}
	req.Header.Set("App-Token", c.cfg.GlpiAppToken)
	req.Header.Set("Session-Token", sessionToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return userID
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return userID
	}

	var u struct {
		Name      string `json:"name"`
		Firstname string `json:"firstname"`
		Realname  string `json:"realname"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return userID
	}

	fn := strings.TrimSpace(u.Firstname)
	rn := strings.TrimSpace(u.Realname)
	if fn != "" || rn != "" {
		if pipeIdx := strings.Index(rn, "|"); pipeIdx != -1 {
			rn = strings.TrimSpace(rn[:pipeIdx])
		}
		fullName := strings.TrimSpace(fmt.Sprintf("%s %s", fn, rn))
		if fullName != "" {
			return fullName
		}
	}
	if u.Name != "" {
		return formatDisplayName(u.Name)
	}
	return userID
}

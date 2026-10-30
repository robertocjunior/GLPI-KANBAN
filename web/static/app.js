// ==========================================================================
// GLPI KANBAN - Application Controller
// Gerencia Drag-and-Drop, filtros instantâneos e persistência no SQLite
// ==========================================================================

let boardState = {
    columns: [],
    totalTickets: 0,
    myTicketsCount: 0,
    queueCount: 0,
    refreshIntervalSec: 30,
    activeFilter: 'all',
    searchQuery: '',
    draggedTicketId: null,
    draggedFromColId: null,
    activeEditingTicketId: null
};

let refreshTimer = null;

document.addEventListener('DOMContentLoaded', () => {
    initUI();
    loadBoard(true);
});

function initUI() {
    // Search
    const searchInput = document.getElementById('searchInput');
    const clearSearchBtn = document.getElementById('clearSearchBtn');

    searchInput.addEventListener('input', (e) => {
        boardState.searchQuery = e.target.value.trim().toLowerCase();
        clearSearchBtn.style.display = boardState.searchQuery ? 'block' : 'none';
        applyFiltersAndSearch();
    });

    clearSearchBtn.addEventListener('click', () => {
        searchInput.value = '';
        boardState.searchQuery = '';
        clearSearchBtn.style.display = 'none';
        applyFiltersAndSearch();
        searchInput.focus();
    });

    // Filter Buttons
    document.querySelectorAll('.filter-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            boardState.activeFilter = btn.dataset.filter;
            applyFiltersAndSearch();
        });
    });

    // Refresh Button
    document.getElementById('refreshBtn').addEventListener('click', () => {
        loadBoard(false);
    });

    // Modal Events
    document.getElementById('closeNoteModal').addEventListener('click', closeNoteModal);
    document.getElementById('cancelNoteBtn').addEventListener('click', closeNoteModal);
    document.getElementById('saveNoteBtn').addEventListener('click', saveNoteModal);
}

// ================= API & Data Loading =================

async function loadBoard(showLoadingSpinner) {
    const boardContainer = document.getElementById('boardContainer');
    const syncDot = document.getElementById('syncDot');
    const syncText = document.getElementById('syncText');
    const refreshBtn = document.getElementById('refreshBtn');

    if (showLoadingSpinner) {
        boardContainer.innerHTML = `
            <div class="board-loading">
                <div class="spinner"></div>
                <p>Consultando chamados no GLPI e carregando quadro do SQLite...</p>
            </div>
        `;
    }

    syncDot.className = 'status-indicator';
    syncText.textContent = 'Sincronizando...';
    refreshBtn.disabled = true;

    try {
        const resp = await fetch('/api/board');
        if (!resp.ok) {
            throw new Error(`Erro na API (${resp.status})`);
        }
        const data = await resp.json();

        boardState.columns = data.columns || [];
        boardState.totalTickets = data.total_tickets || 0;
        boardState.myTicketsCount = data.my_tickets_count || 0;
        boardState.queueCount = data.queue_count || 0;
        boardState.refreshIntervalSec = data.refresh_interval_sec || 30;

        // Atualiza stats
        document.getElementById('statTotal').textContent = boardState.totalTickets;
        document.getElementById('statQueue').textContent = boardState.queueCount;
        document.getElementById('statMine').textContent = boardState.myTicketsCount;

        let inProgressCount = 0;
        const inProgressCol = boardState.columns.find(c => c.id === 'in_progress');
        if (inProgressCol) {
            inProgressCount = inProgressCol.cards.length;
        }
        document.getElementById('statInProgress').textContent = inProgressCount;

        // Atualiza status de sync
        syncDot.className = 'status-indicator live';
        syncText.textContent = 'Conectado';
        document.getElementById('lastSyncTime').textContent = data.last_sync || new Date().toLocaleTimeString();

        renderBoard();
        startAutoRefresh(boardState.refreshIntervalSec);
    } catch (err) {
        console.error('Erro ao consultar quadro:', err);
        syncDot.className = 'status-indicator';
        syncText.textContent = 'Falha de conexão';
        if (showLoadingSpinner) {
            boardContainer.innerHTML = `
                <div class="board-loading">
                    <p style="color: var(--ms-red); font-weight: bold;">Erro ao conectar com o GLPI ou SQLite local.</p>
                    <p style="color: var(--text-muted); font-size: 11px;">${err.message}</p>
                    <button class="ms-btn primary" onclick="loadBoard(true)">Tentar Novamente</button>
                </div>
            `;
        }
    } finally {
        refreshBtn.disabled = false;
    }
}

function startAutoRefresh(sec) {
    if (refreshTimer) clearInterval(refreshTimer);
    refreshTimer = setInterval(() => {
        // Não atualiza automaticamente se o usuário estiver arrastando um card ou com modal aberto
        if (!boardState.draggedTicketId && !boardState.activeEditingTicketId) {
            loadBoard(false);
        }
    }, Math.max(sec, 10) * 1000);
}

// ================= Render Board =================

function renderBoard() {
    const boardContainer = document.getElementById('boardContainer');
    boardContainer.innerHTML = '';

    boardState.columns.forEach(col => {
        const colEl = document.createElement('section');
        colEl.className = 'kanban-column';
        colEl.dataset.colId = col.id;

        // Header
        const headerEl = document.createElement('div');
        headerEl.className = 'column-header';
        headerEl.style.borderBottomColor = col.color || '#444';

        headerEl.innerHTML = `
            <div class="column-title-group">
                <span class="column-solid-bar" style="background-color: ${col.color || '#0078d4'}"></span>
                <span class="column-title">${escapeHTML(col.title)}</span>
            </div>
            <span class="column-count" id="count-${col.id}">${col.cards.length}</span>
        `;
        colEl.appendChild(headerEl);

        // Cards Container
        const cardsEl = document.createElement('div');
        cardsEl.className = 'cards-container';
        cardsEl.dataset.colId = col.id;

        // Drag & drop dropzone listeners
        cardsEl.addEventListener('dragover', handleDragOver);
        cardsEl.addEventListener('dragleave', handleDragLeave);
        cardsEl.addEventListener('drop', handleDrop);

        col.cards.forEach((card, idx) => {
            const cardEl = createCardElement(card, col.id, idx);
            cardsEl.appendChild(cardEl);
        });

        colEl.appendChild(cardsEl);
        boardContainer.appendChild(colEl);
    });

    applyFiltersAndSearch();
}

function createCardElement(card, colId, position) {
    const cardEl = document.createElement('article');
    cardEl.className = `ticket-card ${card.pinned ? 'pinned' : ''}`;
    cardEl.draggable = true;
    cardEl.dataset.ticketId = card.id;
    cardEl.dataset.colId = colId;
    cardEl.dataset.priority = card.priority_id;
    cardEl.dataset.isAssignedMe = card.is_assigned_to_me ? '1' : '0';

    // Drag listeners
    cardEl.addEventListener('dragstart', handleDragStart);
    cardEl.addEventListener('dragend', handleDragEnd);

    // Prioridade class
    const pClass = `priority-${card.priority_id || 3}`;

    // Note preview
    let noteHtml = '';
    if (card.custom_notes && card.custom_notes.trim()) {
        noteHtml = `<div class="card-note-box">${escapeHTML(card.custom_notes.trim())}</div>`;
    }

    // Reset button (se movido manualmente para outra coluna)
    let resetBtn = '';
    if (card.is_manual) {
        resetBtn = `<button class="card-btn" title="Redefinir para coluna automática do GLPI" onclick="resetCardPosition(${card.id}, event)">↩ Auto</button>`;
    }

    cardEl.innerHTML = `
        <div class="card-top">
            <a href="${card.url}" target="_blank" rel="noopener noreferrer" class="ticket-id-link" title="Abrir chamado no GLPI">
                #${card.id} <span style="font-size: 9px;">↗</span>
            </a>
            <div class="card-top-right">
                <span class="status-badge">${escapeHTML(card.status)}</span>
                <span class="priority-pill ${pClass}">${escapeHTML(card.priority)}</span>
            </div>
        </div>

        <div class="card-title">${escapeHTML(card.title)}</div>

        <div class="card-meta">
            <div class="meta-row">
                <span class="meta-label">Req:</span>
                <span class="meta-value" title="${escapeHTML(card.requester_user)}">${escapeHTML(card.requester_user)}</span>
                ${card.requester_group ? `<span style="color:#555">•</span> <span class="meta-value" title="${escapeHTML(card.requester_group)}">${escapeHTML(card.requester_group)}</span>` : ''}
            </div>
            ${card.category ? `
            <div class="meta-row">
                <span class="category-tag" title="${escapeHTML(card.category_full || card.category)}">${escapeHTML(card.category)}</span>
            </div>` : ''}
            <div class="meta-row" style="font-size: 10px; color: #777;">
                <span>Mod: ${escapeHTML(card.date_mod ? card.date_mod.substring(0, 16) : card.date_created_br)}</span>
                ${card.assigned_user && card.assigned_user !== '-' ? `<span style="color:#555">•</span> <span style="color: #999;">Téc: ${escapeHTML(card.assigned_user)}</span>` : '<span style="color:#e81123">• Sem Téc</span>'}
            </div>
        </div>

        ${noteHtml}

        <div class="card-actions">
            <div>
                <button class="card-btn ${card.custom_notes ? 'has-notes' : ''}" onclick="openNoteModal(${card.id}, event)">
                    📝 ${card.custom_notes ? 'Editar Nota' : 'Nota'}
                </button>
                <button class="card-btn ${card.pinned ? 'is-pinned' : ''}" onclick="togglePinCard(${card.id}, event)">
                    📌 ${card.pinned ? 'Desafixar' : 'Fixar'}
                </button>
            </div>
            <div>
                ${card.is_manual ? '<span class="manual-tag" title="Movido manualmente neste quadro">Manual</span>' : ''}
                ${resetBtn}
            </div>
        </div>
    `;

    return cardEl;
}

// ================= Drag and Drop Logic (SQLite Only) =================

function handleDragStart(e) {
    const cardEl = e.currentTarget;
    boardState.draggedTicketId = parseInt(cardEl.dataset.ticketId, 10);
    boardState.draggedFromColId = cardEl.dataset.colId;

    cardEl.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', cardEl.dataset.ticketId);
}

function handleDragEnd(e) {
    e.currentTarget.classList.remove('dragging');
    boardState.draggedTicketId = null;
    boardState.draggedFromColId = null;
    document.querySelectorAll('.cards-container').forEach(c => c.classList.remove('drag-over'));
}

function handleDragOver(e) {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    e.currentTarget.classList.add('drag-over');
}

function handleDragLeave(e) {
    e.currentTarget.classList.remove('drag-over');
}

async function handleDrop(e) {
    e.preventDefault();
    const container = e.currentTarget;
    container.classList.remove('drag-over');

    const targetColId = container.dataset.colId;
    const ticketId = boardState.draggedTicketId;

    if (!ticketId || !targetColId) return;

    // Se soltou na mesma coluna sem alterar nada significativo
    if (targetColId === boardState.draggedFromColId) {
        return;
    }

    // Calcula posição aproximada (coloca no início ou fim)
    const position = 0;

    // 1. Move o elemento visualmente imediatamente (otimista)
    const cardEl = document.querySelector(`.ticket-card[data-ticket-id="${ticketId}"]`);
    if (cardEl) {
        cardEl.dataset.colId = targetColId;
        container.insertBefore(cardEl, container.firstChild);
    }

    // 2. Dispara requisição para gravar no SQLite local (GLPI NÃO é afetado!)
    try {
        const resp = await fetch('/api/card/move', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                ticket_id: ticketId,
                column_id: targetColId,
                position: position
            })
        });

        if (!resp.ok) {
            throw new Error(`Falha ao salvar no banco local (${resp.status})`);
        }

        // Atualiza contadores nas colunas
        updateColumnCounts();
    } catch (err) {
        console.error('Erro ao persistir movimentação no SQLite:', err);
        // Em caso de erro, recarrega o quadro
        loadBoard(false);
    }
}

function updateColumnCounts() {
    document.querySelectorAll('.kanban-column').forEach(colEl => {
        const colId = colEl.dataset.colId;
        const visibleCards = colEl.querySelectorAll('.ticket-card:not(.hidden)').length;
        const countBadge = document.getElementById(`count-${colId}`);
        if (countBadge) {
            countBadge.textContent = visibleCards;
        }
    });
}

// ================= Filters and Search =================

function applyFiltersAndSearch() {
    const filter = boardState.activeFilter;
    const q = boardState.searchQuery;

    document.querySelectorAll('.ticket-card').forEach(cardEl => {
        const isMine = cardEl.dataset.isAssignedMe === '1';
        const priority = parseInt(cardEl.dataset.priority || '0', 10);
        const colId = cardEl.dataset.colId;
        const textContent = cardEl.textContent.toLowerCase();

        let matchesFilter = true;
        if (filter === 'my') {
            matchesFilter = isMine || colId === 'assigned_me';
        } else if (filter === 'queue') {
            matchesFilter = !isMine && colId === 'queue_sistemas';
        } else if (filter === 'critical') {
            matchesFilter = priority >= 4; // Alta ou Maior
        }

        let matchesSearch = true;
        if (q) {
            matchesSearch = textContent.includes(q);
        }

        if (matchesFilter && matchesSearch) {
            cardEl.classList.remove('hidden');
        } else {
            cardEl.classList.add('hidden');
        }
    });

    updateColumnCounts();
}

// ================= Actions: Notes, Reset, Pin =================

function openNoteModal(ticketId, event) {
    if (event) event.stopPropagation();

    boardState.activeEditingTicketId = ticketId;
    let cardObj = null;
    for (const col of boardState.columns) {
        const found = col.cards.find(c => c.id === ticketId);
        if (found) { cardObj = found; break; }
    }

    const modal = document.getElementById('noteModal');
    const title = document.getElementById('modalTicketTitle');
    const meta = document.getElementById('modalTicketMeta');
    const input = document.getElementById('noteInput');

    title.textContent = `Anotação Local • Chamado #${ticketId}`;
    if (cardObj) {
        meta.textContent = `${cardObj.title} • Solicitante: ${cardObj.requester_user}`;
        input.value = cardObj.custom_notes || '';
    } else {
        meta.textContent = '';
        input.value = '';
    }

    modal.classList.add('show');
    input.focus();
}

function closeNoteModal() {
    document.getElementById('noteModal').classList.remove('show');
    boardState.activeEditingTicketId = null;
}

async function saveNoteModal() {
    const ticketId = boardState.activeEditingTicketId;
    if (!ticketId) return;

    const input = document.getElementById('noteInput');
    const notes = input.value.trim();

    try {
        const resp = await fetch('/api/card/notes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                ticket_id: ticketId,
                notes: notes
            })
        });

        if (!resp.ok) {
            throw new Error('Falha ao salvar anotação');
        }

        // Atualiza estado local e recarrega
        closeNoteModal();
        loadBoard(false);
    } catch (err) {
        alert('Erro ao salvar anotação no SQLite: ' + err.message);
    }
}

async function resetCardPosition(ticketId, event) {
    if (event) event.stopPropagation();
    if (!confirm(`Deseja redefinir a posição do chamado #${ticketId} para a coluna automática do GLPI?`)) {
        return;
    }

    try {
        const resp = await fetch('/api/card/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ticket_id: ticketId })
        });

        if (!resp.ok) throw new Error('Falha ao resetar card');
        loadBoard(false);
    } catch (err) {
        alert('Erro ao resetar: ' + err.message);
    }
}

async function togglePinCard(ticketId, event) {
    if (event) event.stopPropagation();

    try {
        const resp = await fetch('/api/card/pin', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ticket_id: ticketId })
        });

        if (!resp.ok) throw new Error('Falha ao alternar fixação');
        loadBoard(false);
    } catch (err) {
        alert('Erro ao alternar pin: ' + err.message);
    }
}

function escapeHTML(str) {
    if (!str) return '';
    return str
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

# GLPI KANBAN

Sistema de quadro Kanban pessoal para acompanhamento e controle de chamados da equipe de Sistemas no GLPI da Lipetral, construído em **Go (Golang)**, banco de dados local **SQLite** e interface web no estilo **Microsoft Metro / Fluent Dark**.

> **Princípio Fundamental:** Esta aplicação é **estritamente somente leitura** em relação ao GLPI. Toda a movimentação de cards, notas pessoais, checklists e fixação de chamados são persistidos exclusivamente no SQLite local. O GLPI **não é modificado**.

---

## 🚀 Funcionalidades

- **Design Microsoft Fluent / Metro:** Botões retos, cantos afiados (0px), paleta de cores sólidas corporativas e tema escuro por padrão.
- **1ª Coluna - Fila de Sistemas (Não Atribuídos):** Consulta em tempo real os chamados abertos da fila de sistemas (`field 8 = 1`), excluindo automaticamente *Holmes*, *Metadados* e *Movimentação Funcional*.
- **2ª Coluna - Meus Chamados (Atribuídos):** Chamados atribuídos a você no GLPI.
- **Colunas de Trabalho Pessoal:**
  - ⚡ **Em Andamento (Foco):** Onde você puxa os chamados que está trabalhando no momento.
  - ⏳ **Aguardando / Bloqueado:** Chamados aguardando resposta de solicitante, Protheus ou terceiro.
  - 🎯 **Validação / Testes:** Chamados prontos para validação.
  - ✅ **Concluído no Quadro:** Chamados finalizados no seu fluxo pessoal.
- **Drag & Drop Instantâneo:** Arraste livremente entre colunas; o estado é salvo imediatamente no SQLite local.
- **Anotações Locais:** Adicione notas rápidas de trabalho ou checklists em cada chamado sem poluir o GLPI.
- **Filtros e Busca em Tempo Real:** Pesquisa rápida por ID, título, requerente, departamento ou categoria.
- **Link Direto:** Abertura rápida do chamado no GLPI em uma nova aba com 1 clique.

---

## 🛠️ Arquitetura

- **Backend:** Go (Golang 1.25)
- **Banco de Dados Local:** SQLite (driver em Go puro sem dependência de CGO)
- **Containerização:** Docker multi-stage build sobre Alpine Linux
- **Frontend:** HTML5 Drag & Drop, CSS Metro Dark nativo embutido no binário via `embed.FS`
- **CI/CD:** GitHub Actions com publicação de imagem no GitHub Container Registry (`ghcr.io`)

---

## 📦 Como Executar

### 1. Com Docker Compose (Recomendado)

1. Clone o repositório e configure as variáveis de ambiente:
   ```bash
   cp example.env .env
   # Edite o .env informando GLPI_APP_TOKEN e GLPI_USER_TOKEN se necessário
   ```

2. Suba o container:
   ```bash
   docker compose up -d
   ```

3. Acesse no navegador:
   ```
   http://localhost:8081
   ```

---

## ⚙️ Variáveis de Ambiente (`.env`)

| Variável | Padrão | Descrição |
|---|---|---|
| `PORT` | `8081` | Porta HTTP da aplicação |
| `GLPI_URL` | `https://grupolipetral.verdanadesk.com` | URL base do GLPI |
| `GLPI_APP_TOKEN` | - | App-Token da API REST do GLPI |
| `GLPI_USER_TOKEN` | - | User-Token da API REST do GLPI |
| `GLPI_TECH_GROUP` | `1` | ID do Grupo Técnico (1 = Sistemas) |
| `MY_USER_ID` | `20` | Seu ID de usuário no GLPI |
| `MY_USER_NAME` | `Roberto` | Nome exibido e usado no filtro de atribuição |
| `DB_PATH` | `data/kanban.db` | Caminho do arquivo SQLite |
| `REFRESH_INTERVAL_SEC` | `30` | Intervalo de sincronização com o GLPI |

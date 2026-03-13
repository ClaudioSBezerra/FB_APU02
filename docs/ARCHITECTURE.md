# Arquitetura do Sistema - FB_APU01

## 1. Diagrama de Entidade-Relacionamento (ERD) - Lógico

A estrutura de dados suporta multi-tenancy hierárquico e processamento assíncrono.

```mermaid
erDiagram
    USERS ||--o{ USER_ENVIRONMENTS : "acessa"
    ENVIRONMENTS ||--|{ ENTERPRISE_GROUPS : "contém"
    ENVIRONMENTS ||--o{ USER_ENVIRONMENTS : "permitido_para"
    ENTERPRISE_GROUPS ||--|{ COMPANIES : "agrupa"
    
    COMPANIES ||--o{ IMPORT_JOBS : "gera"
    IMPORT_JOBS ||--|{ SPED_REGISTERS : "contém (C100, etc)"
    
    USERS {
        uuid id PK
        string email
        string role "admin|user"
        timestamp trial_ends_at
    }
    
    ENVIRONMENTS {
        uuid id PK
        string name "Ambiente Exclusivo"
    }
    
    COMPANIES {
        uuid id PK
        string name
        string trade_name
        uuid owner_id FK
    }
    
    IMPORT_JOBS {
        uuid id PK
        uuid company_id FK
        string status "pending|processing|completed|error"
        string filename
    }
```

## 2. Fluxo de Dados (Data Flow)

### 2.1 Fluxo de Upload e Processamento (Bulk)

```mermaid
sequenceDiagram
    participant U as Usuário
    participant FE as Frontend (React)
    participant API as Backend (Go API)
    participant DB as PostgreSQL
    participant W as Worker (Go Routine)

    U->>FE: Seleciona Pasta/Arquivos (webkitdirectory)
    FE->>API: POST /api/upload (Multipart FormData)
    API->>API: Valida JWT & Company Access
    API->>DB: INSERT INTO import_jobs (status='pending')
    API-->>FE: 200 OK (Job Queued)
    
    loop Async Processing
        W->>DB: Poll pending jobs
        W->>W: Stream Parse (ISO-8859-1 -> UTF8)
        W->>DB: INSERT Batch (Transaction)
        W->>DB: REFRESH MATERIALIZED VIEW
        W->>DB: UPDATE job status='completed'
    end
    
    FE->>API: GET /api/jobs (Polling)
    API-->>FE: Status Atualizado
```

## 3. Componentes de Software

### 3.1 Backend (Go)
- **Camada de Handlers**: Responsável pela validação de entrada, autenticação (Middleware) e roteamento.
- **Camada de Serviço (Worker)**: Desacoplada da API HTTP. Processa arquivos pesados sem bloquear a interface.
- **Camada de Dados**: Acesso direto via `database/sql` para performance máxima. Uso extensivo de SQL nativo para agregações complexas.

### 3.2 Frontend (React)
- **Context API**: `AuthContext` gerencia sessão e renovação de tokens.
- **Hooks Personalizados**: `useUpload` para gerenciar fila de envio de arquivos.
- **Componentes Visuais**: Shadcn/UI para consistência visual.

### 3.3 Banco de Dados (PostgreSQL)
- **Tabelas Raw**: Armazenam dados brutos do SPED (ex: `reg_c100`, `reg_0150`).
- **Materialized Views**: Pré-calculam totais por período/CFOP/Alíquota para performance de leitura em dashboards.
- **Schema Migrations**: Controle de versão do esquema de banco de dados.

## 4. Estratégia de Deploy
- **Ambiente**: Linux VPS (Hostinger).
- **Containerização**: Docker Compose gerenciando serviços (App, DB, Nginx).
- **CI/CD**: Git-based deployment (Push to Main -> Pull & Restart on Server).

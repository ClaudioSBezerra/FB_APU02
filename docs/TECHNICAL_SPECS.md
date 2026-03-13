# Especificações Técnicas - FB_APU01 (Fiscal Engine)

## 1. Visão Geral do Sistema
O **FB_APU01** é um sistema de alta performance para processamento assíncrono de arquivos fiscais (SPED EFD Contribuições) e apuração tributária, preparado para a Reforma Tributária (IBS/CBS). A plataforma opera em arquitetura Multi-Tenant com isolamento estrito de dados e processamento massivo via workers em Go.

## 2. Stack Tecnológico

### 2.1 Backend (Fiscal Engine)
- **Linguagem**: Go (Golang) 1.22+
- **Framework**: Standard Library (`net/http`) para máxima performance.
- **Autenticação**: JWT (JSON Web Tokens) com Claims personalizados (Role, UserID).
- **Drivers**: `github.com/lib/pq` (PostgreSQL).
- **Processamento**:
  - **Workers**: Goroutines concorrentes para parsing de SPED.
  - **Streaming**: `bufio.Scanner` para arquivos de grandes volumes (GBs) com consumo de memória O(1).
  - **Encoding**: Conversão on-the-fly de ISO-8859-1 para UTF-8.

### 2.2 Frontend (Client Interface)
- **Framework**: React 18.3.1 (Vite 5.2.0).
- **Linguagem**: TypeScript 5.2.2.
- **Gerenciamento de Estado**: React Query (TanStack Query) para cache e sincronização server-side.
- **Upload**: `webkitdirectory` para suporte a upload de pastas inteiras e múltiplos arquivos simultâneos.
- **UI/UX**: Tailwind CSS, Shadcn/UI, Recharts (Gráficos).

### 2.3 Banco de Dados
- **RDBMS**: PostgreSQL 15+.
- **Schema**: Relacional com suporte a Materialized Views para agregações complexas.
- **Migrações**: Sistema próprio de migração SQL (`migrations/*.sql`) executado no startup do backend.

---

## 3. Implementações Principais

### 3.1 Gestão de Usuários e Acesso (Auth & RBAC)
O sistema implementa um controle de acesso baseado em funções (RBAC) com suporte a Trial.

- **Roles**:
  - `admin`: Acesso total ao sistema, gestão de ambientes globais, promoção de usuários.
  - `user`: Acesso restrito aos dados de suas próprias empresas/ambientes.
- **Fluxo de Registro**:
  - Novos usuários são criados automaticamente com um **Ambiente Isolado** (ex: "Ambiente de [Nome]").
  - **Trial**: Período de 14 dias grátis. Expiração bloqueia login (exceto admins).
- **Data Isolation**:
  - Todo acesso a dados é filtrado por `company_id`.
  - Middleware `AuthMiddleware` valida JWT e injeta contexto do usuário.
  - Helpers `GetEffectiveCompanyID` garantem que o usuário só acesse empresas que possui ou é membro.

### 3.2 Gestão de Ambiente (Multi-Tenancy)
A arquitetura hierárquica permite organização flexível de conglomerados empresariais.

- **Hierarquia**:
  1. **Environment (Ambiente)**: O nível mais alto (ex: Grupo Empresarial X).
  2. **Enterprise Group (Grupo)**: Subdivisão lógica (ex: Varejo, Indústria).
  3. **Company (Empresa)**: A entidade legal (CNPJ raiz).
  4. **Branches (Filiais)**: Detectadas automaticamente nos arquivos SPED (Campo 0000/C100).
- **Isolamento**:
  - Usuários são vinculados a Ambientes (`user_environments`).
  - Consultas SQL utilizam `JOIN` com tabelas de permissão para garantir visibilidade restrita.

### 3.3 Motor de Processamento (Parsing & Upload)
Projetado para lidar com milhares de arquivos simultaneamente.

- **Bulk Upload**:
  - Frontend permite seleção de múltiplos arquivos ou pastas inteiras.
  - Envio em chunks via `FormData`.
- **Worker Pipeline**:
  1. **Ingestão**: API recebe arquivo -> Salva em disco -> Cria Job `pending`.
  2. **Worker**: Monitora Jobs -> Abre Stream -> Parse Linha-a-Linha.
  3. **Batch Insert**: Dados inseridos em transações (`db.Begin`) a cada N linhas para performance.
  4. **Pós-Processamento**: Trigger de `REFRESH MATERIALIZED VIEW` após sucesso.
  5. **Cleanup**: Arquivo original deletado do disco para economia de espaço.
- **Duplicidade**:
  - Checksum (Hash) ou verificação lógica (CNPJ + Período) previne reimportação acidental.

### 3.4 Views e Agregações (Relatórios)
O sistema utiliza Materialized Views para entregar dashboards instantâneos sobre milhões de registros.

- **MV_MERCADORIAS_AGREGADA**:
  - Consolida registros C100, C500 (Energia), D100 (Transporte), D500 (Comunicação).
  - Agrega por: `filial_cnpj`, `ano_mes`, `cfop`, `aliquota_icms`.
  - **Campos Calculados**:
    - `vl_total`, `vl_icms`, `vl_pis`, `vl_cofins`.
    - Projeções de Reforma Tributária (IBS/CBS) baseadas em tabelas de alíquotas futuras.
- **Reforma Tributária (2027-2033)**:
  - Lógica de transição aplicada dinamicamente nas Views.
  - Cálculo de IBS (Estadual/Municipal) e CBS (Federal) sobre base ajustada.

### 3.5 Infraestrutura e DevOps
- **Deploy**: Docker Compose em VPS (Hostinger).
- **Proxy**: Nginx como Gateway (SSL/TLS, Gzip, Rate Limiting).
- **Monitoramento**: Logs estruturados no stdout (coletados pelo Docker).
- **Persistência**: Volumes Docker para Postgres Data e Uploads.

## 4. Segurança
- **JWT**: Tokens assinados com `HS256`. Expiração de 24h.
- **Senhas**: Hashing com `bcrypt` (custo 14).
- **Sanitização**: Prepared Statements em todas as queries SQL (sem concatenação de strings).
- **CORS**: Configurado estritamente para domínios permitidos (`fbtax.cloud`).

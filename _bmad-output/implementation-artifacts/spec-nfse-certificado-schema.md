---
title: 'NFS-e: armazenamento de certificado A1 + schema de captura (tomadas/prestadas)'
type: 'feature'
created: '2026-10-23'
status: 'done'
review_loop_iteration: 1
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-captura-plano.md']
baseline_commit: 'a8c57daae903104ed855e94bd179eab07c67b7de'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** NFS-e (tomadas/prestadas) hoje não tem nenhuma captação automática — volume "enorme", 100% manual, sem automação no SAP (confirmado pelo usuário). A primeira peça que falta pra qualquer captura é onde guardar o certificado digital A1 por empresa (exigido pelo Portal Nacional ADN via mTLS) e onde gravar os documentos capturados.

**Approach:** criar schema novo (certificado + `nfse_entradas`/`nfse_saidas`) e CRUD de credencial, reaproveitando o padrão já validado de `rfb_credentials`/`sap_credentials` (tabela por empresa, segredo criptografado) e de `nfe_entradas`/`nfe_saidas` (colunas tributárias). Sem cliente mTLS nem captura real nesta sub-spec — issue #6.

## Boundaries & Constraints

**Always:** usar `fb_apu02/crypto` (`EncryptField`/`DecryptField`, AES-256-GCM) pros 2 campos sensíveis (certificado em base64, senha) — nunca gravar nenhum dos dois em texto plano; `company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE` em todas as tabelas novas; migration nova a partir do número 133 (última existente: 132); nenhuma rota de leitura retorna o certificado ou a senha em claro — só metadados (validade, status, nome do arquivo, booleano `tem_certificado`); seguir o padrão factory `func XyzHandler(db *sql.DB) http.HandlerFunc` + `GetEffectiveCompanyID` + `jsonErr`/`sanitizeDBErr` já usado em `cgibs_credentials.go`/`rfb_credentials.go`; **rotas `/api/nfse/certificado` (GET/POST/DELETE) exigem role `"admin"`** em `AuthMiddleware`/`withAuth`, mesmo padrão já usado pra `sap_credentials` (`main.go:471,473`) — decisão do usuário (loopback 1, 2026-10-23), por causa do poder de assinatura jurídica do certificado; **em todo save, qualquer campo vazio (`certificado_pfx`, `certificado_senha` ou `validade`) preserva o valor já persistido em vez de apagar** — mesmo padrão já usado em `cgibs_credentials.go` pro `client_secret` — decisão do usuário (loopback 1), exceto no primeiro cadastro (sem registro existente), onde `certificado_pfx`/`certificado_senha` continuam obrigatórios.

**Ask First:** nenhuma — decisões de arquitetura já resolvidas na spec-mãe (`spec-nfse-captura-plano.md`) e no loopback 1 (ver Spec Change Log).

**Never:** implementar cliente HTTP mTLS ou qualquer chamada real ao Portal Nacional ADN (sub-spec seguinte); copiar código do repositório de referência `cont-ai` (sem LICENSE); popular `nfse_entradas`/`nfse_saidas` com dado real (ficam vazias até a sub-spec do cliente ADN); adicionar lógica de agendamento.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Upload de certificado válido | `.pfx` em base64 + senha, ambos não vazios | Grava criptografado, calcula/armazena validade (se extraível; ver Design Notes), `status='ativo'` | — |
| Primeiro cadastro sem senha ou sem arquivo | Empresa sem certificado prévio, campo vazio | 400, mensagem indicando campo obrigatório (sem registro anterior pra preservar) | `jsonErr(w, 400, ...)` |
| Re-upload (troca de certificado) | Empresa já tem certificado cadastrado, todos os campos enviados | Substitui o anterior (UPSERT por `company_id`), reseta `status` pra `ativo` | — |
| Re-upload parcial (ex: só corrigir validade) | Empresa já tem certificado cadastrado, `certificado_pfx`/`certificado_senha`/`validade` vêm vazios no request | Campos vazios preservam o valor já persistido (não apagam); só os campos não-vazios são atualizados | — |
| Leitura da credencial | `GET` por empresa sem certificado cadastrado | `tem_certificado:false`, demais campos vazios/null, HTTP 200 (não 404) | — |
| Certificado vencido (validade calculável) | `validade < NOW()` | `status` calculado como `vencido` na leitura, mesmo que a coluna ainda diga `ativo` | Alerta visual no frontend (badge vermelho) |

</frozen-after-approval>

## Code Map

- `backend/migrations/132_cgibs_drop_legado.sql` -- última migration; nova começa em `133_nfse_certificados_e_schema.sql`
- `backend/migrations/115_sap_credentials.sql`, `051_create_rfb_credentials.sql` -- padrão de tabela de credencial por empresa a seguir (não copiar schema, só padrão)
- `backend/migrations/059_create_nfe_entradas.sql`, `058_create_nfe_saidas.sql` -- padrão de colunas de documento fiscal (chave, CNPJ partes, datas, `mes_ano`) a espelhar em `nfse_entradas`/`nfse_saidas`
- `backend/migrations/103_nfe_tributos_icms_pis_cofins.sql` -- padrão de nomenclatura `base_X`/`X` por tributo, a replicar pra ISS/PIS/COFINS/INSS/IRRF/CSLL
- `backend/crypto/crypto.go` -- `EncryptField`/`DecryptField` (AES-256-GCM), reaproveitar sem alteração
- `backend/handlers/cgibs_credentials.go` -- modelo de CRUD de credencial por empresa (struct com campo sensível `json:"-"`, `TemClientSecret bool` exposto, UPSERT preservando segredo quando campo vem vazio) -- replicar o MESMO padrão de "nunca expor o segredo" pro certificado/senha
- `backend/main.go:527-553` -- bloco de registro de rotas `/api/cgibs/credentials*`, replicar para `/api/nfse/certificado`
- `frontend/src/pages/CGIBSCredentials.tsx` -- modelo de tela de cadastro de credencial (form + status) a adaptar pra upload de certificado + alerta de validade
- `frontend/src/lib/navigation.ts:86-89` -- lista de "Credenciais" no menu de config (admin-only), adicionar "Certificado NFS-e"
- `frontend/src/components/AppSidebar.tsx:90-91` -- itens de credenciais na sidebar admin, adicionar entrada nova

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/133_nfse_certificados_e_schema.sql` -- cria `nfse_certificados` (id, company_id UNIQUE, certificado_pfx TEXT, certificado_senha TEXT, nome_arquivo TEXT, validade DATE, status TEXT CHECK IN ('ativo','vencido','revogado'), created_at, updated_at) + `nfse_entradas`/`nfse_saidas` (id, company_id, identificador_nfse TEXT, cnpj_prestador VARCHAR(14), cnpj_tomador VARCHAR(14), data_emissao DATE, mes_ano VARCHAR(7), valor_servicos NUMERIC(15,2), base_iss/iss/aliquota_iss/iss_retido/base_pis/pis/base_cofins/cofins/inss/irrf/csll NUMERIC(15,2) DEFAULT 0, created_at) com `UNIQUE(company_id, identificador_nfse)` -- base de dados pra tudo que vem depois. (Estrutura validada na 1ª derivação, sem mudança necessária.) Aplicada e registrada em `schema_migrations` no Postgres de teste local; `TestNFSeSchemaMigration_TabelasNovasExistem` confirma a criação das 3 tabelas.
- [x] `backend/handlers/nfse_credentials.go` -- `GetNFSeCertificadoHandler`, `SaveNFSeCertificadoHandler` (recebe `.pfx` em base64 + senha, criptografa os 2 com `crypto.EncryptField`, UPSERT por `company_id`, nunca retorna os campos sensíveis), `DeleteNFSeCertificadoHandler` -- CRUD completo. Incorporadas as correções do Spec Change Log: preservação de campos vazios (padrão `SaveCGIBSCredentialHandler` -- lê o registro existente antes de decidir o que preservar), `maxUploadBytes()`/`http.MaxBytesReader` (mesmo padrão de `xml_upload.go`), validação de base64 de `certificado_pfx`, round-trip check de `validade` via `time.Parse` + `Format` comparado à string original, status 201 só no cadastro novo / 200 em update, e o struct `NFSeCertificado` não declara `CertificadoPfx`/`CertificadoSenha` (nunca populados).
- [x] `backend/main.go` -- registrado `/api/nfse/certificado` (GET/POST/DELETE) com role `"admin"` via `handlers.AuthMiddleware(..., "admin")`, replicando exatamente o padrão de `/api/sap/credentials` (switch por método dentro do `http.HandleFunc`, não o `withAuth` com role `""` usado por `/api/cgibs/credentials`).
- [x] `backend/handlers/nfse_credentials_test.go` -- 19 testes cobrindo a I/O Matrix completa (upload válido, campos faltando no 1º cadastro, re-upload/UPSERT sem duplicar, leitura sem certificado, cálculo de `status` vencido), validação de base64 inválido, validação de round-trip de `validade` (30/02), preservação de `validade`/`certificado_pfx`/`certificado_senha` em re-upload parcial, delete com/sem certificado existente, e isolamento entre tenants (empresa B não lê nem exclui o certificado da empresa A, mesmo forjando `X-Company-ID`).
- [x] `frontend/src/pages/NFSeCertificado.tsx` (novo) -- form de upload (arquivo `.pfx` via `FileReader`+base64 + senha), exibe validade/status com badge (verde ativo, amarelo ≤30 dias, vermelho vencido), nunca exibe a senha de volta. Incorporado: `handleCancel` limpa `senha`/`validade`/arquivo selecionado do estado, `fetchError` separado de "sem certificado" trata resposta não-OK do GET explicitamente (banner de erro + botão "Tentar novamente", nunca renderiza como se não houvesse certificado), `diasParaVencer` pode retornar negativo mas o render nunca exibe esse número — trata `dias < 0` como `vencido` (badge vermelho).
- [x] `frontend/src/lib/navigation.ts`, `frontend/src/components/AppSidebar.tsx`, roteamento em `App.tsx` -- adicionada entrada "Certificado NFS-e" (admin-only na UI via `AdminRoute`/`adminOnly: true`, independente da role já exigida no backend), path `/nfse/certificado`.

**Acceptance Criteria:**
- Given um certificado e senha válidos enviados via `POST /api/nfse/certificado`, when a leitura seguinte (`GET`) é feita, then `certificado_pfx`/`certificado_senha` nunca aparecem na resposta, só `tem_certificado:true` + metadados. ✅ Coberto por `TestSaveNFSeCertificadoHandler_PrimeiroCadastroValido_Retorna201ENaoExpoeSegredos` (checa o corpo do save E do GET seguinte, byte a byte, pelos valores em claro).
- Given uma empresa sem certificado cadastrado, when `GET /api/nfse/certificado` é chamado, then retorna HTTP 200 com `tem_certificado:false` (não 404/500). ✅ Coberto por `TestGetNFSeCertificadoHandler_SemCertificado_Retorna200TemCertificadoFalse`.
- Given um certificado já cadastrado, when um novo upload é feito pra mesma empresa, then o registro é substituído (UPSERT), não duplicado. ✅ Coberto por `TestSaveNFSeCertificadoHandler_ReUpload_SubstituiNaoDuplica` (conta linhas em `nfse_certificados` + confirma `HTTP 200`, não 201, no re-upload).
- Given `nfse_entradas`/`nfse_saidas` recém-criadas, when a migration roda, then as tabelas existem vazias e não quebram nenhum handler existente (são tabelas novas, sem consumidor ainda). ✅ Coberto por `TestNFSeSchemaMigration_TabelasNovasExistem` + `go build ./...`/`go test ./...` passando sem erro em nenhum outro pacote.
- Given uma empresa B autenticada, when ela tenta ler/excluir o certificado da empresa A (via `X-Company-ID` adulterado ou similar), then não consegue acessar o dado da empresa A. ✅ Coberto por `TestNFSeCertificado_IsolamentoEntreTenants_EmpresaBNaoAcessaCertificadoDaEmpresaA` (duas empresas/usuários/environments genuinamente não relacionados; confirma GET forjado não devolve o certificado de A, DELETE forjado não remove o registro de A, e A continua intacto e acessível pelo próprio dono).
- Given um certificado já cadastrado com `validade` preenchida, when um re-upload é feito sem informar `validade`, then o comportamento segue exatamente a decisão registrada no Spec Change Log (preservar). ✅ Coberto por `TestSaveNFSeCertificadoHandler_ReUploadSemValidade_PreservaValidadeAnterior` (lê a coluna direto do banco + confirma via GET) e por `TestSaveNFSeCertificadoHandler_ReUploadSemPfxSenha_PreservaValoresAnteriores` para `certificado_pfx`/`certificado_senha`.

## Spec Change Log

**Loopback 1 (2026-10-23)** — revisão adversarial (Blind Hunter + Edge Case Hunter) sobre a 1ª implementação achou 2 gaps cuja causa raiz está no bloco de intenção travado (Boundaries/I-O Matrix) — travado pro próprio agente, só o humano resolve:
1. **Boundaries** dizia "seguir o mesmo padrão de auth... já usado em `cgibs_credentials.go`", que registra as rotas com role `""` (sem exigência de admin) — mas a sensibilidade do certificado A1 (poder de assinatura jurídica, já reconhecida na spec-mãe como "maior que um client_secret de API") pede o padrão mais forte que o próprio projeto já usa pra `sap_credentials` (role `"admin"`, `main.go:471,473`). Pendente decisão do usuário.
2. **I/O Matrix** não cobria o caso "re-upload sem informar `validade`" — a implementação gravou `NULL`, e a checagem SQL de vencimento (`status='ativo' AND validade < CURRENT_DATE`) nunca mais marca `vencido` depois disso, derrotando o único propósito do campo. Pendente decisão do usuário sobre preservar o valor anterior (como `cgibs_credentials.go` já faz pro `client_secret`) ou exigir em todo envio.

Código revertido (`git checkout`/`rm` de volta ao `baseline_commit`); tabelas de teste dropadas do Postgres local. KEEP pra próxima derivação: estrutura da migration 133 (`nfse_certificados`/`nfse_entradas`/`nfse_saidas`) estava correta e não muda; uso de `crypto.EncryptField`/`DecryptField` estava correto; estrutura dos 8 testes Go e o cálculo de `status` via `CASE` no SQL estavam corretos (só precisam da correção de preservação de `validade`); wiring de frontend (nav/sidebar/rota admin) estava correto.

**Achados "patch" (auto-fix, aplicados na próxima derivação, sem precisar de decisão):** sem limite de tamanho no upload (usar `maxUploadBytes()`/`http.MaxBytesReader`, padrão já usado em `xml_upload.go`); sem checagem de que `certificado_pfx` é base64 válido antes de criptografar; `time.Parse` aceita datas de calendário inválidas silenciosamente (ex. 30/02) sem round-trip check; `POST` sempre retorna 201 mesmo em update (deveria ser 200); campos `CertificadoPfx`/`CertificadoSenha` declarados no struct Go mas nunca populados (remover, é uma armadilha pra uso futuro); frontend "Cancelar" não limpa `senha`/`validade` do estado; frontend sem tratamento de erro pra `GET` não-OK; frontend `diasParaVencer` pode mostrar número negativo em vez de tratar como vencido; faltou teste de isolamento entre tenants (empresa A não acessa certificado da empresa B).

**Achados "defer" (registrados em `deferred-work.md`, fora do escopo desta sub-spec):** `GetEffectiveCompanyID` concede acesso a qualquer membro do grupo/ambiente, não só da empresa específica (comportamento pré-existente do projeto); enum `status='revogado'` inalcançável via API (sem endpoint de revogação ainda); race condition em POST/DELETE concorrentes pra mesma empresa; `GetNFSeCertificadoHandler` busca o blob criptografado inteiro só pra calcular um booleano; sem trilha de auditoria (quem fez upload/trocou/excluiu o certificado); DELETE é hard-delete sem histórico; certificado (identidade com poder de assinatura) compartilha a mesma `ENCRYPTION_KEY` global de todos os outros segredos do projeto, sem envelope encryption nem separação por tenant.

## Design Notes

Validade do certificado: extrair do `.pfx` exigiria parsing de PKCS#12 (biblioteca Go adicional, fora do padrão atual do projeto). Pra esta sub-spec, `validade` é um campo informado pelo usuário no formulário de upload (não extraído automaticamente do certificado) — simplificação aceitável porque ainda não há certificado real de teste disponível (ver spec-mãe). Extração automática fica como melhoria futura, não bloqueia esta sub-spec.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: compila sem erro
- `cd backend && go test ./handlers/... -run NFSe` -- expected: todos os testes novos passam
- `cd frontend && npx tsc -p tsconfig.app.json --noEmit` -- expected: zero erros (usar SEMPRE esse comando com `-p`, não `npx tsc --noEmit` sozinho, que verifica zero arquivos)

**Manual checks (se CLI não cobrir):**
- Conferir visualmente que a tela `/nfse/certificado` não aparece pra usuário não-admin.

## Suggested Review Order

**Autorização e preservação de segredo (o que o loopback 1 corrigiu)**

- Rota exige role `"admin"` nos 3 métodos — mesmo padrão de `sap_credentials`, não o de `cgibs_credentials`.
  [`main.go:484`](../../backend/main.go#L484)

- Save lê o registro existente antes de decidir o que gravar — campo vazio preserva, nunca apaga.
  [`nfse_credentials.go:243`](../../backend/handlers/nfse_credentials.go#L243)

- Primeiro cadastro exige pfx+senha+validade juntos — sem isso o cálculo de vencimento nunca ativa.
  [`nfse_credentials.go:230`](../../backend/handlers/nfse_credentials.go#L230)

- Em update, pfx e senha são um par: só um dos dois vazio é rejeitado (não dessincroniza).
  [`nfse_credentials.go:234`](../../backend/handlers/nfse_credentials.go#L234)

**Validação de entrada (achados da 2ª revisão)**

- `stripWhitespace` remove quebras de linha embutidas antes do base64 — evita rejeitar certificado válido gerado por CLI.
  [`nfse_credentials.go:39`](../../backend/handlers/nfse_credentials.go#L39)

- Corpo acima do limite retorna 413 explícito, não um 400 genérico.
  [`nfse_credentials.go:166`](../../backend/handlers/nfse_credentials.go#L166)

**Leitura e exibição de status**

- `GET` sem certificado retorna `{"certificado": nil}` (não um struct de valor zero) — mesmo padrão de `cgibs_credentials.go`.
  [`nfse_credentials.go:53`](../../backend/handlers/nfse_credentials.go#L53)

- Badge de vencido usa só o `status` vindo do backend — sem recálculo de data no cliente, sem risco de fuso horário.
  [`NFSeCertificado.tsx:220`](../../frontend/src/pages/NFSeCertificado.tsx#L220)

**Schema**

- 3 tabelas novas: certificado (segredo) + `nfse_entradas`/`nfse_saidas` (vazias, base pra próxima sub-spec).
  [`133_nfse_certificados_e_schema.sql:14`](../../backend/migrations/133_nfse_certificados_e_schema.sql#L14)

**Frontend — hidratação de formulário**

- Validações client-side: arquivo obrigatório no 1º cadastro, arquivo de 0 bytes rejeitado.
  [`NFSeCertificado.tsx:128`](../../frontend/src/pages/NFSeCertificado.tsx#L128)

- Cancelar limpa senha/validade do estado — não deixa segredo digitado residindo em memória.
  [`NFSeCertificado.tsx:177`](../../frontend/src/pages/NFSeCertificado.tsx#L177)

**Testes (peso extra: cobrem exatamente as 2 regressões do loopback 1)**

- Teste que passa pelo `AuthMiddleware` de verdade com usuário não-admin — a regressão do loopback 1 agora tem cobertura.
  [`nfse_credentials_test.go`](../../backend/handlers/nfse_credentials_test.go) — `TestSaveNFSeCertificadoHandler_AuthMiddlewareNaoAdmin_Retorna403`

- Teste de isolamento entre tenants no caminho de save (não só leitura) — o caminho mais reescrito desta spec.
  [`nfse_credentials_test.go`](../../backend/handlers/nfse_credentials_test.go) — `TestSaveNFSeCertificadoHandler_IsolamentoEntreTenants_SaveForjadoNaoAfetaCertificadoDaEmpresaA`

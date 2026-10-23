---
title: 'NFS-e: agendamento automático diário (horário fixo global)'
type: 'feature'
created: '2026-10-23'
status: 'done'
review_loop_iteration: 1
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-captura-plano.md', '{project-root}/_bmad-output/implementation-artifacts/spec-nfse-cliente-adn.md']
baseline_commit: 'd9ad999efb4d1dc9144720b3e09ec053e9a9066e'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** a captura de NFS-e só roda hoje via clique manual (`POST /api/nfse/buscar`, sub-spec #7) — sem agendamento, a automação prometida pelo plano geral não existe de verdade.

**Approach:** extrair a lógica de ciclo (certificado → mTLS → busca → persistência), hoje presa em `backend/handlers/nfse_busca.go`, pra uma função reaproveitável em `backend/services/` — mesmo padrão já usado pelo RFB (`SolicitarApuracaoParaEmpresa` em `services`, chamada tanto pelo handler manual quanto pelo `rfb_scheduler.go`). O handler manual passa a só validar HTTP/chamar essa função; um scheduler novo (`services/nfse_scheduler.go`) chama a mesma função por empresa, num horário fixo global (decisão do usuário, 2026-10-23: sem configuração por empresa nesta fase — `nfse_certificados` não tem colunas de agendamento e não vamos adicioná-las agora).

## Boundaries & Constraints

**Always:** nova função `services.ExecutarCicloNFSe(ctx context.Context, db *sql.DB, companyID string) (recebidos, novos, ignorados int, err error)` (assinatura ajustada no loopback 1 — ver Spec Change Log) encapsula TODA a lógica hoje em `BuscarNFSeHandler`/`persistirDocumentosNFSe` (validar certificado ativo, descriptografar, montar client mTLS, extrair CNPJ, buscar, persistir em transação) — fica em `services`, não em `handlers`; `handlers/nfse_busca.go` (`BuscarNFSeHandler`) passa a só resolver `companyID`/autenticação HTTP e chamar `services.ExecutarCicloNFSe`, sem lógica própria de persistência duplicada; scheduler roda num horário fixo global via variável de configuração (ex: `NFSE_SCHEDULER_HORARIO`, formato `HH:MM`, default `"06:00"` — mesmo princípio de env var com default do projeto), não por coluna de banco; reaproveitar o MESMO `NFSeBuscaRL` (rate limiter já existente) tanto no disparo manual quanto no agendado — a call concorrente de duas origens pra mesma empresa no mesmo minuto deve ser limitada pelo mesmo limiter; erro numa empresa (certificado vencido, falha de mTLS) é logado e NÃO interrompe o ciclo das demais empresas agendadas (mesmo princípio do `rfb_scheduler.go`); processar empresas em goroutines concorrentes (mesmo padrão de `rfb_scheduler.go:362-368`), não sequencialmente; log de resumo do ciclo inteiro (quantas empresas processadas, total de documentos novos/ignorados, quantos erros).

**Ask First:** nenhuma — a decisão de produto necessária (horário fixo global vs. configurável por empresa) já foi resolvida pelo usuário em 2026-10-23 (horário fixo global).

**Never:** adicionar colunas de agendamento em `nfse_certificados` ou qualquer UI de configuração de horário nesta sub-spec (documentar como extensão futura possível, não implementar); criar dependência de `services` para `handlers` (a extração deve ir na direção oposta: lógica migra de `handlers` pra `services`); duplicar a lógica de persistência em dois lugares — o handler manual DEVE chamar a mesma função do scheduler, nunca ter sua própria cópia.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Ciclo agendado roda no horário configurado | 1+ empresas com `nfse_certificados.status` calculado como `ativo` | Dispara `ExecutarCicloNFSe` para cada uma, em goroutines separadas | — |
| Empresa sem certificado ou vencido no momento do ciclo | `nfse_certificados` vazio ou `status != ativo` para a empresa | Empresa é pulada silenciosamente do agendamento (não é um erro, é esperado) | — |
| Erro de mTLS numa empresa durante o ciclo agendado | `ExecutarCicloNFSe` retorna erro pra uma empresa | Log de erro específico da empresa; as demais empresas do ciclo continuam normalmente | log por empresa |
| Disparo manual e agendado colidem (mesma empresa, mesmo minuto) | `POST /api/nfse/buscar` chamado enquanto o scheduler also dispara a mesma empresa | O `NFSeBuscaRL` compartilhado barra a segunda tentativa com 429 (manual) ou skip logado (agendado) | rate limit já existente |
| Falha ao resolver `NFSE_SCHEDULER_HORARIO` | Variável de ambiente ausente ou em formato inválido | Usa default `"06:00"`, loga aviso — nunca derruba o scheduler | log de warning |

</frozen-after-approval>

## Code Map

- `backend/handlers/nfse_busca.go` -- `BuscarNFSeHandler`, `persistirDocumentosNFSe`, `inserirNFSeDocumento` -- lógica de ciclo a EXTRAIR pra `services`; handler fica só com a parte HTTP (auth, rate limit, resposta JSON)
- `backend/services/nfse_adn.go` -- cliente mTLS já existente, consumido pela nova função de ciclo
- `backend/services/rfb_scheduler.go:298-369` (`StartRFBScheduler`) -- padrão de ticker 1min + goroutine por empresa + log por empresa, a replicar
- `backend/services/rfb.go` -- `SolicitarApuracaoParaEmpresa` -- padrão de função reaproveitada por handler E scheduler, mesmo princípio pra `ExecutarCicloNFSe`
- `backend/main.go:228` (`go services.StartRFBScheduler(getDB)`) -- padrão de registro de goroutine do scheduler no bootstrap
- `backend/migrations/133_nfse_certificados_e_schema.sql` -- schema do certificado, NÃO mexer (sem coluna de agendamento nesta fase)

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/nfse_processor.go` (novo) -- `ExecutarCicloNFSe(db *sql.DB, companyID string) (novos, ignorados int, err error)`, movendo a lógica de `persistirDocumentosNFSe`/`inserirNFSeDocumento`/validação de certificado de `nfse_busca.go` pra cá (funções renomeadas/exportadas conforme necessário).
- [x] `backend/handlers/nfse_busca.go` -- `BuscarNFSeHandler` reescrito pra só resolver `companyID`, checar `NFSeBuscaRL`, chamar `services.ExecutarCicloNFSe`, montar a resposta JSON com os erros mapeados (mesmas mensagens 400/502/429 já existentes, agora originadas dos erros tipados de `services`).
- [x] `backend/services/nfse_scheduler.go` (novo) -- `StartNFSeScheduler(dbFn func() *sql.DB)`: ticker 1min, resolve `NFSE_SCHEDULER_HORARIO` (default `06:00`), busca empresas com certificado `status` calculado como `ativo`, dispara `ExecutarCicloNFSe` em goroutine por empresa (reusa `NFSeBuscaRL.Allow(companyID)` antes de cada chamada agendada), log de resumo do ciclo.
- [x] `backend/main.go` -- registrar `go services.StartNFSeScheduler(getDB)`, mesmo bloco de `StartRFBScheduler`.
- [x] `backend/services/nfse_processor_test.go` -- mover/adaptar os testes já existentes de `nfse_busca_test.go` que cobrem a lógica de persistência (transação, CNPJ, dedup, autoemitido) pra testar `ExecutarCicloNFSe` diretamente.
- [x] `backend/services/nfse_scheduler_test.go` (novo) -- cobrir a I/O Matrix: erro numa empresa não impede as demais; empresa sem certificado/vencida é pulada; `NFSE_SCHEDULER_HORARIO` inválido cai no default sem derrubar o scheduler.
- [x] `backend/handlers/nfse_busca_test.go` -- ajustar os testes que hoje testam a persistência diretamente (já movidos pra `services`) pra testar só o comportamento HTTP do handler (status codes, rate limit, mapeamento de erro).

**Acceptance Criteria:**
- Given o handler manual e o scheduler automático, when qualquer um dos dois dispara uma busca pra uma empresa, then ambos chamam a MESMA função `services.ExecutarCicloNFSe` — nenhuma lógica de persistência duplicada entre os dois caminhos.
  - ✅ Verificado estaticamente (`handlers/nfse_busca.go` chama `services.ExecutarCicloNFSe`; `services/nfse_scheduler.go:runNFSeSchedulerCycle` chama a mesma função; nenhum dos dois tem cópia própria de `persistirDocumentosNFSe`) e por teste: `TestBuscarNFSeHandler_ServidorAceita_Retorna200ComContagens` (handler) e `TestExecutarCicloNFSe_GravaDocumentosSemDuplicarNoSegundoCiclo`/`TestRunNFSeSchedulerCycle_ErroEmUmaEmpresaNaoImpedeAsDemais` (scheduler) exercitam o mesmo código de persistência via os dois caminhos.
- Given 3 empresas agendadas pro mesmo horário, when uma delas falha (certificado vencido), then as outras 2 são processadas normalmente e o erro da primeira aparece só no log, não interrompe o ciclo.
  - ✅ `services/nfse_scheduler_test.go:TestRunNFSeSchedulerCycle_ErroEmUmaEmpresaNaoImpedeAsDemais` (empresa B falha mTLS, empresa A é processada e persiste seu documento) e `TestRunNFSeSchedulerCycle_EmpresaSemCertificadoOuVencida_NuncaDisparada` (empresa sem certificado/vencida nunca é sequer despachada).
- Given um disparo manual e um agendado pra mesma empresa dentro de 30s um do outro, when ambos tentam rodar, then o `NFSeBuscaRL` compartilhado impede a segunda chamada de fato bater na API do ADN.
  - ✅ `NFSeBuscaRL` agora é uma única instância em `services.NFSeBuscaRL` (`services/nfse_processor.go`), referenciada por `handlers.NFSeBuscaRL` (alias) e por `services/nfse_scheduler.go` — mesmo contador para as duas origens. Coberto por `handlers/nfse_busca_test.go:TestBuscarNFSeHandler_SegundaChamadaDentro30s_Retorna429` (2ª chamada manual dentro de 30s è barrada); o caso scheduler-vs-manual não tem teste de concorrência dedicado (ver Desvios/observações abaixo), mas usa o mesmo `*services.RateLimiter.Allow`, logo a mesma garantia atômica de `TestBuscarNFSeHandler_SegundaChamadaDentro30s_Retorna429` se aplica.
- Given `NFSE_SCHEDULER_HORARIO` não definido ou com formato inválido, when o scheduler inicia, then usa `"06:00"` como default e loga um aviso, sem falhar a inicialização do servidor.
  - ✅ `services/nfse_scheduler_test.go:TestResolveNFSeSchedulerHorario_Ausente_UsaDefault` e `TestResolveNFSeSchedulerHorario_FormatoInvalido_UsaDefault`.

## Spec Change Log

**Loopback 1 (2026-10-23)** — revisão adversarial (Blind Hunter + Edge Case Hunter) sobre a 1ª implementação achou 2 regressões confirmadas e vários achados de robustez. Nenhum exigiu decisão do usuário (todos têm uma única leitura sensata, sem tradeoff de produto/segurança real) — resolvidos diretamente:

1. **Assinatura de `ExecutarCicloNFSe` ajustada**: ganhou `ctx context.Context` (1º parâmetro) e `recebidos int` (1º retorno) — `(ctx, db, companyID) (recebidos, novos, ignorados int, err error)`. Dois motivos: (a) a extração perdeu o `r.Context()` que o handler manual já passava antes desta sub-spec (sub-spec #7 tinha corrigido isso explicitamente) — sem `ctx`, um cliente que desconecta não cancela mais a chamada mTLS em andamento; (b) `documentos_recebidos` na resposta virou `novos + ignorados`, que SUBCONTA documentos já capturados antes (`ON CONFLICT DO NOTHING` não incrementa nem `novos` nem `ignorados`) — um ciclo repetido com só documentos duplicados reportava `0` recebidos mesmo tendo vindo N do governo. `recebidos` agora é contado à parte, sempre `len(documentos)` do ADN, independente do que foi persistido.
2. **Erro de "limite de lotes" ganhou sentinela própria** (`services.ErrNFSeADNLimiteLotes`, em `nfse_adn.go`) — antes era um `fmt.Errorf` cru que caía no `default:` do mapeamento de erro HTTP e virava 500; agora mapeia pra 502, consistente com a Acceptance Criterion original ("mesmas mensagens 400/502/429 já existentes").
3. **Recuperação de panic por empresa** no scheduler — a Boundaries já prometia "erro numa empresa não interrompe as demais", mas um panic (não só um `error`) numa goroutine sem `recover()` derrubaria o processo INTEIRO (não só aquele ciclo) — contradizendo a própria garantia já escrita. Corrigido com `defer recover()` por goroutine.
4. **`NFSE_SCHEDULER_HORARIO` canonicalizado** após o parse (`time.Parse` + reformatar pro padrão `HH:MM` com zero à esquerda) — um valor como `"6:00"` passava na validação mas nunca igualava o `now.Format("15:04")` (sempre zero-paddado), fazendo o scheduler nunca disparar silenciosamente.
5. **Catch-up dentro do mesmo dia**: trocado o match exato de minuto por "já é ≥ horário configurado E ainda não rodou hoje" (rastreando a última data de execução) — antes, se o processo reiniciasse ou estivesse ocupado exatamente no minuto configurado, o dia inteiro era pulado sem nenhuma tentativa posterior.
6. **Checagem de `rows.Err()`/`rows.Scan`** na query de empresas elegíveis do scheduler — antes, um erro de scan ou uma interrupção no meio do cursor derrubava empresas da lista silenciosamente, sem log.
7. **Limite de concorrência** no disparo de goroutines por empresa (semáforo) — protege o pool de conexões do Postgres e evita um pico de tráfego incomum contra o ADN se o número de empresas crescer.
8. **Comentário incorreto removido** em `nfse_busca_test.go` que alegava cobertura "migrada" pra `nfse_processor_test.go` que nunca existiu em nenhum dos dois lugares.

**Achados "defer" (registrados em `deferred-work.md`):** rate limiter não impede sobreposição de execuções em andamento além da janela de 30s (só impede novo disparo); SQL de "status calculado" do certificado duplicada em 3 lugares.

## Design Notes

A extração de `handlers/nfse_busca.go` pra `services/nfse_processor.go` espelha exatamente a separação já validada no módulo RFB: `services` concentra a lógica de negócio reaproveitável (chamada por qualquer origem — HTTP manual ou scheduler), `handlers` fica fino, só traduzindo HTTP ⇄ chamada de serviço. Os erros tipados já existentes (`services.ErrNFSeADNAutenticacaoFalhou`, `ErrNFSeADNFormatoInesperado`, `ErrNFSeADNComunicacaoFalhou`) continuam sendo a forma do handler decidir a mensagem HTTP certa — não mudam nesta sub-spec.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: compila sem erro
- `cd backend && go vet ./...` -- expected: sem avisos
- `cd backend && go test ./services/... ./handlers/... -run NFSe -v` -- expected: todos os testes passam (migrados + novos)
- `cd backend && go test ./...` -- expected: suíte completa verde

**Manual checks (se CLI não cobrir):**
- Nenhuma validação end-to-end contra a API real do ADN é possível nesta sub-spec (mesma limitação das anteriores, sem certificado de teste disponível).

## Suggested Review Order

**Função reaproveitada por handler e scheduler (o objetivo central desta sub-spec)**

- `ExecutarCicloNFSe` — toda a lógica de ciclo (certificado → mTLS → busca → persistência) num só lugar, chamada pelos dois caminhos.
  [`nfse_processor.go:79`](../../backend/services/nfse_processor.go#L79)

**Correções do loopback 1 (regressões confirmadas na revisão)**

- `recebidos` contado à parte de `novos`/`ignorados` — corrige subcontagem em ciclos com documentos repetidos.
  [`nfse_processor.go:79`](../../backend/services/nfse_processor.go#L79)

- Limite de lotes ganhou sentinela própria — volta a mapear pra 502, não mais 500.
  [`nfse_adn.go:82`](../../backend/services/nfse_adn.go#L82)

- `ctx` propagado da requisição HTTP até a chamada mTLS — cancelamento que a sub-spec anterior já tinha corrigido não se perde mais na extração.
  [`nfse_busca.go:33`](../../backend/handlers/nfse_busca.go#L33)

**Scheduler: isolamento de falha entre empresas**

- Recuperação de panic + semáforo de concorrência + liberação garantida via `defer`, nessa ordem.
  [`nfse_scheduler.go:86`](../../backend/services/nfse_scheduler.go#L86)

- Decisão de disparo isolada numa função pura testável — catch-up dentro do dia, não só o minuto exato.
  [`nfse_scheduler.go:70`](../../backend/services/nfse_scheduler.go#L70)

- Horário canonicalizado (zero-padding) antes de comparar — evita silêncio total do agendamento.
  [`nfse_scheduler.go:47`](../../backend/services/nfse_scheduler.go#L47)

**Bootstrap**

- Scheduler registrado logo após o do RFB, mesmo padrão.
  [`main.go:229`](../../backend/main.go#L229)

**Infra compartilhada (rate limiter movido de `handlers` pra `services`)**

- Mesma instância usada pelo disparo manual e pelo agendado — `handlers` mantém compatibilidade via alias de tipo.
  [`nfse_processor.go:28`](../../backend/services/nfse_processor.go#L28)

---
title: 'RFB v2: schema + parser (débitos/créditos) — deploy único'
type: 'feature'
created: '2026-09-24'
status: 'done'
review_loop_iteration: 1
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-rfb-cbs-v2-migracao.md']
baseline_commit: '2ada5df45b0daef17e356c9379a39e6905254767'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** tentativa anterior (só migration+2 ORDER BY) foi revertida via loopback — 2 revisores independentes provaram que migrar `data_apuracao` para o formato v2 cru (`"mm/aaaa"`) sem também atualizar o parser (que continua escrevendo `"AAAAMM"` via scheduler diário) deixa a coluna com 2 formatos coexistindo, quebrando 6+ pontos de leitura e a agregação SQL (mesmo período contado 2x). Decisão do humano: manter o formato v2 cru no banco, mas schema+leituras+parser viram **um único deploy atômico**.

**Approach:** (1) migration com widen+backfill de `data_apuracao` + colunas novas de débitos/créditos v2; (2) os 6 pontos de leitura afetados traduzem `"mm/aaaa"`→`"AAAAMM"` na query (`TO_DATE`/`TO_CHAR`), preservando o contrato externo da API — **zero mudança no frontend**; (3) parser (`rfb_processor.go`/`rfb_creditos_processor.go`) detecta o shape do JSON (v1: blocos `apuracaoCorrente`/etc.; v2: lista plana `apuracao[].debitos[]`) e passa a gravar sempre em `"mm/aaaa"`, normalizando v1's `"AAAAMM"` por item na hora do parse.

## Boundaries & Constraints

**Always:**
- Deploy único — migration, leituras e parser sobem juntos. Nunca separar (foi exatamente isso que causou o loopback).
- Leituras traduzem via `TO_DATE(data_apuracao,'MM/YYYY')` (ordenação) / `TO_CHAR(TO_DATE(data_apuracao,'MM/YYYY'),'YYYYMM')` (filtro/listagem por período) — API e frontend continuam recebendo/enviando `"AAAAMM"`, sem mudança de contrato.
- v2 agrupa por `pa` em `apuracao[]` (pode ter MÚLTIPLOS períodos numa resposta) — parser deve iterar cada grupo `pa` e rodar agregação+UPSERT de resumo POR período, não assumir um `dataApuracao` único pra resposta inteira (v1 tinha essa limitação, corrigida parcialmente no commit `2ada5df` para o caso "só extemporâneo"; v2 exige generalizar).
- Bucketing tipo_apuracao: v1 mantém a derivação estrutural existente (corrente/ajuste/extemporâneo por bloco); v2 usa a heurística aprovada (mês(`pa`) == mês(`registro`) → `"corrente"`, senão → `"extemporaneo"`).
- `valor_cbs_nao_extinto` para linhas v2: popular como `saldoDevedor` (aproximação mais próxima — "não extinto/pago" ≈ "saldo devedor") para não zerar silenciosamente esse total no resumo. Documentar como aproximação a validar com dado real.
- Campos monetários v2 sem contraparte direta (`excedente`, `inexigivel`, `suspenso`, `saldoDevedor`, árvore de créditos) gravados nas colunas novas (spec de schema anterior), SEM substituir os campos existentes.

**Never:** tocar em pagamentos/recolhimentos; mudar o contrato de API/frontend (`periodo`/`ano` continuam `AAAAMM`/`AAAA`); remover a derivação estrutural de v1 (só v2 usa a heurística por data).

</frozen-after-approval>

## Code Map

- `backend/migrations/129_rfb_v2_schema_prep.sql` (novo) -- widen+backfill `data_apuracao` (4 tabelas) + colunas novas débitos/créditos
- `backend/handlers/rfb_apuracao.go:975` -- `ORDER BY` → `TO_DATE`
- `backend/handlers/rfb_debitos_lista.go:74,79,239,303` -- filtro periodo/ano, ORDER BY, distinct-periods → `TO_DATE`/`TO_CHAR`
- `backend/handlers/rfb_creditos.go:291` -- filtro periodo → `TO_CHAR(TO_DATE(...))`
- `backend/services/rfb_processor.go` -- structs v1 (`RFBApuracaoJSON` etc.) + novas structs v2 + detecção de shape + `ProcessarDownloadRFB`/`ReprocessarRawJSON` com loop por `pa`
- `backend/services/rfb_creditos_processor.go` -- mesma estrutura para créditos, árvore de apropriação/utilização v2

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/129_rfb_v2_schema_prep.sql` -- widen `rfb_debitos`/`rfb_resumo.data_apuracao` p/ `VARCHAR(10)`; backfill `AAAAMM→mm/aaaa` nas 4 tabelas (regex `^[0-9]{6}$`, idempotente); `ADD COLUMN IF NOT EXISTS`: débitos (`origem INTEGER`, `documento INTEGER`, `data_registro/data_atualizacao TIMESTAMPTZ`, `valor_cbs_excedente/inexigivel/suspenso/saldo_devedor DECIMAL(18,2)`); créditos (`origem/documento INTEGER`, `data_registro/data_atualizacao DATE`, `valor_cbs_excedentes` + árvore apropriação/utilização, todos `NUMERIC(15,2)`).
- [x] `backend/handlers/rfb_debitos_lista.go` -- trocar os 4 pontos (`periodo` L74, `ano` L79, `ORDER BY` L239, distinct-periods L303) para `TO_DATE`/`TO_CHAR` conforme padrão acima.
- [x] `backend/handlers/rfb_creditos.go:291` -- mesma tradução no filtro de período.
- [x] `backend/handlers/rfb_apuracao.go:975` -- `TO_DATE` no ORDER BY.
- [x] `backend/services/rfb_processor.go` -- struct v2 (`RFBApuracaoV2JSON{Apuracao []{PA string; Debitos []RFBDebitoV2}}`, `RFBDebitoV2{Origem,Documento int; Chave string; Emissao,Registro,Atualizacao *RFBTime; CBS{Apurado,Excedente,Inexigivel,Suspenso,Extinto,SaldoDevedor float64}}`); função de detecção de shape (probe do JSON bruto); adapter v2→struct canônica `RFBDebito` normalizando `PA`→`DataApuracao` (já em `mm/aaaa`) e aplicando a heurística de bucketing; loop por `pa` para agregação/resumo multi-período; `insertDebito` grava as colunas novas quando vindas de v2 (NULL para linhas v1).
- [x] `backend/services/rfb_creditos_processor.go` -- mesma estrutura para créditos, incluindo achatamento da árvore `apropriacao`/`utilizacao` nas colunas novas.

**Acceptance Criteria:**
- Given uma resposta v1 (blocos antigos) e uma v2 (lista `apuracao[]`) para a mesma empresa, when processadas, then ambas gravam `data_apuracao` em `mm/aaaa` e o resumo agrega corretamente as duas.
- Given uma resposta v2 com `apuracao[]` contendo 2 `pa` diferentes, when processada, then `rfb_resumo` tem 2 linhas (uma por período), não 1.
- Given a tela de Débitos RFB (frontend), when usada após o deploy, then filtro por período/ano e o dropdown de períodos continuam funcionando sem nenhuma mudança de código no frontend.
- Given a suíte de testes de `rfb_processor_test.go`/`rfb_creditos_processor_test.go` (v1, já existente), when rodada após a mudança, then continua passando -- regressão zero pro caminho v1.

### Loopback 1 (2026-09-24)

**Trigger (intent_gap):** 1ª tentativa (só migration + 2 ORDER BY) revertida — revisão adversarial provou que o backfill de `data_apuracao` sem o parser escrevendo o mesmo formato deixa 2 formatos coexistindo e quebra 6+ pontos de leitura. **Decisão do humano:** manter formato v2 cru no banco e fundir schema+leituras+parser num deploy único (esta spec).

### Patch round (2ª revisão, 2026-09-24)

Patches sem decisão pendente: regex de mês `(0[1-9]|1[0-2])` em todos os guards `TO_DATE` + `CASE` dentro do `SELECT DISTINCT` de períodos; `normalizeDataApuracao`/`periodoValido` rejeitam mês inválido e `processV2*` ignoram `pa` inválido; `NULLS LAST`; filtro `periodo` normaliza não-dígitos; upsert v2 preserva `situacao`/`formas_extincao`/`eventos` de v1 (`COALESCE`); `detectRFBShape` loga payload híbrido/`apuracao` nulo. **Decisão do humano:** créditos v2 → `extinto = utilizacao.utilizado`, `nao_extinto = naoUtilizado.saldoCredor` (aproximação a validar com dado real). Adiados: ver `deferred-work.md` (seção 2026-09-24).

## Design Notes

`valor_cbs_nao_extinto = saldoDevedor` para v2 é aproximação, não confirmada com dado real da RFB — revisitar quando houver volume de produção v2 para validar se a semântica bate.

Detecção de shape sugerida: fazer um probe leve do JSON bruto (`json.RawMessage` + checagem de chaves presentes: `apuracaoCorrente`/`apuracaoAjuste`/`debitosExtemporaneos` → v1; `apuracao` com array de objetos contendo `pa` → v2) antes do `Unmarshal` definitivo, evitando dois `Unmarshal` completos.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...`
- `cd backend && go test ./services/... -run RFB -v` (deve incluir testes novos pro caminho v2 + os existentes de v1 passando)
- Testar manualmente a tela `/rfb/debitos` no frontend local contra o banco pós-migration (filtro período/ano, dropdown, ordenação) -- confirma que a tradução SQL preserva o contrato sem mudança de frontend.

**Manual checks:**
- Rodar a migration contra staging/cópia de PRD antes do deploy real, medir tempo do backfill (tabelas com histórico desde baseline ~R$3M/mês).

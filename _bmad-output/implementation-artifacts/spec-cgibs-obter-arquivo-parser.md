---
title: 'CGIBS: Obter Arquivo + parser do extrato_cc'
type: 'feature'
created: '2026-09-28'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'a904c8d34d3366ecf0efffac515537dc5f2d0efb'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-cgibs-conta-corrente-plano.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** o webhook (sub-spec anterior) persiste `cgibs_solicitacoes`/`cgibs_arquivos` mas não baixa nem processa nada — `cgibs_operacoes`/`cgibs_lancamentos` continuam vazias. É o ponto de maior incerteza técnica: a seção "Obter Arquivo" (5.3) do MOC não extraiu (imagem), então o shape exato do request/response é inferido por analogia aos outros endpoints já confirmados.

**Approach:** `services.ObterArquivo()` (mesmo padrão de `Habilitar()`: `ClientID`+`ClientSecret` no corpo, URL via env sem fallback) busca o arquivo (JSON cru, estrutura do ANEXO I); `ProcessarArquivoCGIBS()` faz parse e grava em `cgibs_operacoes`/`cgibs_lancamentos` dentro de uma tx, upsert por `(company_id, chave_acesso)` e `(operacao_id, lancamento_id_externo)` respectivamente — lançamentos são **append-only** (nunca atualiza um já visto, só insere novos, conforme "sistemática incremental" do MOC). Disparo: o webhook (editar `cgibs_webhook.go`) dispara isso em goroutine pra cada `cgibs_arquivos` novo, mesmo padrão do webhook RFB.

## Boundaries & Constraints

**Always:**
- `CGIBS_OBTER_ARQUIVO_URL` (env, sem fallback) — erro tipado se vazia, sem tentar rede, mesmo padrão de `Habilitar()`.
- Campos numéricos/inteiros do JSON usam tipo tolerante (aceita string OU número — mesma lição já aprendida com `FlexString` em `rfb_processor.go`; API de governo, mesmo risco).
- JSON raiz é o próprio "header" (sem wrapper) — `nomearquivo`, `tiposolicitacao`, `datageracao`, `parametrosgeracao{datainicial,datafinal,qtdoperacoes}`, `operacoes[]`; cada operação: `ID`, `CHAVE_ACESSO`, `DTH_EMISSAO`, `DTH_AUTORIZACAO`, `CNPJ_FORNECEDOR`, `CNPJ_ADQUIRENTE` (opcional), `extrato_cc{hash, lista[]}` (opcional); cada lançamento: `ID`, `DTH_LANCTO`, `MOV`, e os 7 valores (nomes exatos na migration 131).
- `company_id` de `cgibs_operacoes`/`cgibs_lancamentos` vem da `cgibs_solicitacoes` (via `cgibs_arquivos.solicitacao_id`), nunca do `CNPJ_FORNECEDOR`/`CNPJ_ADQUIRENTE` do payload (mesma regra já fixada na sub-spec de schema).
- Lançamentos: `ON CONFLICT (operacao_id, lancamento_id_externo) DO NOTHING` — nunca `DO UPDATE` (o MOC é explícito: "linha nova complementa, não altera anteriores").
- Tudo numa única tx por arquivo: parse falhar não deixa `cgibs_arquivos`/`cgibs_operacoes`/`cgibs_lancamentos` inconsistentes entre si.
- `cgibs_arquivos.raw_json` grava o JSON bruto (mesmo princípio do `rfb_requests.raw_json`) ANTES do parse — falha no parse não perde o dado recebido.
- Falha em qualquer etapa marca `cgibs_arquivos.status='erro'` + `error_message`, não trava as outras operações/lançamentos do mesmo arquivo que já processaram com sucesso (processar item a item, não abortar tudo no primeiro erro de uma operação isolada — logar e continuar pras demais).

**Never:** implementar UI ainda (fica pra sub-spec 4); reprocessamento manual/scheduler de retry (fica pra sub-spec 4, que já mexe em `cgibs_apuracao.go`); mudar schema (usar exatamente as colunas da migration 131).

</frozen-after-approval>

## Code Map

- `backend/services/cgibs.go` -- adicionar `ObterArquivo(clientID, clientSecret string, idSolicitacao, numeroSequencial int64) ([]byte, error)`
- `backend/services/cgibs_parser.go` (novo) -- structs do JSON (`CGIBSArquivoJSON` etc.) + `ProcessarArquivoCGIBS(db *sql.DB, arquivoID string) error`
- `backend/handlers/cgibs_webhook.go` -- após persistir `cgibs_arquivos`, disparar `go services.ProcessarArquivoCGIBS(db, arquivoID)` por arquivo novo (mesmo padrão do dispatch em `RFBWebhookHandler`)
- `backend/migrations/131_cgibs_conta_corrente_fiscal.sql` -- schema alvo (referência, não alterar)
- `backend/services/rfb_processor.go` (`FlexString`, `RFBTime`) -- padrão de tipos tolerantes a replicar

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/cgibs.go` -- `ObterArquivo()`.
- [x] `backend/services/cgibs_parser.go` -- structs com tipos tolerantes; `ProcessarArquivoCGIBS()`: busca `cgibs_arquivos`+`company_id` via join, chama `ObterArquivo`, salva `raw_json`, parseia, abre tx, upsert operações + lançamentos (item a item, erro isolado não aborta o arquivo inteiro), atualiza `cgibs_operacoes.ultimo_arquivo_id`/`extrato_hash`, marca `cgibs_arquivos.status='baixado'`/`baixado_em` ou `'erro'`/`error_message`.
- [x] `backend/handlers/cgibs_webhook.go` -- dispatch em goroutine por arquivo novo.
- [x] Testes (`openTestDB`, fake `httptest`): `ObterArquivo` sem env configurada; parser com arquivo de exemplo (2 operações, uma com `extrato_cc` e outra sem); lançamento duplicado (`ON CONFLICT DO NOTHING`, não sobrescreve); campo numérico vindo como string no JSON não quebra o parse; falha de parse marca `erro` sem perder `raw_json`; uma operação malformada dentro de um arquivo não impede as demais operações do mesmo arquivo de serem gravadas.

**Acceptance Criteria:**
- Given um arquivo de exemplo com 2 operações (1 com 3 lançamentos, 1 sem `extrato_cc`), when processado, then existem 2 `cgibs_operacoes` e 3 `cgibs_lancamentos`, `cgibs_arquivos.status='baixado'`.
- Given o mesmo arquivo processado de novo (reenvio), when reprocessado, then não duplica lançamentos (idempotente).
- Given um valor monetário vindo como `"123.45"` (string) no JSON, when parseado, then grava `123.45` sem erro.

## Design Notes

Exemplo mínimo do shape esperado (ilustrativo, adaptar ao Go real):
```json
{
  "nomearquivo": "arquivo1.json",
  "tiposolicitacao": "diferencial(delta)",
  "datageracao": "2026-09-28 06:00:00",
  "parametrosgeracao": {"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 1},
  "operacoes": [{
    "ID": 123, "CHAVE_ACESSO": "35260912345678000199550010000012341123456789",
    "DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "DTH_AUTORIZACAO": "2026-09-27 10:00:05.000-03:00",
    "CNPJ_FORNECEDOR": "12345678", "CNPJ_ADQUIRENTE": "98765432",
    "extrato_cc": {"hash": "abc123...", "lista": [
      {"ID": 1, "DTH_LANCTO": "2026-09-27 10:00:10", "MOV": 10,
       "RECURSO_FINANCEIRO_DISPONIVEL_PARA_TRANSFERENCIA": 0, "RECURSO_FINANCEIRO_A_TRANSFERIR": 0,
       "CREDITO_A_PROPRIAR": 150.00, "CREDITO_NAO_UTILIZADO": 0, "CREDITO_UTILIZADO": 0,
       "DEBITO_EM_ABERTO": 150.00, "DEBITO_EXTINTO": 0}
    ]}
  }]
}
```

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test -count=1 ./services/... ./handlers/... -run CGIBS`

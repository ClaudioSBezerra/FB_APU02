# Addendum — Carga de Pagamentos SAP S/4HANA

Conteúdo técnico que aprofunda o PRD sem inflar sua narrativa principal. Destinado ao time de arquitetura/dev na próxima etapa (`bmad-create-architecture`, `bmad-create-epics-and-stories`).

## A. Referência da spec técnica SAP (fonte)

Documento fonte: "Especificação Técnica e Funcional — API de Consulta de Pagamentos por Chave de DF-e" v0.1, 09/07/2026, time FI/Basis (compartilhado em anexo pelo usuário em 2026-07-11). Status: em elaboração, pendente validação funcional.

**Pontos técnicos relevantes não repetidos no corpo do PRD:**

- Camada de dados do lado SAP: CDS views resolvendo a cadeia `J_1BNFE_ACTIVE`/`J_1BNFLIN` (Nota Fiscal Writer) → `RBKP` → `BKPF` → `BSAK`/`BSIK` (partidas compensadas/em aberto).
- Exposição via OData V4 (modelo RAP), autenticação OAuth2 client credentials ou X.509.
- Endpoints: `GET .../PaymentByDFe(dfeKey='{chave}')` (individual) e `POST .../SearchPayments` (lote, corpo `{"dfeKeys": [...]}`).
- Contrato de resposta por chave: `dfeKey`, `dfeType`, `matchType`, `companyCode` (BUKRS), `supplierCNPJ`, `supplierId`, `invoiceDocument`, `invoiceFiscalYear`, `invoiceDate`, `invoiceAmount`, `currency`, `paymentStatus`, `totalPaidAmount`, `payments[]` (cada item: `clearingDocument`, `clearingDate`, `paymentPostingDate`, `paidAmount`, `paymentMethod`, `paymentMethodDesc`, `settlementMode`, `isPartial`).
- Fallback para NFS-e/lançamentos diretos FI (FB60/FB01): busca por CNPJ do emitente + número do documento em janela de datas (± 90 dias configurável); ambíguo → melhor candidato + `fallbackNote`.
- Regra de `paymentStatus`: `PAGO_TOTAL` (soma pagamentos ≥ valor fatura e sem saldo em `BSIK`), `PAGO_PARCIAL` (liquidação parcial + saldo remanescente), `EM_ABERTO` (sem liquidação), `NAO_LOCALIZADO`.
- Códigos HTTP: 200 mesmo para `NAO_LOCALIZADO` (não é erro); 400 chave malformada ou lote acima do limite; 401/403 auth; 429 rate limit (header `Retry-After`); 500 erro interno com `correlationId`.
- NFR do lado SAP: consulta individual ≤ 2s p95; lote de 5000 chaves ≤ 160s p95 (a validar com massa real no QAS); rate limit sugerido 60 req/min por consumidor.
- **Inconsistência identificada:** seções 2.1/4.2 dizem limite de lote = 5000 chaves; seção 7 (código 400) e casos de teste 08/09 dizem 500. Ver PRD §13 Open Question #1.
- Casos de teste já definidos do lado SAP (12 cenários, seção 9 da spec) cobrem: match direto, pagamentos parciais, fatura em aberto, NF-e cancelada, chave malformada, fallback único/múltiplo, lote no limite/acima do limite, estorno filtrado, empresa fora do escopo, consumidor sem autorização.
- **Não aplicável ao FB_APU02** (item interno ao lado SAP, não incorporado a nenhuma FR): a spec deixa em aberto a confirmação dos nomes de campos das tabelas do NF Writer (`J_1BNFDOC`/`J_1BNFE_ACTIVE`) na release instalada. É um detalhe de implementação ABAP interno ao SAP — não afeta o contrato JSON externo consumido pelo FB_APU02, por isso não vira Open Question do PRD.

## B. Esboço de arquitetura do lado FB_APU02 (para a fase de arquitetura)

Sugestão de padrão, espelhando o já usado para RFB (`services/rfb.go`, `services/rfb_processor.go`, `services/rfb_scheduler.go`):

- `services/sap_payments.go` — cliente HTTP com cache de token OAuth2 (client credentials), análogo ao cache de token RFB já existente.
- `services/sap_payments_processor.go` — orquestra o lote: recebe chaves de `rfb_creditos`, monta requisições `SearchPayments` respeitando o tamanho de lote e rate limit, trata retry/backoff, grava resultado.
- Disparo: hook no fim do processamento que hoje grava `rfb_creditos` (ver `services/rfb_processor.go`) — chamar o novo processor de forma assíncrona (goroutine), sem bloquear a resposta ao webhook RFB.
- Nova tabela de credenciais: `sap_credentials` (padrão idêntico a `rfb_credentials`/`cgibs_credentials` — client_id, client_secret criptografado, base_url, bukrs_list).
- Nova tabela de execuções: `sap_sync_runs` (padrão idêntico a `erp_bridge_runs`) — company_id, **bukrs** (granularidade por código de empresa, não só por company_id — necessário para FR-6 tratar falha parcial de 1-de-N BUKRS isoladamente), iniciado_em, concluido_em, status, chaves_enviadas, chaves_pago_total, chaves_pago_parcial, chaves_em_aberto, chaves_nao_localizado, erro_detalhe.
- Idempotência do disparo (FR-1): antes de disparar uma nova sincronização para uma apuração, verificar em `sap_sync_runs` se já existe uma execução em andamento/concluída para o mesmo conjunto de chaves — evita chamada redundante à API SAP em caso de reentrega de webhook.

## C. Esboço de migration (schema)

Não é uma migration final — insumo para a fase de planejamento técnico:

```sql
-- Nova migration (112+): evolução de pagamentos_fornecedores para suportar origem SAP

ALTER TABLE pagamentos_fornecedores
    ADD COLUMN IF NOT EXISTS origem            VARCHAR(10) NOT NULL DEFAULT 'csv',   -- 'csv' | 'sap_api'
    ADD COLUMN IF NOT EXISTS payment_status     VARCHAR(20),                          -- espelha paymentStatus do SAP — indicador auxiliar, NUNCA decide status_conciliacao
    ADD COLUMN IF NOT EXISTS match_type         VARCHAR(20),                          -- CHAVE | FALLBACK
    ADD COLUMN IF NOT EXISTS bukrs              VARCHAR(4),                           -- código de empresa SAP (companyCode)
    ADD COLUMN IF NOT EXISTS settlement_mode    VARCHAR(50),                          -- modalidade art. 27 (texto bruto do SAP)
    ADD COLUMN IF NOT EXISTS fallback_ambiguous BOOLEAN NOT NULL DEFAULT FALSE;       -- true se FALLBACK com múltiplos candidatos — alimenta o filtro de revisão FR-12

-- Ajuste da constraint de deduplicação (ver FR-3):
-- de: UNIQUE (company_id, chave_doc, data_pagamento, valor_pagamento)
-- para: UNIQUE (company_id, chave_doc, num_doc_pagamento, bukrs) — exige backfill/validação de dados existentes
-- antes de trocar a constraint em produção (dados de CSV legado podem não ter num_doc_pagamento/bukrs preenchidos).
```

**Atenção para a fase de planejamento:** a troca da constraint de deduplicação é uma mudança sensível em dado de produção (compat., "Migrations existentes não podem ser alteradas, apenas adicionadas" — ver CLAUDE.md do projeto). Precisa de uma migration de backfill cuidadosa para linhas de CSV legado sem `num_doc_pagamento`/`bukrs`.

## D. Inversão da consulta de conciliação (FR-11) — esboço conceitual

O handler atual (`rfb_pagamentos_fornecedores.go`, CTE `pagamentos_agg` → `conciliacao`) parte de `pagamentos_fornecedores` e só depois faz `LEFT JOIN rfb_creditos` — um crédito RFB sem nenhum pagamento registrado hoje **não aparece em lugar nenhum da tela**. FR-11 exige inverter essa direção:

```sql
-- Esboço conceitual, não uma reescrita completa do handler:
FROM rfb_creditos rc
LEFT JOIN pagamentos_agg pa
    ON pa.company_id = rc.company_id
   AND pa.chave_doc  = rc.chave_dfe
-- status_conciliacao passa a ser determinado assim (rc.valor_cbs_nao_extinto é SEMPRE quem decide):
--   CASE
--     WHEN rc.valor_cbs_nao_extinto = 0 THEN 'extinto'
--     WHEN pa.chave_doc IS NOT NULL      THEN 'pendente'             -- saldo > 0 E há pagamento localizado (indicador)
--     ELSE                                     'aguardando_pagamento' -- saldo > 0 E nenhum pagamento localizado
--   END
-- 'sem_dados' (pagamento sem rfb_creditos correspondente) continua exigindo o caminho inverso
-- (FROM pagamentos_agg WHERE NOT EXISTS rfb_creditos) — provavelmente um UNION ou uma segunda query.
```

Este esboço é apenas conceitual — cabe à fase de arquitetura decidir se via `UNION`, `FULL OUTER JOIN` ou duas queries separadas compostas na camada de aplicação.

## E. Referências

- Lei Complementar 214/2025, arts. 27, 28 e 46.
- Ato Conjunto RFB/CGIBS nº 1/2025.
- Spec técnica SAP anexa (ver seção A).
- Código atual: `backend/migrations/110_pagamentos_fornecedores.sql`, `111_pagamentos_imports.sql`, `backend/handlers/pagamentos_fornecedores.go`, `backend/handlers/rfb_pagamentos_fornecedores.go`, `frontend/src/pages/RFBPagamentosFornecedores.tsx`.

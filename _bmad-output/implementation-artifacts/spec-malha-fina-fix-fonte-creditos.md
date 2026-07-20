---
title: 'Malha Fina: corrige fonte de dados de NF-e Entradas e CT-e (rfb_debitos → rfb_creditos)'
type: 'bugfix'
created: '2026-07-20'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '78839e4a4f7e46bce4a03273c121d091096282ee'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** As abas "NF-e Entradas" e "CT-e" da Malha Fina comparam documentos identificados pela RFB contra os documentos locais (`nfe_entradas`/`cte_entradas`) usando `rfb_debitos` — a tabela do lado de **débito/venda** da apuração CBS. Deveriam usar `rfb_creditos` (lado de **crédito/compra**, criado na migration 096, bem depois da materialized view `mv_malha_fina_resumo` que já tinha esse padrão). Confirmado por investigação de código: a linha do tempo dos commits (a MV é de antes de `rfb_creditos` existir; `malha_fina.go` foi editado depois e ninguém corrigiu), a semântica dos 2 endpoints RFB distintos (`/apuracao-cbs/v1` = débitos, `/creditos-cbs/v1` = créditos), e o próprio texto da tela ("NF-e identificadas pela Receita Federal que não foram importadas como entradas") que já promete o comportamento correto mas entrega dado do lado errado. Só a aba "NF-e Saídas" está correta hoje (débito é mesmo o lado certo pra vendas).

**Approach:** Generalizar a função compartilhada `malhaFinaList` (`backend/handlers/malha_fina.go`) para aceitar a tabela de origem e o nome da coluna de situação como parâmetros, já que `rfb_creditos`/`rfb_debitos` têm exatamente o mesmo shape de coluna (espelho documentado). Trocar a fonte de "NF-e Entradas" e "CT-e" para `rfb_creditos`/`situacao_credito`; manter "NF-e Saídas" em `rfb_debitos`/`situacao_debito`. Recriar `mv_malha_fina_resumo` com a mesma correção nos blocos `nfe-entradas` e `cte` (manter `nfe-saidas` como está).

## Boundaries & Constraints

**Always:**
- `MalhaFinaNFeSaidasHandler` e o bloco `nfe-saidas` da MV continuam lendo de `rfb_debitos`/`situacao_debito` — não tocar, está correto.
- `rfb_creditos` e `rfb_debitos` têm exatamente as mesmas colunas usadas por `malhaFinaList` (`chave_dfe`, `modelo_dfe`, `numero_dfe`, `data_dfe_emissao`, `data_apuracao`, `ni_emitente`, `ni_adquirente`, `valor_cbs_total`, `valor_cbs_extinto`, `valor_cbs_nao_extinto`, `tipo_apuracao`, só a coluna de situação muda de nome) — a generalização não precisa de nenhum tratamento condicional além de parametrizar nome de tabela e nome de coluna.
- Nenhuma mudança de frontend — `MalhaFinaPanel.tsx` já trata o campo de situação genericamente (`row.situacao_debito`, independente do tipo) e a descrição da tela já está correta.
- A migration que recria `mv_malha_fina_resumo` segue o mesmo padrão de `073_fix_mv_malha_fina_resumo.sql` (`DROP MATERIALIZED VIEW ... CASCADE` + `CREATE MATERIALIZED VIEW` completo + recriar os 2 índices).

**Ask First:** Nenhuma — já confirmado com o usuário que o bug deve ser corrigido antes de qualquer outra coisa.

**Never:** Não adicionar filtro de `modelo_dfe` novo em `rfb_creditos` (a tabela já não restringe modelo, aceita NF-e e CT-e) — a distinção de tipo continua sendo feita pelos parâmetros já existentes (`modelosDFe`, `excludeTable`).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Aba "NF-e Entradas" | Usuário abre `/malha-fina/nfe-entradas` | Lista compara `rfb_creditos` (modelo 55/65) contra `nfe_entradas` local | N/A |
| Aba "CT-e" | Usuário abre `/malha-fina/cte` | Lista compara `rfb_creditos` (modelo 57) contra `cte_entradas` local | N/A |
| Aba "NF-e Saídas" (não muda) | Usuário abre `/malha-fina/nfe-saidas` | Continua comparando `rfb_debitos` contra `nfe_saidas`, comportamento idêntico a hoje | N/A |
| Resumo por emitente (MV) | Refresh da MV após correção | Blocos `nfe-entradas`/`cte` da `mv_malha_fina_resumo` recalculados a partir de `rfb_creditos` | N/A |
| Empresa sem nenhum dado em `rfb_creditos` | Nunca baixou créditos CBS via RFB | Abas "NF-e Entradas"/"CT-e" mostram lista vazia (0 registros), sem erro | N/A |

</frozen-after-approval>

## Code Map

- `backend/handlers/malha_fina.go` -- generalizar `malhaFinaList` para aceitar `sourceTable string, situacaoCol string` como novos parâmetros; usar `sourceTable` na `fromClause` (hoje hardcoded `"rfb_debitos rd LEFT JOIN..."`) e `situacaoCol` no `SELECT` de dados (hoje hardcoded `COALESCE(rd.situacao_debito, '')`); atualizar `MalhaFinaNFeEntradasHandler` e `MalhaFinaCTeHandler` para passar `"rfb_creditos", "situacao_credito"`; `MalhaFinaNFeSaidasHandler` passa `"rfb_debitos", "situacao_debito"` (inalterado em efeito).
- `backend/migrations/125_fix_malha_fina_creditos.sql` (nova) -- `DROP MATERIALIZED VIEW IF EXISTS mv_malha_fina_resumo CASCADE` + recriar com os blocos `nfe-entradas` e `cte` lendo `FROM rfb_creditos rd` (bloco `nfe-saidas` idêntico ao atual, continua `FROM rfb_debitos rd`); recriar os 2 índices (`mv_malha_fina_resumo_pk`, `mv_malha_fina_resumo_company_tipo`) exatamente como em `073_fix_mv_malha_fina_resumo.sql`.

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/malha_fina.go` -- generalizar `malhaFinaList` (2 novos parâmetros) e atualizar os 3 call sites -- corrige a fonte de dados da aba principal de NF-e Entradas e CT-e
- [x] `backend/migrations/125_fix_malha_fina_creditos.sql` -- recriar a MV com a mesma correção -- corrige a fonte de dados do resumo por emitente/dia

**Acceptance Criteria:**
- Given uma empresa com dados em `rfb_creditos` para um CNPJ ainda não importado localmente como entrada, when o usuário abre "Malha Fina — NF-e Entradas", then esse CNPJ aparece na lista como `AUSENTE`.
- Given a mesma empresa, when o usuário abre "Malha Fina — NF-e Saídas", then o comportamento é idêntico ao de antes desta mudança (continua lendo `rfb_debitos`).
- Given a materialized view é atualizada (refresh manual ou automático), when a aba de Resumo é consultada para tipo `nfe-entradas` ou `cte`, then os números refletem `rfb_creditos`, não mais `rfb_debitos`.

## Design Notes

Diferente da decisão de manter os 3 shells de submenu vertical (DFe-s/Notas/Malha Fina) como implementações-irmãs sem abstração compartilhada, aqui a duplicação é de lógica de geração de SQL sobre 2 tabelas com shape idêntico (espelho documentado na própria migration 096) — generalizar `malhaFinaList` com 2 parâmetros a mais é a mudança de menor risco e menor blast radius, não introduz uma abstração especulativa.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: sem erros de compilação
- `cd backend && go vet ./handlers/...` -- expected: limpo

**Manual checks (if no CLI):**
- Comparar uma consulta manual em `rfb_creditos`/`nfe_entradas` (via psql, se disponível) com o resultado da aba "NF-e Entradas" após a correção, para confirmar que os `AUSENTE`/`CANCELADA` batem.
- Confirmar que "NF-e Saídas" continua idêntica (mesma contagem antes/depois desta mudança).
- **Nota de execução:** sem Postgres local disponível nesta sessão (sem acesso sudo) — verificação foi só estática (compilação limpa + diff estrutural byte-a-byte da migration contra a versão anterior). Rodar a verificação manual acima quando houver acesso a um banco real.

## Suggested Review Order

**Correção principal**

- Generalização de `malhaFinaList` (2 novos parâmetros) e os 3 call sites — só "NF-e Entradas"/"CT-e" mudam de fonte, "NF-e Saídas" fica idêntico.
  [`malha_fina.go:58`](../../backend/handlers/malha_fina.go#L58)

- Migration que recria a MV com a mesma correção nos blocos `nfe-entradas`/`cte`.
  [`125_fix_malha_fina_creditos.sql:1`](../../backend/migrations/125_fix_malha_fina_creditos.sql#L1)

- Refresh da MV adicionado ao branch de créditos do webhook — achado de revisão: sem isso, a MV nunca invalidaria para os 2 tipos agora dependentes de `rfb_creditos`.
  [`rfb_apuracao.go:558`](../../backend/handlers/rfb_apuracao.go#L558)

**Achados da revisão adversarial — contexto importante para priorização futura**

- **Tranquilizador:** as 2 abas corrigidas estão com `rfbDisponivel: false` desde que existem — o bug corrigido aqui nunca teve impacto visível a usuários (nem a lista nem o resumo por tipo são consultados enquanto isso). Detalhes em `deferred-work.md`.
- **Atenção, achado separado e possivelmente ativo:** o filtro de filial (`filial_cnpj` → `ni_adquirente`) está provavelmente invertido na aba "NF-e Saídas", que é a única habilitada hoje — não corrigido nesta spec, registrado com prioridade em `deferred-work.md`.

---
title: 'RFB: plano de migração CBS débitos/créditos v1 → v2'
type: 'feature'
created: '2026-09-24'
status: 'ready-for-dev'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A RFB republicou (17/09/2026) a API de apuração CBS como v2 — não é só troca de versão: URL muda de estrutura (`apuracao-cbs/v1/{cnpj}` + `creditos-cbs/v1/{cnpj}` → `apuracao-cbs/v2/{debitos|creditos}/{cnpj}`), o schema de resposta muda completamente (sem os blocos `apuracaoCorrente`/`apuracaoAjuste`/`debitosExtemporaneos` de onde hoje deriva `tipo_apuracao`), e o mecanismo de download pode mudar (v2 menciona `urlAssinada`, hoje o código sempre chama `GET /download/v1/{tiquete}`). Corte anunciado para início de outubro/2026 — prazo curto para um redesenho desta profundidade num módulo com apuração fiscal real em produção.

**Approach:** esta spec é só PLANEJAMENTO — mapeia toda a superfície de mudança e lista as decisões de design que precisam de aprovação humana antes de qualquer implementação. Não gera código. Produz, como Tasks, uma sequência de sub-specs implementáveis (cada uma um `bmad-quick-dev` separado) uma vez que as decisões abaixo estiverem resolvidas.

## Boundaries & Constraints

**Always:** preservar dedup por `chave_dfe` (UPSERT já corrigido no commit `2ada5df`); preservar leitura de dados v1 já persistidos (não migrar/apagar histórico); qualquer sub-spec de implementação segue `plan-code-review` (não one-shot) dado o blast radius fiscal; manter `rfbAPIVersion` como ponto único de troca de versão.

**Decisões resolvidas (2026-09-24):**
1. **`data_apuracao`**: MIGRAR colunas (`rfb_debitos`/`rfb_resumo`: `VARCHAR(6)`→`VARCHAR(10)`), guardando o formato v2 cru (`"mm/aaaa"`). A migration DEVE também converter/normalizar os valores v1 já persistidos (`"AAAAMM"`) para o mesmo formato `"mm/aaaa"` — sem isso, a mesma coluna passa a ter 2 formatos coexistindo, o que quebra `GROUP BY`/`ORDER BY`/comparação de período (ex.: `"202601"` e `"01/2026"` seriam tratados como períodos diferentes para o mesmo mês real). Backfill é parte obrigatória desta migration, não opcional.
2. **Equivalente de `tipo_apuracao`**: pesquisa confirmou que `origem`/`documento` respondem "o que é o lançamento" (normal/cancelamento/devolução/tipo de documento), não "quando ele apareceu na apuração" — não há mapeamento direto. **Opção B escolhida**: aproximar por data — mês(`pa`) == mês(`registro`) → `"corrente"`; mês(`registro`) posterior ao mês(`pa`) → `"extemporaneo"` (funde o que antes era `ajuste`+`extemporaneo`, já que não dá pra distinguir os dois só por data). Mantém as colunas/telas atuais (`total_corrente`/`total_ajuste`/`total_extemporaneo`) com aproximação funcional — `total_ajuste` deve zerar ou ser removido da UI nesse novo cálculo (só sobra corrente/extemporâneo).
3. **Download**: criar fallback — se o webhook trouxer `urlAssinada`, baixar direto dela; se não vier, cair para `GET .../v2/download/{tiquete}` como hoje. Convivência até a RFB confirmar que `urlAssinada` é definitivo.
4. **Webhook de erro**: tratar `codigoErro`/`mensagemErro` quando vierem no webhook — marcar `rfb_requests.status='error'` com os campos correspondentes, em vez de descartar como "missing tiquetes".
5. **Campos novos**: migration aditiva na primeira oportunidade (não adiar) — débitos ganham `excedente`/`inexigivel`/`suspenso`/`saldo_devedor`; créditos ganham a árvore de apropriação/utilização (colunas exatas a detalhar na sub-spec de schema).
6. **Cutover**: suportar v1 E v2 simultaneamente — o parser precisa detectar o shape do JSON (blocos `apuracaoCorrente`/etc. = v1, lista plana `apuracao[].debitos[]` = v2) e despachar para a lógica correta. Necessário de qualquer forma para `ReprocessarRawJSON` continuar lendo `raw_json` histórico já salvo em formato v1.
7. **Limite diário**: elevar `LimiteDebitosDiaRFB` de 2 para 4 (mantendo `LimiteDebitosDiaAgendamento=1`, sobrando 3 slots pra chamada manual/retry em vez de 1).

**Never:** implementar pagamentos/recolhimentos (greenfield, sem pressão de prazo, spec futura separada); alterar migrations existentes (só aditivas); decidir sozinho qualquer item "Ask First" acima sem resposta do usuário.

</frozen-after-approval>

## Code Map

- `backend/services/rfb.go` -- URLs (`SolicitarApuracao:220`, `SolicitarCredito:298`, `DownloadArquivo:358`), `pathPrefix`/`SetAmbiente:113-119`, `rfbAPIVersion:20`
- `backend/services/rfb_processor.go` -- structs `RFBApuracaoJSON`/`RFBGrupoDebitos`/`RFBDebito`, `ProcessarDownloadRFB`, `aggregateRfbDebitosSQL`, `tipoApuracao` derivado por bloco (L305/339/373)
- `backend/services/rfb_creditos_processor.go` -- structs `RFBCreditosJSON`/`RFBCredito`, `ProcessarDownloadCreditosRFB`, `insertCredito`
- `backend/handlers/rfb_apuracao.go` -- `RFBWebhookHandler:580-716`, parse de `tiqueteSolicitacao`/`tiqueteDownload`
- `backend/migrations/053_create_rfb_debitos.sql`, `054_create_rfb_resumo.sql`, `096_rfb_creditos.sql` -- schema atual, referência para migration aditiva
- `backend/services/rfb_scheduler.go` -- `LimiteDebitosDiaAgendamento/RFB:66-69`

## Tasks & Acceptance

**Execution:**
- [x] Resolver as 7 decisões "Ask First" com o usuário -- respostas registradas acima (2026-09-24).
- [x] `spec-rfb-cbs-v2-schema.md` -- 1ª tentativa (só migration) revertida via loopback intent_gap: revisão adversarial (2x) achou que o backfill de `data_apuracao` sozinho quebra 5+ pontos de leitura (`rfb_debitos_lista.go` periodo/ano/dropdown, `rfb_creditos.go` periodo, frontend `RFBDebitos.tsx`) e, mais fundamental, o parser continuaria escrevendo `"AAAAMM"` até a sub-spec seguinte, recriando a coexistência de formatos. **Decisão do humano:** manter "migrar colunas" (não normalizar no parse), mas fundir schema+leituras+parser num ÚNICO deploy -- ver `spec-rfb-cbs-v2-schema.md` (escopo ampliado, mesmo arquivo).
- [x] `spec-rfb-cbs-v2-schema.md` (implementação, escopo ampliado) -- migration (widen+backfill `data_apuracao`, colunas novas débitos/créditos) + os 5+ pontos de leitura afetados + parser v1/v2 dual-support com bucketing por data -- tudo num só deploy, não pode ser splitado (é exatamente o que causou o loopback).
- [ ] `spec-rfb-cbs-v2-urls-download.md` (implementação) -- migrar `rfb.go` para paths v2, implementar fallback `urlAssinada`→`GET download` (decisão 3), elevar limite diário para 4 (decisão 7).
- [ ] `spec-rfb-cbs-v2-webhook.md` (implementação) -- tratar `codigoErro`/`mensagemErro` no webhook (decisão 4).

**Acceptance Criteria:**
- Given `spec-rfb-cbs-v2-schema.md` (escopo ampliado), when implementada, then é um único deploy atômico -- nunca migration/backfill sem o parser correspondente no mesmo release.
- Given as demais sub-specs (urls/webhook), when abertas independentemente, then não dependem de mudança de formato de `data_apuracao`, só da spec de schema já ter criado as colunas novas.

## Design Notes

Achados que não bloqueiam decisão (podem ir direto pras sub-specs quando chegar a hora):
- Inconsistência de schema pré-existente `rfb_debitos` vs `rfb_creditos` (tipos/larguras diferentes para `data_dfe_emissao`, `valor_cbs_*`, `formas_extincao`) não é causada pelo v2, mas relevante para decisão 5 (reaproveitar tabelas vs redesenhar).
- `aggregateRfbDebitosSQL`/`aggregateRfbCreditosSQL` (commit `2ada5df`) já usam `COALESCE(SUM(...),0)` por coluna — colunas novas ficando NULL em linhas v1 antigas não deve quebrar a agregação, só contribui 0.
- Nenhum polling de status de tiquete existe hoje (fluxo 100% webhook) — o endpoint `GET .../v2/situacao/{tiquete}` documentado pela RFB é opcional a menos que decisão 3/4 exijam fallback ativo.

## Verification

**Manual checks (nesta rodada, sem CLI):**
- Confirmar que as 7 perguntas foram respondidas e refletidas nesta spec antes de abrir qualquer sub-spec de implementação.

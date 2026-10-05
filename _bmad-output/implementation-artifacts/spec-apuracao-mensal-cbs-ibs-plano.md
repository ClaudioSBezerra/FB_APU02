---
title: 'Apuração Mensal CBS/IBS: validação do que já existe + plano de saldo a pagar/transportar'
type: 'feature'
created: '2026-10-05'
status: 'done'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** o usuário quer que o sistema "deixe muito claro" a apuração mensal de CBS/IBS, no mesmo espírito do ICMS/PIS-COFINS (crédito − débito = saldo, saldo credor transporta). Antes de desenhar isso, pesquisamos a fundo (dois subagentes, PDFs locais da RFB + web) e confirmamos que **o modelo real é bem diferente do ICMS**: não é um corte mensal simples — é um ledger operação-a-operação onde o crédito do adquirente só nasce quando o débito da contraparte (fornecedor) extingue (pagamento, RAD, split payment ou compensação), existe decadência real de 5 anos pra usar o crédito já apropriado (diferente do ICMS), e o split payment pode liquidar boa parte do tributo fora do ciclo mensal. Esta spec valida o que já implementamos (CGIBS conta corrente fiscal + RFB CBS débitos/créditos) contra essas regras e aponta o que falta.

**Approach:** spec de planejamento (não implementação) — valida schema/handlers existentes linha a linha contra as 2 memórias de pesquisa (calendário + compensação/decadência/split payment), separa o que já está correto do que precisa de camada nova, e propõe fases. Decisões de produto ficam pra seção "Ask First" abaixo — não implementar nada até o usuário responder.

## Validação do que já existe

### IBS (CGIBS) — schema de captura já está certo, falta só a camada de apuração por cima

`cgibs_lancamentos` (migration 131) já grava exatamente os 7 campos incrementais que a mecânica real exige (`credito_a_apropriar`/`credito_nao_utilizado`/`credito_utilizado`, `debito_em_aberto`/`debito_extinto`, `recurso_financeiro_disponivel`/`recurso_financeiro_a_transferir`), por lançamento, com timestamp (`dth_lancto`) e vínculo à operação (par fornecedor×adquirente via `cgibs_operacoes`). **Isso já é o "ledger operação-a-operação" que a pesquisa disse ser necessário — nenhuma mudança de schema de captura é exigida.**

O que falta é só uma CAMADA DE LEITURA por cima do que já temos:
1. Calendário real: o resumo (`ResumoCGIBSHandler`, já existente) hoje calcula "ao vivo" pra qualquer período pedido, sem noção de "esse mês ainda está em Período de Ajuste, o saldo pode mudar". Isso está certo para visualização a qualquer momento, mas precisa deixar claro na UI quando um saldo é "prévia" (mês ainda em ajuste) vs. "definitivo" (depois do último dia útil do mês seguinte, ou dia 25/26 no piloto 2026).
2. "Saldo do período" = `debito_extinto − credito_utilizado` somados no range — é literalmente o que `ResumoCGIBSHandler` já soma, só que hoje apresenta os 7 campos como 3 blocos neutros ("Recursos financeiros"/"Créditos"/"Débitos") sem rotular qual combinação é o saldo a pagar de fato.
3. Idade do crédito apropriado (alerta de decadência, 5 anos): **já é computável com os dados que temos** — o `dth_lancto` do lançamento que moveu valor pra `credito_nao_utilizado` É a data de apropriação. Só falta a query/relatório, não precisa mudar schema.
4. Split payment/RAD: hoje só aparece como texto livre em `mov_descricao` (ex.: "RAD provisionado", "SPLIT provisionado", vistos nos exemplos do MOC) — funciona pra busca textual, mas frágil; vai melhorar quando a CGIBS publicar a tabela oficial de códigos MOV (ainda não existe, é pendência conhecida deles, não nossa).

### CBS (RFB) — schema atual é raso demais pra replicar a mesma camada agora

`rfb_debitos`/`rfb_creditos` (migrations 053/096) só têm `valor_cbs_total/extinto/nao_extinto` — um corte BINÁRIO (extinto vs. não extinto), sem o detalhamento `a_apropriar → não_utilizado → utilizado` que o IBS já captura. Isso bate com uma pendência JÁ DOCUMENTADA na memória do plano RFB v2 (`project_rfb_cbs_api_incremental_2026`, item 7: "avaliar migration pros campos novos de crédito").

**Achado que muda a prioridade** (reconfirmado 2026-10-05 direto em `docs.receitafederal.gov.br/apuracao-cbs/credito.html`, não só por memória): o schema de créditos v2 traz, por documento, `cbs.apropriacao.{inapropriavel,suspenso,prescrito,aApropriar,apropriado}` e, dentro de `apropriado`, `utilizacao.{inutilizavel,utilizado,restabelecido,naoUtilizado.{saldoCredor,pedidoRessarcimento}}` — `prescrito` é um **valor monetário** (quanto desse crédito já decaiu), não um status booleano. Ou seja, **a própria RFB já calcula e expõe pronto quanto de cada crédito expirou**, diferente do IBS onde teríamos que calcular a idade do crédito por conta própria a partir do `dth_lancto`. A doc não cita o prazo de "5 anos" explicitamente (o valor já vem calculado, a regra em si não precisa aparecer na API) — esse número continua vindo só das fontes jurídicas da pesquisa anterior. **Recomendação: não construir cálculo manual de decadência pro CBS agora — esperar a v2 trazer `prescrito` nativo** (a sub-spec de parser v2 já está mapeada como pendência, não precisa de trabalho novo aqui).

`eventos`/`formas_extincao` em `rfb_debitos`/`rfb_creditos` já guardam o JSON bruto da RFB (passthrough, nunca interpretado) — nenhum dado de modalidade de extinção está sendo perdido hoje, só não está estruturado/consultável.

### Split payment — não modelar ainda

Nenhum tributo (CBS ou IBS) tem campo estruturado pra split payment hoje — só texto livre (IBS) ou JSON opaco (CBS). Como o cronograma de obrigatoriedade do split payment ainda não foi definido por ato conjunto (pesquisa confirmou isso), e 2026 é ambiente de TESTE sem efeito tributário real, **não vale investir agora** numa feature de detecção/relatório de split payment — o risco de desenhar contra um shape que ainda não existe de verdade é alto. Revisitar quando tivermos volume real de operações com split payment confirmado.

## Decisões "Ask First" (responder antes de qualquer sub-spec de implementação)

1. **Escopo da Fase 1**: confirma que começamos só pelo **IBS** (schema já pronto, é trabalho de camada de leitura, não de captura) e deixamos o CBS pra quando a v2 trazer os campos de apropriação granulares? Ou quer as duas juntas mesmo com o CBS raso hoje?
2. **Saldo "prévia" vs. "definitivo"**: durante o Período de Ajuste (do dia 15/20 até o último dia útil do mês seguinte — ou dia 25/26 fixo no piloto 2026), o contribuinte ainda pode mexer nos valores. O sistema deve (a) mostrar um saldo "ao vivo" sempre, com aviso claro de que é prévia até a data de fechamento, ou (b) só mostrar o saldo quando o período realmente fechar?
3. **Alerta de decadência (5 anos)**: entra já na Fase 1 (é barato, só precisa de uma query sobre `dth_lancto` de créditos `nao_utilizado`), ou fica pra depois?
4. **Calendário piloto vs. real**: o sistema precisa saber diferenciar "estamos no ambiente de teste 2026 (datas fixas dia 25/26)" de "estamos em produção real a partir de 2027 (último dia útil, variável por domicílio)"? Isso muda a lógica de quando considerar um mês "fechado".

**Never:** implementar split payment estruturado agora (sem schema confirmado); replicar decadência manual pro CBS (vai vir pronto na v2); mexer na captura/schema do IBS (já está correta).

## Decisões resolvidas (2026-10-05)

1. **Escopo**: IBS + CBS juntos nesta rodada (usuário optou por não esperar a v2 do CBS) — o saldo do CBS é explicitamente rotulado como **aproximado**, já que o schema v1 só tem o corte binário extinto/não-extinto, sem a granularidade de apropriação que o IBS já tem.
2. **Saldo prévia/definitivo**: mostrar sempre, com rótulo claro ("prévia — sujeita a ajuste até DD/MM" ou "definitivo") baseado no `status_periodo` calculado.
3. **Alerta de decadência**: entra nesta fase (IBS apenas — CBS não precisa, a v2 vai trazer `prescrito` pronto).
4. **Calendário**: suportar os dois modos (piloto 2026 com datas fixas dia 25/26; real pós-2027 com último dia útil do mês) via parâmetro/config — não travar num só.

</frozen-after-approval>

## Code Map

- `backend/handlers/cgibs_resumo.go` (`ResumoCGIBSHandler`) — ponto de partida da Fase 1 (rotular/estender o resumo existente)
- `backend/migrations/131_cgibs_conta_corrente_fiscal.sql` — schema de captura (referência, não alterar)
- `backend/handlers/cgibs_operacoes.go` (`LancamentosOperacaoCGIBSHandler`) — fonte do `dth_lancto` pra cálculo de idade do crédito
- `backend/migrations/053_create_rfb_debitos.sql`, `096_rfb_creditos.sql` — schema raso do CBS (referência, não mudar nesta fase)
- `_bmad-output/implementation-artifacts/spec-rfb-cbs-v2-migracao.md` — plano-mãe do CBS v2, onde o detalhamento de crédito/`prescrito` já está mapeado como pendência

## Tasks & Acceptance

**Execution:**
- [x] Responder as 4 decisões "Ask First" acima com o usuário.
- [x] (Após decisões) Escrever sub-spec de implementação da Fase 1 (Apuração Mensal IBS — rótulo de saldo + aviso prévia/definitivo + alerta de decadência, conforme decisões 2 e 3).

**Acceptance Criteria:**
- Given as decisões "Ask First" respondidas, when a sub-spec de implementação for escrita, then ela referencia só os campos/tabelas já existentes (nenhuma migration de captura nova é necessária pro IBS).

## Verification

**Manual checks (nesta rodada, sem CLI):**
- Confirmar que as 4 perguntas foram respondidas antes de abrir qualquer sub-spec de implementação.

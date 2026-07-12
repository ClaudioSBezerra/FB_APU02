# Reconciliação — Spec SAP v0.1 vs PRD/Addendum "Carga de Pagamentos SAP S/4HANA"

> Método: extract, don't ingest. Este relatório aponta **ausências e incompletudes** do PRD/addendum em relação ao documento-fonte. Não avalia se algo deveria estar no PRD nem sugere mudança de escopo — apenas rastreia cobertura.

**Fonte:** `source-sap-payment-api-spec-v0.1.md`
**Verificado contra:** `prd.md` + `addendum.md`

---

## 1. Regras de negócio da seção 6 da spec — cobertura por FR

| Regra da spec (§6) | Cobertura no PRD/addendum | Status |
|---|---|---|
| 6.1 Fluxo principal (decomposição de chave, DOCNUM em J_1BNFE_ACTIVE, apenas autorizados/não cancelados, J_1BNFLIN→RBKP→BKPF→BSIK/BSAK) | Addendum §A resume a cadeia de CDS views; PRD trata como caixa-preta consumida via `SearchPayments`/`PaymentByDFe` (FR-1, FR-2) | **OK** — apropriadamente abstraído como dependência externa |
| 6.2 Fallback NFS-e/lançamentos diretos FI (FB60/FB01), janela ±90 dias, ambiguidade → melhor candidato + fallbackNote | Addendum §A resume; PRD FR-8 trata exibição de `matchType=FALLBACK` com ambiguidade; PRD §8 Riscos reconhece explicitamente que o fallback também se aplica a lançamentos diretos de NF-e/CT-e (FB60/FB01), não só NFS-e | **OK** — nuance de que o fallback não é exclusivo de NFS-e foi preservada |
| 6.2 (ponto em aberto explícito da spec): layout da chave NFS-e nacional a confirmar antes do build | PRD Não-Metas §5 e Open Question #1 (§13) | **OK** |
| 6.3 Pagamentos parciais/residuais (REBZG agregado à fatura original, cadeia até fatura raiz) | PRD FR-3 e Glossário definem "pagamento por conta"; cadeia REBZG em si é interna ao SAP (abstraída) | **OK** |
| 6.3 Adiantamentos (razão especial, UMSKZ) — "fora do escopo v1, **aparecem naturalmente se vinculados na compensação**" | PRD Não-Metas §5: "Não inclui tratamento de adiantamentos (razão especial) — também fora do escopo da API SAP v1." Trata como exclusão limpa. | **GAP (nuance perdida)** — a spec não diz que adiantamentos simplesmente não existem no dataset; diz que eles podem aparecer "naturalmente" na compensação mesmo sem tratamento dedicado. O PRD não discute se esse valor contamina o `totalPaidAmount`/`paidAmount` usado pela regra de extinção automática por valor em FR-4. Não há nenhuma menção, FR, Não-Meta ou risco que trate da interação adiantamento × regra de fechamento automático por valor. |
| 6.3 Compensações que não são pagamento (estornos/reclassificações) filtradas por BLART configurável (sugestão: apenas KZ e ZP) | Não encontrado em nenhum lugar do PRD ou addendum. String "BLART" não aparece em nenhum dos dois documentos. | **GAP** — regra de negócio inteira (filtro de tipo de documento contábil para excluir estornos/reclassificações do cálculo de pagamento) não tem representação em nenhuma FR, Não-Meta, risco ou pergunta em aberto do PRD. |
| 6.4 Mapeamento forma de pagamento → modalidade art. 27 (explicitamente NÃO obrigatório na spec) | PRD Não-Metas §5 ("não inclui a tabela de-para completa... armazenar o valor textual"), migration inclui coluna `settlement_mode`, Open Question #5 | **OK** |
| 6.5 Determinação de paymentStatus (PAGO_TOTAL/PAGO_PARCIAL/EM_ABERTO/NAO_LOCALIZADO) | Addendum §A reproduz a regra verbatim; PRD FR-4 mapeia os 4 status para o modelo interno | **OK** |

---

## 2. Casos de teste da seção 9 (12 cenários) — cobertura

| # | Cenário (spec §9) | Aparece no PRD/addendum? |
|---|---|---|
| 01 | Match direto pago integral | Sim — subsumido em FR-4 (PAGO_TOTAL) |
| 02 | Dois pagamentos parciais | Sim — FR-3 ("pagamentos por conta"), payments[] → linhas próprias |
| 03 | Fatura em aberto | Sim — FR-4 (EM_ABERTO) |
| 04 | NF-e cancelada → NAO_LOCALIZADO | Nomeado apenas na lista-resumo do addendum §A ("NF-e cancelada"); não tem tratamento diferenciado — é subsumido genericamente em NAO_LOCALIZADO (FR-4/FR-8) sem distinguir "documento cancelado" de outras causas de não localização |
| 05 | Chave DV inválido → 400 | **Parcial/ausente** — FR-2 só especifica o 400 causado por "lote grande demais" (reduz tamanho e retenta); não há menção a como o sistema trata uma chave malformada individual dentro de um lote (o lote inteiro falha? a chave é filtrada antes do envio?) |
| 06 | Fallback candidato único | Sim — FR-8 |
| 07 | Fallback múltiplos candidatos | Sim — FR-8, fallbackNote |
| 08 | Lote com 500 chaves mistas → 200 | Sim — FR-2 (decisão de produto de usar lotes de 500) |
| 09 | Lote com 501 chaves → 400 | Sim — FR-2 (mesmo raciocínio) |
| 10 | Estorno filtrado (BLART fora da lista) | **GAP** — citado apenas na lista-resumo do addendum §A ("estorno filtrado"), sem nenhuma FR, Não-Meta ou observação correspondente no PRD. Ver também gap de BLART na seção 1 acima. |
| 11 | Fatura de empresa fora do escopo → NAO_LOCALIZADO (sem vazamento) | Sim — FR-10, explicitamente referenciado |
| 12 | Consumidor sem autorização → 401/403 | **Parcial** — não há FR que trate especificamente de resposta 401/403 da API SAP (ex.: não retentar, alertar credencial inválida). FR-2 só detalha comportamento para 429 e 400; FR-7 menciona "erros" de forma genérica, sem diferenciar falha de autenticação de falha transitória |

---

## 3. Requisitos não funcionais da seção 8 — cobertura em PRD §9/§10

| NFR da spec (§8) | Aparece no PRD? |
|---|---|
| Desempenho: individual ≤2s p95; lote 5000 ≤160s p95 | Lote citado em PRD §9 Desempenho ("conforme spec"); SLA da consulta individual (≤2s p95) não é mencionado — consistente com o fato de o PRD não referenciar uso do endpoint `PaymentByDFe` individual em nenhuma FR (só `SearchPayments` em lote é usado) |
| Volumetria mensal a levantar | Sim — PRD Open Question #3 (§13) |
| **Disponibilidade: horário comercial estendido 7h-22h dias úteis + janelas de manutenção RISE** | **GAP** — PRD §9 e §10 não mencionam a janela de disponibilidade 7h-22h em nenhum lugar. O risco "Indisponibilidade da API SAP" em §8 (Riscos e Mitigações) só cita "janelas de manutenção do RISE" de forma genérica, sem a janela horária diária específica — que tem implicação operacional direta (execuções fora de 7h-22h em dias úteis podem falhar sistematicamente, e a apuração RFB que dispara a sincronização via FR-1 não necessariamente respeita esse horário) |
| Segurança: OAuth2 client credentials **ou X.509**, usuário somente leitura restrito por BUKRS, TLS 1.2+ | PRD §9 Segurança cobre OAuth2, TLS 1.2+, criptografia, acesso admin-only. **X.509 como alternativa de autenticação** (mencionado na spec e reproduzido no addendum §A) não é mencionado no corpo do PRD — FR-5 comprometeu-se apenas com OAuth2 sem observação explícita de que a alternativa X.509 da spec foi descartada |
| LGPD: dados PJ; avaliar minimização/DPO se PF entrar | Sim — PRD §10 Constraints (Compliance), quase verbatim |
| Auditoria: log de aplicação por requisição (consumidor, qtd chaves, tempo resposta, correlationId) | Parcial — PRD §9 Observabilidade + FR-7 cobrem correlationId, quantidade de chaves e status por paymentStatus; **tempo de resposta por execução não é citado explicitamente** como campo de auditoria (embora `iniciado_em`/`concluido_em` no esboço de schema do addendum §B permitam derivá-lo implicitamente) |
| Rate limiting: sugestão 60 req/min | Sim — FR-2, citado verbatim |

---

## 4. Questões em aberto da seção 10 (6 itens) — cobertura em PRD §13

| # | Questão da spec (§10) | Aparece no PRD §13? |
|---|---|---|
| 1 | Layout/decomposição da chave NFS-e nacional | Sim — Open Question #1 |
| 2 | Definir lista de empresas (BUKRS) no escopo **e** tipos de documento de pagamento (BLART) válidos | **Parcial** — PRD Open Question #2 cobre apenas a parte de mapeamento `company_id`↔BUKRS ("1 ou N códigos de empresa SAP"); a parte de **definição da lista de BLART válidos não aparece em nenhum lugar do PRD** (consistente com o gap de BLART já apontado nas seções 1 e 2 acima) |
| 3 | Tabela de-para ZLSCH → modalidade art. 27, incluindo split payment futuro | Sim — Open Question #5 |
| 4 | Padrão de exposição externa no RISE (Cloud Connector/API Management/WAF) | Sim — Open Question #4 |
| 5 | Volumetria mensal esperada | Sim — Open Question #3 |
| 6 | Confirmar nomes de campos das tabelas do NF Writer (J_1BNFDOC/J_1BNFE_ACTIVE) na release instalada | **GAP** — não aparece em nenhum lugar do PRD ou addendum. Some sem nenhuma nota explicando a omissão (diferente das demais exclusões do PRD, que costumam vir acompanhadas de uma justificativa explícita em Não-Metas) |

---

## 5. Nuances qualitativas potencialmente achatadas pela estrutura de FRs

- **Adiantamentos (UMSKZ) vs. residuais comuns (REBZG):** o PRD trata os dois como se fossem a mesma categoria de "pagamento parcial" tratada por FR-3, exceto pela exclusão blanket de adiantamentos em Não-Metas. A spec, porém, distingue claramente: REBZG é uma regra de agregação normal (documentada e implementada no fluxo principal, §6.3), enquanto UMSKZ (razão especial de adiantamento) é uma categoria à parte que **não tem tratamento dedicado mas pode aparecer nos dados retornados mesmo assim**. O PRD não discute o que acontece se um valor de adiantamento entrar no `totalPaidAmount` usado pela regra de extinção automática por valor (FR-4) — a regra de FR-4 fecha o crédito quando "valor acumulado de pagamentos... atinge o valor total da nota", sem excluir explicitamente valores de adiantamento dessa soma.
- **"Não é erro HTTP" (200 com NAO_LOCALIZADO) vs. demais códigos de erro:** essa distinção foi preservada razoavelmente bem — FR-4 trata NAO_LOCALIZADO/EM_ABERTO como respostas de negócio normais (não geram linha, mas ficam no log), enquanto FR-2 trata 429/400 como falhas técnicas com retry/backoff. Não identificado como gap relevante.
- **401/403 vs. 429/500 (retry policy):** a spec lista os 5 códigos de retorno (200, 400, 401/403, 429, 500) no mesmo nível, mas o PRD (FR-2) só detalha comportamento de retry/backoff para 429 e redução de lote para 400. Não há distinção explícita entre erros não-retentáveis (401/403 — problema de credencial, retentar não ajuda) e erros potencialmente transitórios (500, 429) — ambos caem genericamente em "erros" no histórico de execuções (FR-7).

---

## Resumo de gaps identificados

1. **Filtro de BLART (estornos/reclassificações)** — regra de negócio (§6.3), caso de teste 10 e parte da questão em aberto #2 (§10) não têm nenhuma representação no PRD/addendum.
2. **Questão em aberto #6 da spec** (nomes de campos J_1BNFDOC/J_1BNFE_ACTIVE) — desaparece sem nota em nenhum lugar do PRD.
3. **Janela de disponibilidade 7h-22h dias úteis** (§8) — ausente das NFRs (§9) e Constraints (§10) do PRD; risco menciona apenas "janelas de manutenção RISE" de forma genérica.
4. **Interação adiantamento (UMSKZ) × regra de extinção automática por valor (FR-4)** — nuance da spec de que adiantamentos "aparecem naturalmente" mesmo fora de escopo não é discutida frente à regra de fechamento automático por valor acumulado.
5. **Tratamento diferenciado de 401/403** (teste 12) vs. 429/400 — FR-2 só especifica retry/redução de lote para os dois últimos; autenticação falha não tem tratamento nomeado.
6. Gaps menores: alternativa de autenticação X.509 (§8) não referenciada no corpo do PRD (só no addendum); tempo de resposta por execução não citado explicitamente como campo de auditoria (§8) apesar de derivável do schema; caso de teste 05 (chave com DV inválido dentro de um lote) sem tratamento explícito distinto do 400 por lote grande demais.

---
title: "Revisão Cética Adversarial — PRD Carga de Pagamentos SAP S/4HANA e Evolução da Conciliação CBS/IBS"
reviewed_document: "prd.md + addendum.md (prd-FB_APU02-2026-07-11)"
reviewer_stance: "cético contratado — presume que o documento tem falhas até prova em contrário"
date: 2026-07-11
---

# Revisão Cética Adversarial

## Postura

Este documento decide fechar automaticamente créditos fiscais (`extinto`) com base em uma soma de valores vinda de um vínculo **ambíguo** (`matchType = FALLBACK`, múltiplos candidatos) do lado SAP, e faz isso invocando o art. 47 da LC 214/2025 como se a questão jurídica estivesse resolvida. É a decisão de maior risco do documento e a que recebe o tratamento mais anêmico em termos de mecanismo técnico e governança. A spec SAP que sustenta tudo isso é um rascunho v0.1 "pendente validação funcional" — e o PRD trata suas inconsistências como já mitigadas. Abaixo, os pontos que não resistem a uma leitura cética.

Fatos de grounding usados nesta revisão (verificados no código atual, não no PRD):
- `status_conciliacao` hoje (`rfb_pagamentos_fornecedores.go`) é derivado **inteiramente** de `rfb_creditos.valor_cbs_nao_extinto` (campo que a própria RFB calcula) cruzado só por igualdade de `chave_doc`/`chave_dfe` — não há hoje nenhuma comparação de valor pago vs. valor da nota, nem checagem de CNPJ do fornecedor.
- `rfb_creditos` é atualizado via `INSERT ... ON CONFLICT DO UPDATE` — sobrescreve o registro anterior, sem histórico. Não existe tabela de auditoria de mudança de status de crédito.
- O padrão RFB citado como modelo a reaproveitar (`services/rfb_processor.go`, `rfb_scheduler.go`) é fire-and-forget via goroutines soltas, sem fila persistente, sem lock/advisory lock, com um TOCTOU conhecido (`COUNT` seguido de `INSERT` sem transação) no scheduler.

---

## Achados

### 1. A regra de extinção mistura duas fontes de verdade diferentes sem dizer qual vence

FR-4 afirma, na mesma seção, duas coisas que não são a mesma computação:
> "o `status_conciliacao` continua sendo derivado do cruzamento com `rfb_creditos` como hoje" — ou seja, `valor_cbs_nao_extinto == 0` (número que a **RFB** calcula, com sua própria base de liquidação).

e logo em seguida:
> "o sistema fecha um crédito automaticamente como `extinto` quando o **valor acumulado de pagamentos** vinculados àquela chave atinge o **valor total da nota** (`invoiceAmount`)" — um número que o **FB_APU02** calcularia somando `payments[]` do SAP.

Essas são duas fontes de verdade distintas para a mesma decisão (a RFB via `valor_cbs_nao_extinto`, e o FB_APU02 via soma de `paidAmount` contra `invoiceAmount`). O PRD nunca resolve o conflito: se a RFB ainda não zerou `valor_cbs_nao_extinto` mas a soma dos pagamentos SAP já bate com `invoiceAmount`, o crédito fecha como `extinto` mesmo assim? Se sim, isso significa que o FB_APU02 vai **contrariar/antecipar** o cálculo oficial da RFB usando um dado de terceiro (SAP, via match ambíguo) — uma decisão jurídica e técnica muito mais agressiva do que "confirmar" sugere, e que não está sequer mencionada como decisão explícita. Esta é a ambiguidade mais grave do documento porque é justamente sobre o mecanismo que decide o que "extinto" quer dizer na prática. Sem essa definição, não há como estimar arquitetura, testes ou risco de glosa — o addendum (§B) também não resolve, apenas descreve tabelas.

### 2. "Bater o valor" não prova identidade — é o ponto cego central do art. 47 aplicado aqui

O art. 47 fala em extinção do débito **daquela operação**. Um match `FALLBACK` (CNPJ + número de documento, janela de ±90 dias, "ambíguo → melhor candidato") não estabelece qual operação foi paga — estabelece só uma coincidência de CNPJ e uma faixa de datas. "O valor acumulado bate com o valor da nota" é uma coincidência aritmética, não uma prova de vínculo causal. Fornecedores com faturamento recorrente (mensalidades, contratos de serviço, aluguel, franquias) frequentemente emitem múltiplas notas de **valor idêntico** — exatamente o cenário em que essa regra tem maior probabilidade de "bater" errado e maior probabilidade de acontecer de forma sistemática (não seria um evento raro, seria um padrão recorrente todo mês para os mesmos fornecedores). O documento nunca examina esse cenário, apesar de ser o caso de uso mais comum de fallback citado no próprio addendum (NFS-e/lançamento direto).

### 3. Não existe verificação de "smell test" mais básica: overpayment

Nenhuma FR verifica se `totalPaidAmount > invoiceAmount` (pagamento acima do valor da nota) — o sinal mais barato e mais óbvio de que um match está errado. Isso seria uma checagem trivial de implementar e o PRD nem cogita. Sem ela, um fallback que acumula pagamentos de duas notas diferentes para o mesmo CNPJ (situação plausível dado o próprio desenho do fallback) passa despercebido, silenciosamente.

### 4. A mitigação de "visibilidade obrigatória" é estruturalmente invisível na jornada normal de uso

FR-8/UJ-2 dizem que a ambiguidade fica sinalizada na tela para quem quiser auditar. Mas UJ-1 — a jornada "normal", descrita como a experiência-padrão de Ana — é: abrir a tela e **filtrar por "pendente"** para ver só as exceções. Um crédito fechado como `extinto` via fallback ambíguo **não aparece no filtro "pendente"** — ele já saiu da lista de exceções por definição, mesmo sendo exatamente o caso que mais precisaria de atenção humana. A "mitigação" descrita nos Riscos (§8) — "visibilidade obrigatória... não bloqueio do fechamento automático" — não é obrigatória coisa nenhuma: nada força ninguém a olhar, e o próprio fluxo de trabalho desenhado no PRD ativamente esconde o item do caminho onde a revisão aconteceria. "Auditar se quiser" (UJ-2, linha do PRD) é a admissão de que a mitigação é opcional.

### 5. SM-C1 mede algo que o PRD explicitamente recusa instrumentar

A contra-métrica SM-C1 ("taxa de créditos fechados via FALLBACK ambíguo não deve crescer sem revisão humana") pressupõe um processo de revisão humana que **não existe** — o workflow de anotação/aprovação está explicitamente fora de escopo (§6.2, com nota "revisitar quando o volume... justificar"). Uma contra-métrica sem processo de resposta associado, sem dono definido, sem cadência e sem limiar de "crescimento aceitável" não é uma salvaguarda — é um número que alguém vai observar mensalmente sem conseguir agir sobre ele a tempo de evitar o dano (o crédito já fechou).

### 6. Overwrite destrutivo em `rfb_creditos` elimina qualquer trilha forense se uma glosa acontecer

O código atual faz `UPDATE` sobre `rfb_creditos` sem histórico algum. Se um crédito for fechado erroneamente como `extinto` via fallback ambíguo e, meses depois, a RFB glosar o crédito, não há como reconstruir no banco "por que o sistema achou que estava extinto naquela data" — nem o snapshot do match, nem o snapshot da soma de pagamentos no momento do fechamento. O addendum adiciona `fallback_ambiguous BOOLEAN` em `pagamentos_fornecedores` (nível de linha de pagamento), mas isso não é o mesmo que um registro auditável de **por que um crédito específico foi considerado extinto** (quais pagamentos, de qual match, somando quanto, comparado a qual `invoiceAmount`, em qual timestamp). Em caso de autuação fiscal, "o sistema simplesmente marcou extinto e sobrescreveu o estado anterior" é uma posição de defesa muito fraca.

### 7. O PRD importa a fragilidade de concorrência do RFB para um componente agora com consequência legal

FR-1 assume reaproveitar o padrão de `services/rfb_processor.go` (goroutine solta, fire-and-forget). Esse padrão já tem uma corrida de dados conhecida no scheduler atual (checagem de "slot usado hoje" via `COUNT` seguido de `INSERT`, sem transação — TOCTOU). Reaproveitar esse padrão para o motor de sincronização SAP significa herdar o mesmo risco de disparo duplicado (ex.: reprocessamento manual + disparo automático simultâneos), mas agora o efeito colateral de uma corrida não é "duas requisições RFB duplicadas" — é **potencialmente contar o mesmo pagamento duas vezes na soma que fecha um crédito como extinto**, ou gravar linhas parcialmente antes de outra execução ler o "valor acumulado". Nenhuma FR trata bloqueio (advisory lock, transação serializável, ou fila) para a soma usada por FR-4. Isso é preocupante justamente porque a decisão em jogo (fechar crédito fiscal) tem consequência jurídica, diferente da idempotência "cosmética" que basta para RFB hoje.

### 8. A spec SAP v0.1 é tratada como praticamente resolvida quando ainda está "pendente validação funcional"

O quadro de Riscos rotula a inconsistência 500 vs. 5000 e o rascunho da spec como impacto "Baixo" ("decisão já mitigada do nosso lado", "nenhuma ação adicional necessária"). Isso confunde **isolar o código** (bom, reduz retrabalho de integração) com **reduzir o risco de dado errado**. A spec ainda não teve validação funcional pelo próprio time FI/Basis — ou seja, ninguém confirmou que a API produz respostas corretas no domínio SAP, muito menos que `matchType`, a janela de fallback (±90 dias, "configurável" — por quem? onde?), ou a semântica de `paymentStatus` vão permanecer estáveis. O FB_APU02 planeja fechar créditos fiscais com base em uma API cujo próprio fornecedor ainda não validou a correção funcional. Rotular isso como risco baixo é otimismo, não engenharia de risco.

### 9. Dependências críticas do lado SAP ficam fora do controle e sem verificação independente do lado FB_APU02

O filtro de `BLART` (que exclui estornos/reclassificações da soma de pagamentos) é delegado inteiramente ao time SAP (Open Question #6) — mas é um input crítico para a correção da regra de extinção do FR-4. O PRD não propõe **nenhuma checagem de sanidade do lado FB_APU02** (ex.: alertar se `totalPaidAmount` para uma chave muda de forma abrupta entre sincronizações, ou se aparecem valores negativos/estornos refletidos na soma). Zero controle técnico próprio sobre um insumo do qual depende uma decisão de compliance — o FB_APU02 fica 100% refém da configuração correta de um sistema que não controla e não pode auditar programaticamente.

### 10. Adiantamentos: "fora de escopo" é uma falsa sensação de segurança

§5/§6.2 dizem que adiantamentos (`UMSKZ`) estão "fora de escopo da API SAP v1" — mas o próprio addendum/FR-4 reconhece que eles "aparecem naturalmente" no array `payments[]` quando vinculados na compensação. Ou seja, mesmo sem nenhuma FR "tratando" adiantamentos, o valor deles **entra na soma que decide extinção**, porque a regra soma tudo que vem em `payments[]` sem filtro. Chamar isso de "fora de escopo" é impreciso: o comportamento indesejado (adiantamento inflando a soma e fechando crédito prematuramente) está dentro do MVP de fato, só não está no MVP de intenção. Isso deveria estar listado como um risco ativo do MVP, não como uma não-meta — e a Open Question #7 já reconhece o problema, mas nada impede o build de prosseguir sem essa resposta.

### 11. Conflito CSV vs. SAP resolvido silenciosamente a favor do SAP, sem alerta

FR-9 assume (`[ASSUMPTION]`) que em conflito de valor entre CSV e SAP para a mesma chave, o registro `sap_api` prevalece na exibição — sem qualquer sinalização de que um conflito ocorreu. Isso significa que uma reconciliação manual cuidadosa feita por um analista pode ser silenciosamente sobreposta por um match SAP automático (possivelmente via fallback ambíguo) sem que ninguém seja avisado que os dois números divergiam. Prevalência automática sem alerta de conflito é uma escolha de conveniência, não uma escolha de segurança de dado.

### 12. A pergunta jurídica central foi resolvida por decisão de produto, sem registro de validação jurídica/fiscal

O rev-tag do documento ("regra de extinção art. 47 confirmada em 2026-07-11") e o texto de Constraints tratam a leitura do art. 47 como settled — mas não há, em nenhum lugar do PRD, referência a um parecer jurídico/tributário que tenha validado essa interpretação (que "valor pago = valor da nota, independente de como o vínculo foi feito" satisfaz a "extinção do débito"). É uma leitura de produto sobre uma norma tributária nova, e o documento se auto-certifica. Dado que o próprio PRD cita o Ato Conjunto RFB/CGIBS nº 1/2025 como uma "janela de calibração" (caráter informativo em 2026) para justificar o risco assumido agora, cabe perguntar: quem revisita essa decisão quando a apuração deixar de ser informativa e passar a ser vinculante? Não há gatilho, dono ou data de revisão associada a essa mudança de regime — o risco fica "esquecido dentro do código" até que alguém se lembre.

### 13. Granularidade do flag de ambiguidade não bate com a granularidade da decisão

O schema proposto (`addendum.md` §C) adiciona `fallback_ambiguous BOOLEAN` por **linha de pagamento**. Mas a decisão de extinção é sobre o **crédito** (acumulado de múltiplos pagamentos, possivelmente uma mistura de match `CHAVE` e `FALLBACK`, alguns ambíguos e outros não). Não há campo ou regra que diga "este crédito foi extinto com contribuição de ao menos um pagamento ambíguo" no nível agregado — a tela teria que recalcular isso em tempo de leitura (FR-8 não detalha como). Se essa agregação não for feita com cuidado, é fácil um crédito fechar como extinto por 90% match direto + 10% fallback ambíguo e a tela não deixar claro que uma fração do fechamento depende do vínculo fraco.

### 14. Rate limit "por consumidor" é ambíguo em um cenário multi-tenant que o PRD nunca menciona

A spec sugere 60 req/min "por consumidor" mas o PRD não define se cada empresa (`company_id`/`BUKRS`) tem sua própria credencial OAuth2 (logo, seu próprio budget de rate limit) ou se várias empresas compartilham um client id (logo, disputam o mesmo budget). Como apurações RFB de múltiplas empresas podem concluir na mesma janela (ex.: fechamento mensal em lote), esse detalhe determina se uma empresa pode "roubar" o rate limit de outra — um problema de justiça multi-tenant que não aparece em nenhum risco, métrica ou FR, apesar do documento ser explícito em outros lugares sobre isolamento multi-tenant (FR-10).

### 15. "N tentativas malsucedidas" (FR-2/FR-7) é um parâmetro definido em nenhum lugar

O critério de quando um lote é "marcado como falho" e quando dispara alerta por e-mail depende de um "N" que nunca é numerado, nem no PRD nem no addendum. Isso parece um detalhe pequeno, mas ele determina diretamente a velocidade de detecção de uma integração degradada (SM-3) e o volume de alertas por e-mail — deixar isso totalmente em aberto para a fase de execução é razoável para *alguns* parâmetros, mas este afeta diretamente a análise de risco operacional que o próprio PRD tenta fazer na tabela de Riscos.

### 16. Rollout faseado não tem kill-switch nem plano de reversão para créditos já fechados incorretamente

§12 define início com 1-2 empresas piloto, mas não descreve o que acontece se o piloto revelar que FR-4 fechou créditos incorretamente. Dado o Achado #6 (sem histórico em `rfb_creditos`), não fica claro se existe qualquer caminho operacional para "reabrir" um crédito e sinalizar que ele precisa de reexame, ou se a única saída é uma correção manual ad hoc no banco. Um recurso que fecha posições fiscais automaticamente deveria ter, no mínimo, um mecanismo declarado de reversão testável antes do primeiro piloto rodar em produção.

---

## Nota final

O documento é tecnicamente bem organizado (glossário, FRs numeradas, suposições indexadas, riscos tabulados) — mas essa organização cria uma aparência de rigor que não se sustenta no ponto mais importante: a regra de FR-4 é apresentada como "decisão confirmada" fundamentada em lei, quando na verdade combina (a) uma leitura jurídica não referendada externamente, (b) um mecanismo técnico ambíguo sobre qual fonte de dado realmente decide a extinção, (c) uma mitigação de visibilidade que o próprio desenho de UX torna opcional e provavelmente inobservada, e (d) zero trilha de auditoria para reconstruir o que aconteceu se a decisão se provar errada. "Fundamentado no art. 47" está fazendo mais trabalho retórico do que a engenharia por trás dela sustenta.

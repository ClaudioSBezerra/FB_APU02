# PRD Quality Review — Carga de Pagamentos SAP S/4HANA e Evolução da Conciliação CBS/IBS

## Overall verdict

Este é um PRD bem acima da média: as duas decisões mais difíceis (tamanho de lote em FR-2, regra de extinção automática em FR-4) foram tomadas explicitamente, com o trade-off nomeado e registrado na tabela de Riscos, não escondido. Glossário, IDs e Índice de Suposições fecham sem drift, e as referências de código brownfield (`RFBPagamentosFornecedoresHandler`, `pagamentos_fornecedores`, `services/rfb_processor.go`, etc.) foram verificadas contra o repositório e são precisas. O que está em risco: dois "N" não definidos em FR-2/FR-7 impedem escrever teste de aceite hoje, e a Open Question #4 (padrão de exposição de rede do RISE) é um bloqueio de conectividade real para todo o MVP que está registrado como pergunta em aberto comum, sem dono nem prazo — para um PRD de stakes "lançamento/produção" isso deveria estar marcado como bloqueador de cronograma, não como mais um item da lista.

## Decision-readiness — strong

FR-2 e FR-4 são o melhor exemplo do que este rubric pede: a decisão do tamanho de lote (500, não 5000) é apresentada como decisão de produto já tomada, com o motivo (valor mais conservador entre os dois presentes na spec) e o que foi abandonado (fechar a inconsistência do lado SAP não bloqueia mais o FB_APU02). A regra de fechamento automático em FR-4 é ainda mais honesta: o PRD nomeia o risco de aceitar um match `FALLBACK` ambíguo como "extinto" (§8, linha 3 da tabela — "risco de glosa fiscal não fica visível se ninguém auditar a sinalização de ambiguidade") e decide mesmo assim, com justificativa jurídica (art. 47 é sobre valor liquidado, não sobre método de identificação) e mitigação por visibilidade em vez de bloqueio. Isso é uma trade-off nomeada de verdade, não uma "balanceamento" retórico.

O `[NOTE FOR PM: revisitar quando o volume de exceções manuais justificar um fluxo formal]` em §6.2 está numa tensão real (falta de workflow de aprovação), não num checkpoint seguro.

### Findings
- **medium** NFR de disponibilidade de dados não é um compromisso, é uma esperança (§9 Desempenho) — "alvo de disponibilidade dos dados na tela em até 24h após a apuração concluir (**a confirmar com o time fiscal**)". Isso deveria ser um `[ASSUMPTION]` ou uma Open Question explícita, não um alvo de NFR entre parênteses. *Fix:* mover para §13 como pergunta em aberto ou transformar em `[ASSUMPTION: 24h é o alvo até confirmação do time fiscal]` indexado em §14.
- **medium** Open Question #4 (padrão de exposição de rede do ambiente RISE — Cloud Connector/API Management/WAF) é tratada como mais uma das 7 perguntas em aberto, mas a própria tabela de Riscos (§8) descreve o impacto como "Bloqueio de conectividade de rede entre FB_APU02 e SAP" — ou seja, é potencialmente bloqueador para *todo* o desenvolvimento (nenhuma FR de sincronização funciona sem essa definição), não apenas um detalhe a resolver em paralelo. O PRD não distingue essa pergunta das demais (ex. #1 NFS-e, que já está fora do MVP) em termos de urgência/dono/prazo. *Fix:* destacar #4 como bloqueador crítico com dono nomeado (Basis/Segurança) e prazo antes do kickoff de implementação, separando-a das perguntas que não bloqueiam o início do desenvolvimento.

## Substance over theater — strong

Sem personas decorativas: apenas Ana (analista fiscal) carrega UJ-1 e UJ-2, e "administrador" em UJ-3 é um papel operacional, não uma persona extra criada para preencher espaço. As 4 JTBDs (§2.1) mapeiam para decisões reais — inclusive a JTBD do "gestor fiscal" (confiança em vínculo ambíguo) que se traduz diretamente em FR-8 e na contra-métrica SM-C1, em vez de ficar solta.

A Visão (§1) é específica ao ponto de não poder ser reaproveitada em outro PRD: cita art. 27 da LC 214/2025, a planilha exportada manualmente do SAP, a rota exata `/importacoes/pagamentos-fornecedores`, a tabela `rfb_creditos`. Os NFRs (§9) trazem números da spec real (lote de 500, 160s p95) em vez de adjetivos genéricos — não há "o sistema deve ser escalável/seguro" sem contexto.

### Findings
- **low** A JTBD do "gestor fiscal" (§2.1, item 4) não tem uma UJ própria — é atendida apenas indiretamente via FR-8/SM-C1. Não chega a ser teatro (dirige decisões reais), mas fica implícito que esse persona não terá uma tela ou fluxo dedicado a ele; vale um `[NON-GOAL for MVP]` explícito dizendo que a "confiança do gestor" é resolvida por transparência de dado, não por um relatório dedicado a esse papel.

## Strategic coherence — strong

A tese é clara e é carregada por toda a estrutura: LC 214/2025 art. 27 condiciona crédito a liquidação financeira → hoje isso é validado manualmente via CSV → a automação elimina o elo manual e concentra atenção humana na exceção. As features 4.1→4.4 seguem essa tese em ordem lógica (motor de sincronização → habilitação/credenciais → observabilidade → convivência sem regressão), não uma lista de "o que era fácil primeiro".

As métricas de sucesso validam a tese, não atividade: SM-1 mede redução de crédito `pendente` (o problema real), SM-2 mede redução de upload manual (o elo eliminado) — nenhuma é vaidosa (não há DAU/MAU aqui). As contra-métricas são incomumente bem desenhadas: SM-C1 (fechamentos automáticos via `FALLBACK` ambíguo não devem crescer sem revisão) e SM-C2 (volume de chamadas não deve crescer artificialmente) atacam exatamente os dois jeitos de "trapacear" SM-1 — reduzir `pendente` aceitando vínculos errados, ou inflar SM-3.

Sem achados de severidade relevante nesta dimensão.

## Done-ness clarity — adequate

A maioria das FRs tem consequências testáveis de verdade e incomumente precisas para um PRD (nomes de campo exatos, HTTP codes, fórmulas de valor). Mas dois "N" ficam indefinidos em pontos que bloqueiam a escrita de testes de aceite hoje, e o NFR de performance mistura bound real com alvo não confirmado.

### Findings
- **high** FR-2 ("Após **N** tentativas malsucedidas (429/500), o lote é marcado como falho") e FR-7 ("Falhas recorrentes (**N** execuções seguidas com erro) disparam um alerta") deixam `N` como variável não definida em ambos os casos — são exatamente o tipo de lacuna que a fase de stories vai precisar resolver com uma suposição própria, sem que o PM tenha decidido. *Fix:* fixar um valor (`[ASSUMPTION: N=3 tentativas / N=3 execuções consecutivas]`) e indexar em §14, ou registrar como Open Question explícita — hoje nenhuma das duas coisas acontece.
- **medium** FR-4, nota final: a soma usada pela regra de extinção automática ("valor acumulado de pagamentos... atinge o valor total da nota") não exclui adiantamentos (`UMSKZ`), e o próprio PRD admite que não está confirmado se isso é correto à luz do art. 47 (também Open Question #7). Isso significa que a consequência testável mais crítica de FR-4 — quando um crédito fecha como `extinto` — não tem um "done" estável enquanto essa pergunta não for respondida; qualquer teste escrito hoje pode precisar ser refeito. *Fix:* nomear um comportamento interino explícito (ex.: "até resolução da OQ#7, créditos cujo `payments[]` contenha ao menos um item com razão especial de adiantamento são sinalizados como `extinto (revisar adiantamento)` em vez de `extinto` puro") em vez de deixar a regra geral testável mascarar essa exceção não resolvida.
- **low** §9 Desempenho mistura um bound real (500 chaves / 160s p95, vindo da spec SAP) com um alvo não confirmado ("até 24h... a confirmar com o time fiscal") na mesma frase — ver também finding em Decision-readiness.

## Scope honesty — strong

O Não-Metas (§5) e o Fora de Escopo (§6.2) fazem trabalho de verdade, não são um bullet list de template: cada exclusão vem com a razão (NFS-e bloqueado por chave de 50 dígitos não confirmada pelo lado SAP; adiantamentos fora do escopo da própria API SAP v1; tabela de-para de `settlementMode` é responsabilidade do time fiscal do lado SAP, spec seção 6.4). O `[NOTE FOR PM]` em §6.2 está numa decisão de fato adiada (workflow de aprovação), não decorativo.

As 6 suposições inline (`[ASSUMPTION]`) fecham 100% com o Índice de Suposições em §14 — nenhuma órfã, nenhuma faltando (ver Notas Mecânicas).

A densidade de itens em aberto (7 Open Questions + 6 Assumptions + 2 NOTE FOR PM = 15) é alta para um PRD de stakes "lançamento/produção", mas a maior parte dessa densidade reflete incerteza real e externa (spec SAP v0.1 ainda em elaboração, decisões que pertencem ao time FI/Basis) — não preguiça do PM. O ponto fraco não é o volume, é a falta de hierarquia entre eles (ver finding de Decision-readiness sobre OQ#4): perguntas que bloqueiam todo o desenvolvimento (rede/conectividade) estão listadas lado a lado com perguntas que não bloqueiam nada no curto prazo (NFS-e, já fora do MVP).

### Findings
- **high** (mesmo achado da seção Decision-readiness, repetido aqui por afetar também esta dimensão) A falta de priorização/dono explícito nas Open Questions esconde que a OQ#4 é um bloqueador de cronograma, não um item de acompanhamento — ver finding correspondente acima.

## Downstream usability — strong

Glossário (§3) cobre os 14 termos-chave, incluindo a distinção deliberada entre `paymentStatus` (SAP) e `status_conciliacao` (interno) — exatamente o tipo de ambiguidade que costuma causar confusão entre PRD e implementação, e o PRD a resolve preemptivamente. IDs contíguos e sem duplicidade: UJ-1 a UJ-3, FR-1 a FR-10, SM-1 a SM-3 + SM-C1/C2. Toda UJ é realizada por ao menos uma feature (UJ-1 → FR-1 e §4.3; UJ-2 → §4.3/FR-8; UJ-3 → §4.2/FR-5,6), sem UJ flutuante.

O addendum técnico separa corretamente o conteúdo destinado à próxima etapa (arquitetura/stories) do corpo do PRD, e as referências de código nele (handlers, migrations, páginas) foram conferidas contra o repositório e todas existem exatamente como citadas: `backend/handlers/rfb_pagamentos_fornecedores.go`, `backend/handlers/pagamentos_fornecedores.go`, `backend/migrations/110_pagamentos_fornecedores.sql` / `111_pagamentos_imports.sql`, `frontend/src/pages/RFBPagamentosFornecedores.tsx`, `RFBCredentials.tsx`, `CGIBSCredentials.tsx`, `services/rfb_processor.go`, `services/email.go`. A constraint atual de deduplicação citada em FR-3 (`UNIQUE (company_id, chave_doc, data_pagamento, valor_pagamento)`) bate exatamente com a migration 110 real.

### Findings
- **low** Apenas FR-1 e as descrições de feature (§4.1–§4.3) carregam a tag "Realiza UJ-X"; FR-2, FR-3, FR-5, FR-6, FR-9, FR-10 não têm essa tag individual (a rastreabilidade existe no nível de feature, mas não em cada FR). Não quebra a navegação, mas fica menos explícito para quem for extrair FRs isoladamente.

## Shape fit — strong

O PRD acerta o formato para as três dimensões simultâneas do produto: (1) ferramenta interna de operador único/administrador — não infla com personas ou UJs além do necessário (3 UJs para 2 papéis reais, sem excesso); (2) atualização regulatória — rastreabilidade de constraint é tratada como inegociável, com §10 amarrando cada regra de negócio ao artigo de lei correspondente (FR-4 ↔ art. 47; disparo de FR-1/liquidação ↔ art. 27); (3) chain-top (alimenta arquitetura/stories) — o addendum técnico separado, com esboço de migration e de arquitetura, antecipa exatamente o que a próxima fase vai precisar sem inflar a narrativa do PRD principal.

Brownfield: todas as referências de código verificadas batem com o repositório (ver Downstream usability). Nenhuma UJ nova é confundida com fluxo existente — UJ-1/UJ-2/UJ-3 são claramente descritas como o comportamento *novo*, com o CSV manual tratado à parte como fallback existente preservado (§4.4).

Sem achados de severidade relevante nesta dimensão.

## Mechanical notes

- **Glossário:** sem drift perceptível — `status_conciliacao`, `origem`, `matchType`, `paymentStatus` usados de forma idêntica em UJs, FRs e Glossário.
- **Continuidade de IDs:** UJ-1..3, FR-1..10, SM-1..3 + SM-C1/C2 — sem lacunas, sem duplicidade, sem referência cruzada quebrada.
- **Roundtrip do Índice de Suposições:** 6/6 `[ASSUMPTION]` inline (FR-1, FR-2, FR-5, FR-6, FR-9, e a nota de FR-3) aparecem em §14; nenhuma entrada do índice é órfã. Fecha limpo.
- **Nomeação de protagonista de UJ:** Ana nomeada em UJ-1/UJ-2; "administrador" em UJ-3 é papel, não nome — aceitável dado que é uma capability de onboarding administrativo, não uma jornada centrada em usuário final.
- **Seções exigidas para o stakes acordado:** presentes todas as esperadas para "lançamento/produção" + compliance — Riscos, NFRs, Constraints/Compliance, Rollout, Perguntas em Aberto, Índice de Suposições.

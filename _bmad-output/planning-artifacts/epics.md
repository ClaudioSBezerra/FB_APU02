---
stepsCompleted: [1, 2, 3, 4]
inputDocuments:
  - "_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md"
  - "_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/addendum.md"
---

# FB_APU02 — Carga de Pagamentos SAP S/4HANA e Evolução da Conciliação CBS/IBS - Epic Breakdown

## Overview

Este documento decompõe o PRD "Carga de Pagamentos SAP S/4HANA e Evolução da Conciliação CBS/IBS" (`prd.md` + `addendum.md`, status final) em epics e stories implementáveis. Não há documento de Architecture.md formal nem UX Design — o `addendum.md` do próprio PRD serve como input técnico (esboço de arquitetura Go, migration, inversão de query), decisão confirmada com o usuário na ativação deste workflow.

## Requirements Inventory

### Functional Requirements

FR1: O sistema deve, ao concluir o processamento de uma apuração RFB, extrair as chaves de DF-e (NF-e/CT-e) dos créditos recém-gravados e disparar automaticamente a sincronização de pagamentos com o SAP.
FR2: O sistema deve consultar `SearchPayments` em lotes de até 500 chaves, respeitando rate limit configurável, com retry/backoff para 429/500, redução automática de lote para 400, e alerta imediato (sem retry) para 401/403.
FR3: O sistema deve gravar cada pagamento retornado pelo SAP como uma linha em `pagamentos_fornecedores`, com `origem = 'sap_api'`, usando `(company_id, chave_doc, num_doc_pagamento, bukrs)` como nova chave de deduplicação.
FR4: O sistema deve tratar os dados de pagamento do SAP (`paymentStatus`, `matchType`) exclusivamente como indicador auxiliar — a decisão de que um crédito está `extinto` continua sendo determinada 100% por `rfb_creditos.valor_cbs_nao_extinto`, nunca pelo SAP.
FR5: Um administrador deve poder cadastrar, editar e testar credenciais OAuth2 (client id/secret) de acesso à API SAP por empresa, com o segredo criptografado.
FR6: Um administrador deve poder associar um `company_id` do FB_APU02 a um ou mais códigos de empresa SAP (BUKRS), com falha isolada por BUKRS (não afeta os demais).
FR7: O sistema deve registrar e expor o histórico de execuções de sincronização com o SAP (empresa, BUKRS, data/hora, quantidades por status, erros), com alerta por e-mail em falhas recorrentes (N=3) ou de autenticação (imediato).
FR8: A tela de conciliação deve exibir a origem do pagamento (`sap_api` vs `csv`) e, quando não houver pagamento localizado ou o match for ambíguo, o motivo retornado pelo SAP, com mensagens distintas para `EM_ABERTO` vs `NAO_LOCALIZADO`.
FR9: O sistema deve permitir que uma mesma chave de DF-e tenha pagamentos de CSV e de SAP ao longo do tempo sem duplicar valores, sinalizando visualmente quando `total_pago` excede `valor_nota`.
FR10: Nenhuma chamada ou resposta da integração SAP pode misturar dados de empresas fora do escopo autorizado da empresa que originou a consulta.
FR11: A tela de conciliação deve exibir todo crédito retornado pela RFB, mesmo sem nenhum pagamento localizado (novo status `aguardando_pagamento`), exigindo inverter a direção da consulta de conciliação (de `pagamentos_fornecedores` para `rfb_creditos`).
FR12: A tela de conciliação deve oferecer um filtro/contador dedicado para créditos cujo indicador de pagamento veio de um match `FALLBACK` ambíguo, como parte do fluxo normal de revisão (não uma auditoria opcional).

### NonFunctional Requirements

NFR1 (Desempenho): a sincronização de uma apuração típica deve completar dentro do SLA da API SAP (lote de 500 chaves, orçamento de 160s p95) mais overhead de rede/retry do FB_APU02; alvo de disponibilidade dos dados na tela em até 24h após a apuração concluir.
NFR2 (Disponibilidade): a API SAP opera em horário comercial estendido (7h-22h, dias úteis) além de janelas de manutenção do RISE; o disparo automático deve tolerar esse horário, aguardando o próximo horário disponível em vez de falhar repetidamente.
NFR3 (Segurança): credenciais SAP criptografadas em repouso (`ENCRYPTION_KEY`), OAuth2 client credentials, TLS 1.2+, acesso à tela de credenciais restrito a admin.
NFR4 (Confiabilidade): falha na sincronização SAP não pode impactar o fluxo de apuração RFB nem o fluxo de import CSV — processos independentes e isolados.
NFR5 (Observabilidade): toda execução de sincronização é logada com correlationId, seguindo o padrão de log já usado no projeto; histórico consultável.
NFR6 (Multi-tenancy): nenhuma consulta ou gravação cruza `company_id`, seguindo o mesmo modelo de isolamento explícito já usado no restante do backend.

### Additional Requirements

*(extraídas do addendum.md, que serve como input de arquitetura para esta iniciativa — ver Overview)*

- Novo serviço `services/sap_payments.go` — cliente HTTP com cache de token OAuth2 client credentials, análogo ao cache de token RFB existente.
- Novo serviço `services/sap_payments_processor.go` — orquestra o lote: recebe chaves de `rfb_creditos`, monta requisições `SearchPayments`, trata retry/backoff, grava resultado.
- Disparo assíncrono (goroutine) no fim do processamento que hoje grava `rfb_creditos` (`services/rfb_processor.go`), sem bloquear a resposta ao webhook RFB.
- Nova tabela `sap_credentials` (padrão idêntico a `rfb_credentials`/`cgibs_credentials`): client_id, client_secret criptografado, base_url, bukrs_list.
- Nova tabela `sap_sync_runs` (padrão idêntico a `erp_bridge_runs`), com granularidade por `bukrs`: company_id, bukrs, iniciado_em, concluido_em, status, chaves_enviadas, chaves_pago_total, chaves_pago_parcial, chaves_em_aberto, chaves_nao_localizado, erro_detalhe.
- Nova migration (112+) em `pagamentos_fornecedores`: colunas `origem`, `payment_status`, `match_type`, `bukrs`, `settlement_mode`, `fallback_ambiguous`.
- Troca da constraint de deduplicação de `(company_id, chave_doc, data_pagamento, valor_pagamento)` para `(company_id, chave_doc, num_doc_pagamento, bukrs)` — exige migration de backfill cuidadosa para linhas de CSV legado sem `num_doc_pagamento`/`bukrs` (migrations existentes não podem ser alteradas, apenas adicionadas).
- Inversão da consulta de conciliação em `backend/handlers/rfb_pagamentos_fornecedores.go`: de `FROM pagamentos_fornecedores LEFT JOIN rfb_creditos` para `FROM rfb_creditos LEFT JOIN pagamentos_agg`, com tratamento separado (UNION ou segunda query) para o caso `sem_dados` (pagamento sem crédito correspondente).
- Reaproveitamento de `services/email.go` para alertas de falha recorrente/autenticação.
- Integração externa: API SAP OData V4 (RAP) no S/4HANA RISE, ainda em spec v0.1 (pendente validação FI/Basis) — padrão de exposição externa (Cloud Connector/API Management/WAF) ainda não definido pelo time Basis/Segurança, bloqueador de cronograma real (ver PRD §8, Open Question #4).

### UX Design Requirements

Não aplicável — não há documento de UX Design para esta iniciativa. Trata-se de evolução de uma tela já existente (`/rfb/pagamentos-fornecedores`), sem mudanças de padrão visual: novos indicadores/filtros seguem os componentes shadcn/ui já usados na página (`Card`, badges de status, filtros existentes).

### FR Coverage Map

FR1: Epic 2 - Disparo automático de sincronização pós-apuração RFB
FR2: Epic 2 - Consulta em lote (500 chaves) com retry/backoff e tratamento de erros
FR3: Epic 2 - Persistência de pagamentos com nova chave de deduplicação
FR4: Epic 3 - Dados do SAP como indicador auxiliar (RFB decide extinção)
FR5: Epic 1 - Cadastro de credenciais SAP por empresa
FR6: Epic 1 - Mapeamento company_id ↔ BUKRS
FR7: Epic 2 - Histórico de execuções de sincronização e alertas
FR8: Epic 3 - Exibição de origem e motivo na tela de conciliação
FR9: Epic 2 - Coexistência de origens (CSV/SAP) sem duplicidade
FR10: Epic 2 - Isolamento multi-tenant nas chamadas ao SAP
FR11: Epic 3 - Créditos sem pagamento localizado aparecem na tela
FR12: Epic 3 - Revisão dedicada de matches ambíguos

## Epic List

### Epic 1: Credenciais e Habilitação SAP por Empresa
Um administrador consegue cadastrar, testar e habilitar a integração SAP para uma empresa.
**FRs covered:** FR5, FR6

### Epic 2: Sincronização Automática e Confiável de Pagamentos SAP
Uma empresa habilitada (Epic 1) passa a ter pagamentos carregados automaticamente do SAP a cada apuração RFB, com histórico auditável e sem conflito com o fluxo de CSV manual existente.
**FRs covered:** FR1, FR2, FR3, FR7, FR9, FR10

### Epic 3: Conciliação Completa e Transparente
Ana vê todo crédito da RFB na tela (mesmo sem pagamento localizado ainda) e tem um espaço dedicado para revisar evidências fracas, sem que o sistema decida extinção por conta própria.
**FRs covered:** FR4, FR8, FR11, FR12

## Epic 1: Credenciais e Habilitação SAP por Empresa

Um administrador consegue cadastrar, testar e habilitar a integração SAP para uma empresa.

### Story 1.1: Cadastro de Credenciais e Mapeamento de BUKRS por Empresa

As a administrador,
I want cadastrar credenciais OAuth2 do SAP e o(s) código(s) de empresa (BUKRS) para uma empresa,
So that a sincronização saiba com qual empresa SAP se comunicar.

**Acceptance Criteria:**

**Given** sou admin autenticado
**When** acesso a tela de credenciais SAP de uma empresa
**Then** posso inserir client_id, client_secret e um ou mais códigos BUKRS

**Given** salvo as credenciais
**When** persistidas
**Then** o client_secret é criptografado com `ENCRYPTION_KEY` e nunca retornado em texto plano por nenhuma rota de leitura

**Given** não sou admin
**When** tento acessar a tela/API
**Then** recebo 403

**Given** a migration correspondente roda
**When** aplicada
**Then** a tabela `sap_credentials` é criada (client_id, client_secret criptografado, base_url, bukrs_list, company_id)

**Given** uma empresa sem nenhum BUKRS configurado
**When** qualquer sincronização futura rodar para ela
**Then** nenhuma chamada ao SAP é feita (100% fluxo CSV)

### Story 1.2: Teste de Conexão com Credenciais SAP

As a administrador,
I want testar a conexão com as credenciais salvas,
So that eu confirme que a integração está corretamente configurada antes de depender dela.

**Acceptance Criteria:**

**Given** credenciais válidas salvas
**When** clico em "testar conexão"
**Then** o sistema executa uma chamada OAuth2 mínima ao SAP e reporta sucesso/falha

**Given** credenciais inválidas
**When** testo a conexão
**Then** recebo mensagem clara de falha

**And** o teste de conexão nunca persiste dado de negócio (não grava em `pagamentos_fornecedores`)

## Epic 2: Sincronização Automática e Confiável de Pagamentos SAP

Uma empresa habilitada (Epic 1) passa a ter pagamentos carregados automaticamente do SAP a cada apuração RFB, com histórico auditável e sem conflito com o fluxo de CSV manual existente.

### Story 2.1: Disparo Automático de Sincronização Pós-Apuração

As a sistema,
I want disparar automaticamente a sincronização de pagamentos SAP quando uma apuração RFB concluir,
So that Ana nunca precise solicitar isso manualmente.

**Acceptance Criteria:**

**Given** uma apuração RFB muda para `completed` e grava novos `rfb_creditos`
**When** o processamento termina
**Then** o sistema extrai as chaves NF-e/CT-e e dispara a sincronização SAP de forma assíncrona (goroutine), sem bloquear a resposta do webhook RFB

**Given** uma apuração concluída sem nenhum crédito novo
**When** isso ocorre
**Then** nenhuma chamada ao SAP é disparada

**Given** a sincronização SAP falha
**When** isso ocorre
**Then** a apuração RFB em si não é afetada — a falha fica isolada

**Given** a migration correspondente roda
**When** aplicada
**Then** a tabela `sap_sync_runs` é criada com os campos mínimos necessários para rastrear execuções (company_id, bukrs, chaves do lote, status, iniciado_em, concluido_em) — campos adicionais de contagem detalhada por status são adicionados na Story 2.4

**Given** o webhook RFB é reentregue para a mesma apuração
**When** o disparo seria repetido
**Then** o sistema verifica em `sap_sync_runs` se já existe execução em andamento/concluída para aquele conjunto de chaves e não dispara de novo

**And** apenas os BUKRS configurados (Epic 1) para aquele `company_id` são consultados — isolamento multi-tenant (FR10)

### Story 2.2: Consulta em Lote com Rate Limit e Tratamento de Erros

As a sistema,
I want consultar `SearchPayments` em lotes bem dimensionados com lógica de retry,
So that a sincronização seja confiável dentro dos limites do SAP.

**Acceptance Criteria:**

**Given** um conjunto de chaves a sincronizar
**When** monta os lotes
**Then** usa no máximo 500 chaves por requisição

**Given** o rate limit configurado
**When** envia requisições
**Then** respeita o limite (sugestão inicial: 60 req/min)

**Given** resposta HTTP 429
**When** recebida
**Then** retry com backoff exponencial

**Given** resposta HTTP 400 por lote grande demais
**When** recebida
**Then** reduz o tamanho do lote automaticamente e tenta novamente

**Given** uma chave malformada dentro de um lote
**When** detectada
**Then** é filtrada/reportada antes do envio, sem invalidar o lote inteiro

**Given** resposta HTTP 401/403
**When** recebida
**Then** não há retry — o lote é marcado em `sap_sync_runs` como falha de credencial, distinta de falha transitória (429/500), para que a Story 2.4 possa alertar imediatamente sobre ela

**Given** 3 tentativas malsucedidas (429/500) para um lote
**When** atingidas
**Then** o lote é marcado como falho no log de execução (`sap_sync_runs`, criada na Story 2.1) sem interromper os demais lotes

### Story 2.3: Persistência de Pagamentos com Deduplicação Robusta

As a sistema,
I want persistir cada pagamento retornado pelo SAP com uma chave de deduplicação robusta,
So that reprocessamentos nunca criem duplicidade.

**Acceptance Criteria:**

**Given** uma resposta de `SearchPayments`
**When** processa `payments[]`
**Then** cada item vira uma linha própria em `pagamentos_fornecedores` com `origem = 'sap_api'`

**Given** a nova chave de dedup `(company_id, chave_doc, num_doc_pagamento, bukrs)`
**When** uma chave é reprocessada
**Then** nenhuma linha duplicada é criada (upsert idempotente)

**Given** a migration de evolução do schema
**When** aplicada
**Then** adiciona colunas `origem`, `payment_status`, `match_type`, `bukrs`, `settlement_mode`, `fallback_ambiguous`, com troca de constraint e backfill cuidadoso para linhas de CSV legado sem `num_doc_pagamento`/`bukrs`

**Given** um pagamento de um BUKRS que não pertence ao escopo da empresa que originou a consulta
**When** persistido
**Then** nunca é associado a outro `company_id` (equivalente ao caso de teste 11 da spec SAP)

### Story 2.4: Histórico de Execuções e Alertas de Falha

As a administrador ou analista,
I want ver o histórico de execuções de sincronização e receber alertas em falhas repetidas,
So that eu confie que a automação está saudável.

**Acceptance Criteria:**

**Given** a migration desta story
**When** aplicada
**Then** estende `sap_sync_runs` (criada na Story 2.1) com contagens detalhadas por `paymentStatus` e tempo total de execução, e expõe o histórico via API e tela

**Given** 3 execuções seguidas com erro transitório (Story 2.2)
**When** a 3ª ocorre
**Then** um alerta por e-mail dispara via `services/email.go`

**Given** uma execução marcada como falha de credencial (401/403, Story 2.2)
**When** isso ocorre
**Then** dispara alerta imediato distinto, sem esperar as 3 falhas

**Given** falha em 1 de N BUKRS configurados
**When** visualizado o histórico
**Then** o resultado por BUKRS é visível isoladamente (não apenas agregado por empresa)

### Story 2.5: Coexistência de Pagamentos CSV e SAP sem Duplicidade

As a Ana (analista fiscal),
I want que o sistema combine com segurança pagamentos de CSV e do SAP para a mesma nota,
So that a transição para sincronização automática nunca crie conflitos de dado.

**Acceptance Criteria:**

**Given** uma chave com pagamento CSV e depois um pagamento SAP da mesma liquidação (mesmo `num_doc_pagamento`/`clearingDocument`)
**When** ambos existem
**Then** a chave de dedup evita duplicidade

**Given** conflito de valor para a mesma chave de dedup entre CSV e SAP
**When** exibido
**Then** o registro `sap_api` prevalece na tela, mantendo o registro `csv` para auditoria

**Given** `total_pago` de uma chave excede `valor_nota`
**When** isso ocorre
**Then** uma sinalização visual aparece (indício de duplicidade ou match errado)

## Epic 3: Conciliação Completa e Transparente

Ana vê todo crédito da RFB na tela (mesmo sem pagamento localizado ainda) e tem um espaço dedicado para revisar evidências fracas, sem que o sistema decida extinção por conta própria.

### Story 3.1: Créditos sem Pagamento Localizado Aparecem na Tela

As a Ana (analista fiscal),
I want ver todo crédito da RFB na tela de conciliação, mesmo quando nenhum pagamento foi localizado ainda,
So that nada fique escondido da minha visão.

**Acceptance Criteria:**

**Given** um crédito RFB com `valor_cbs_nao_extinto > 0` e nenhum pagamento (CSV nem SAP) localizado
**When** Ana abre a tela
**Then** ele aparece com status `aguardando_pagamento`

**Given** a inversão da query de conciliação (de `rfb_creditos` com LEFT JOIN para pagamentos)
**When** os cards de sumário renderizam
**Then** "CBS Extinto" reflete todos os créditos que a RFB já considera extintos, não só o subconjunto com pagamento registrado

**Given** um pagamento sem crédito `rfb_creditos` correspondente
**When** exibido
**Then** continua aparecendo como `sem_dados` (comportamento atual preservado)

### Story 3.2: Indicador de Pagamento como Evidência Auxiliar

As a Ana (analista fiscal),
I want ver um indicador de pagamento (localizado/não localizado, origem, motivo) em cada crédito sem que ele jamais altere a decisão de extinção da RFB,
So that eu sempre saiba que o status oficial veio da RFB.

**Acceptance Criteria:**

**Given** um crédito com dado `PAGO_TOTAL`/`PAGO_PARCIAL` do SAP
**When** exibido
**Then** mostra indicador "pagamento localizado" sem alterar `status_conciliacao`

**Given** `status_conciliacao` sempre derivado de `rfb_creditos.valor_cbs_nao_extinto`
**When** o SAP discorda (soma de pagamentos cobre a nota mas a RFB não zerou o saldo)
**Then** a tela mostra "pagamento localizado, aguardando confirmação da RFB" como mensagem distinta — sem reclassificar o crédito

**Given** um item `NAO_LOCALIZADO`
**When** exibido
**Then** mostra mensagem distinta de `EM_ABERTO` ("não localizamos o documento" vs. "localizamos, mas ainda não foi pago"), com data da última tentativa

**Given** `origem = sap_api` vs `csv`
**When** exibido
**Then** a origem é visualmente distinguível

### Story 3.3: Revisão Dedicada de Matches Ambíguos

As a Ana (analista fiscal),
I want um filtro/contador dedicado para créditos cuja evidência de pagamento veio de um match ambíguo,
So that revisar evidência fraca seja parte da minha rotina, não algo que eu precise lembrar de fazer.

**Acceptance Criteria:**

**Given** o indicador de pagamento de um crédito tem `matchType = FALLBACK` com ambiguidade sinalizada pelo SAP
**When** Ana abre o filtro dedicado
**Then** ele aparece ali independentemente do `status_conciliacao` atual (inclusive créditos já `extinto`)

**Given** Ana está na tela principal de conciliação
**Then** um contador/badge visível mostra quantos itens aguardam revisão nesse filtro — reforçando que faz parte do fluxo normal, não uma auditoria escondida

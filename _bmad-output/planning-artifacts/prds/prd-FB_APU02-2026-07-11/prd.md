---
title: "Carga de Pagamentos SAP S/4HANA e Evolução da Conciliação CBS/IBS"
status: final
created: 2026-07-11
updated: 2026-07-11
---

# PRD: Carga de Pagamentos SAP S/4HANA e Evolução da Conciliação CBS/IBS
*Working title — confirmar.*

## 0. Documento — Propósito

Este PRD é dirigido ao time de produto/dev do FB_APU02, ao time SAP (FI/Basis) que desenvolverá a API consumida, e a quem planejar a arquitetura e as stories desta iniciativa. Vocabulário âncora no [Glossário](#3-glossário) — features agrupadas em §4 com FRs numeradas globalmente (FR-1…FR-N), suposições marcadas inline com `[ASSUMPTION]` e indexadas em §14.

Este PRD **não substitui** a especificação técnica SAP "API de Consulta de Pagamentos por Chave de DF-e" v0.1 (09/07/2026, em elaboração pelo time FI/Basis) — ele consome essa API como dependência externa. O detalhamento técnico dos dois lados (contrato de campos SAP, esboço de arquitetura Go do lado FB_APU02) vive em `addendum.md`, não duplicado aqui.

## 1. Visão

A Lei Complementar 214/2025 condiciona a apropriação dos créditos de CBS e IBS à liquidação financeira das operações (art. 27). Na apuração assistida, a Receita Federal pré-calcula os créditos a partir dos documentos fiscais eletrônicos, mas cabe ao contribuinte comprovar que o pagamento efetivamente ocorreu — hoje, no FB_APU02, essa comprovação depende de um analista fiscal exportar uma planilha do SAP e subir manualmente via CSV em `/importacoes/pagamentos-fornecedores` todo mês.

Esta iniciativa elimina esse elo manual: uma vez que uma apuração RFB é concluída e os créditos chegam em `rfb_creditos`, o FB_APU02 consulta automaticamente a nova API de pagamentos do SAP S/4HANA (RISE) por chave de DF-e e grava o resultado direto na tela de conciliação já existente em `/rfb/pagamentos-fornecedores`. O analista deixa de fazer upload manual rotineiro e passa a atuar apenas sobre as exceções — créditos que a busca automática não conseguiu vincular a um pagamento.

O resultado é uma conciliação CBS/IBS mais rápida, com menos risco de erro humano de digitação/upload, e melhor postura de conformidade com o art. 27 antes que a apuração assistida deixe de ser meramente informativa.

**Princípio de design não negociável:** o FB_APU02 nunca decide, por conta própria, se um crédito está extinto. Essa determinação é e continua sendo exclusivamente da RFB (`rfb_creditos.valor_cbs_nao_extinto`). O que este PRD entrega é um indicador automático de pagamento (via SAP) que acelera e aumenta a cobertura da evidência disponível para Ana — nunca um substituto ou uma antecipação da decisão da RFB.

## 2. Usuário-Alvo

### 2.1 Jobs To Be Done
- Como analista fiscal do grupo Ferreira Costa, preciso saber, para cada crédito CBS/IBS retornado pela RFB, se o fornecedor já foi pago — sem precisar exportar e subir planilhas do SAP manualmente todo mês.
- Como analista fiscal, preciso identificar rapidamente quais créditos ficaram sem conciliação (pagamento não localizado) para investigar antes do prazo de apuração.
- Como administrador do FB_APU02, preciso cadastrar e testar credenciais de acesso à API SAP por empresa, sem expor segredos em texto plano.
- Como gestor fiscal, preciso confiar que a automação **nunca decide sozinha** que um crédito está extinto — essa decisão é sempre da RFB; a automação só acelera a evidência disponível para o analista.

### 2.2 Não-Usuários (v1)
- Usuários do SAP que executam o pagamento em si — este PRD cobre apenas o lado consumidor (FB_APU02), não o processo de pagamento no SAP.
- Empresas do grupo que ainda não têm a API SAP disponível/configurada — continuam 100% no fluxo de CSV manual (ver §5 Não-Metas).
- Fornecedores pessoa física (CPF) — a API SAP v1 trata apenas fornecedores PJ (CNPJ); ver Constraints §Compliance.

### 2.3 Jornadas-Chave do Usuário

- **UJ-1. Ana confere a conciliação sem levantar um dedo.**
  - **Persona + contexto:** Ana, analista fiscal, acompanha o fechamento mensal de apuração CBS/IBS de uma das empresas do grupo.
  - **Estado de entrada:** autenticada no FB_APU02, acabou de ver a notificação de que a apuração RFB do mês concluiu.
  - **Caminho:** Ana abre `/rfb/pagamentos-fornecedores` (aba "Pgtos Fornecedores" do módulo RFB) sem precisar rodar nenhuma importação antes — o sistema já buscou os pagamentos no SAP automaticamente assim que os créditos chegaram (realiza FR-1). Ela vê os cards de sumário já atualizados, incluindo créditos que antes nunca apareciam na tela por falta de qualquer pagamento registrado (realiza FR-11).
  - **Clímax:** o card "CBS Extinto" reflete fielmente a decisão da RFB para todos os créditos do mês — inclusive os que a RFB já considerava extintos mas que, sem esta iniciativa, ficariam invisíveis por falta de um pagamento manualmente importado.
  - **Resolução:** Ana filtra por status "pendente"/"aguardando pagamento" para focar apenas nas exceções que precisam de atenção humana.
  - **Edge case:** se a sincronização com o SAP falhou naquele dia (API fora do ar), Ana ainda vê os dados da última execução bem-sucedida e um aviso de que a atualização automática está atrasada (realiza FR-7).

- **UJ-2. Ana investiga um crédito que não conciliou.**
  - **Persona + contexto:** mesma Ana, agora dentro da lista filtrada por "pendente" ou "aguardando pagamento".
  - **Estado de entrada:** já na tela de conciliação, um item específico chamou atenção por valor alto de CBS ainda não extinto segundo a RFB.
  - **Caminho:** Ana clica no item e vê a origem do dado (SAP automático) e, quando aplicável, o motivo pelo qual o SAP não localizou o pagamento (ex.: `NAO_LOCALIZADO`, ou `FALLBACK` com ambiguidade sinalizada — realiza FR-8/FR-12). Se o indicador de pagamento já mostra o valor coberto mas a RFB ainda não confirmou extinção, Ana tem em mãos a evidência para decidir se contesta o crédito junto à RFB — o sistema nunca faz essa reclassificação por ela.
  - **Clímax:** Ana entende *por que* aquele crédito está pendente e *o quanto pode confiar* no indicador de pagamento disponível, sem precisar perguntar para o time de TI.
  - **Resolução:** Ana registra sua decisão (fora do escopo deste PRD o "como" — hoje não há workflow de anotação/aprovação na tela).
  - **Edge case:** se o match foi `FALLBACK` com múltiplos candidatos (nota fiscal de serviço, por exemplo), o indicador de pagamento é marcado como evidência fraca (ver FR-12) — o `status_conciliacao` do crédito nunca muda por causa disso; a ambiguidade só orienta o quanto Ana deve confiar no indicador antes de decidir agir.

- **UJ-3. Um administrador habilita a integração SAP para uma nova empresa.**
  - **Persona + contexto:** administrador do FB_APU02 responsável por onboarding de empresas no módulo de conciliação.
  - **Estado de entrada:** empresa recém-adicionada ao grupo, hoje usando apenas CSV manual.
  - **Caminho:** administrador acessa a tela de credenciais SAP (padrão análogo a `RFBCredentials`/`CGIBSCredentials`), cadastra client id/secret OAuth2 e o(s) código(s) de empresa SAP (BUKRS) correspondentes, testa a conexão (realiza FR-5, FR-6).
  - **Clímax:** teste de conexão retorna sucesso; a próxima apuração RFB dessa empresa já dispara sincronização automática.
  - **Resolução:** empresa passa a ter CSV como fallback, não mais como fonte primária.

## 3. Glossário

- **DF-e** — Documento Fiscal Eletrônico; termo guarda-chuva para NF-e, CT-e e NFS-e.
- **Chave de acesso** — identificador de 44 dígitos (NF-e/CT-e) ou 50 dígitos (NFS-e nacional) que referencia unicamente um DF-e.
- **CBS / IBS** — Contribuição sobre Bens e Serviços / Imposto sobre Bens e Serviços; os dois tributos da Reforma Tributária apurados pela RFB e pelo CGIBS respectivamente.
- **Apuração assistida** — modelo em que RFB/CGIBS pré-calculam débitos e créditos a partir dos DF-e, cabendo ao contribuinte validar, ajustar ou contestar.
- **Crédito CBS/IBS** — registro em `rfb_creditos` retornado pela RFB para uma chave de DF-e, com `situacao_credito` e `valor_cbs_nao_extinto`.
- **Liquidação financeira** — o pagamento efetivo ao fornecedor, condição do art. 27 da LC 214/2025 para apropriação do crédito.
- **Extinção do débito** — conceito do art. 47 da LC 214/2025: o crédito de IBS/CBS só é apropriável quando o débito da operação anterior for extinto (isto é, quando o valor da nota estiver integralmente pago). **Quem determina essa extinção é exclusivamente a RFB** (via `rfb_creditos.valor_cbs_nao_extinto`) — o FB_APU02 nunca faz essa determinação por conta própria (ver FR-4).
- **Pagamento por conta (ou pagamento parcial)** — liquidação parcial de uma nota fiscal em parcelas, podendo duas parcelas distintas ter o mesmo valor no mesmo dia; motivo pelo qual a deduplicação de pagamentos não pode se basear apenas em `(data_pagamento, valor_pagamento)` (ver FR-3).
- **Motor de conciliação** — a lógica (hoje em `RFBPagamentosFornecedoresHandler`) que cruza pagamentos, notas fiscais e créditos RFB para determinar o `status_conciliacao` de cada DF-e.
- **status_conciliacao** — determinado 100% por `rfb_creditos.valor_cbs_nao_extinto`, nunca pelos dados do SAP: `extinto` (crédito CBS já sem saldo pendente segundo a RFB), `pendente` (saldo de CBS não extinto **e** ao menos um pagamento localizado, de qualquer origem), `aguardando_pagamento` (saldo de CBS não extinto **e** nenhum pagamento localizado ainda — ver FR-11), `sem_dados` (pagamento localizado sem crédito correspondente em `rfb_creditos`).
- **Indicador de pagamento** *(novo conceito)* — informação auxiliar exibida junto ao crédito (pagamento localizado ou não, `matchType`, confiabilidade da evidência), derivada dos dados do SAP. Nunca determina `status_conciliacao` — apenas orienta a decisão humana de Ana.
- **paymentStatus (SAP)** — status retornado pela API SAP por chave: `PAGO_TOTAL`, `PAGO_PARCIAL`, `EM_ABERTO`, `NAO_LOCALIZADO`. Não deve ser confundido com `status_conciliacao` (interno ao FB_APU02, decidido pela RFB) — ver FR-4 para como um alimenta o indicador de pagamento sem jamais decidir o outro.
- **matchType (SAP)** — como o SAP resolveu a chave: `CHAVE` (vínculo direto pelo Nota Fiscal Writer — evidência forte), `FALLBACK` (heurística CNPJ + número de documento, usada para NFS-e/lançamentos diretos — evidência fraca quando ambígua, ver FR-12), `NAO_LOCALIZADO`.
- **settlementMode (SAP)** — modalidade de liquidação mapeada para o art. 27 da LC 214/2025 (ex.: transferência bancária, split payment); campo textual vindo do SAP, tabela de-para mantida pelo time fiscal do lado SAP.
- **BUKRS** — código de empresa no SAP; pode ter cardinalidade 1:N com `company_id` do FB_APU02 (ver FR-6 e Open Questions).
- **Origem do pagamento** — novo atributo em `pagamentos_fornecedores` distinguindo `csv` (import manual existente) de `sap_api` (nova carga automática).
- **Motor de sincronização SAP** *(novo componente)* — serviço backend responsável por consultar `SearchPayments`/`PaymentByDFe` em lote e persistir o resultado.

## 4. Features

### 4.1 Sincronização Automática de Pagamentos SAP
**Descrição:** ao concluir uma apuração RFB (transição de `rfb_requests.status` para `completed`, quando `rfb_creditos` é populado), o FB_APU02 dispara automaticamente uma consulta em lote à API SAP `SearchPayments` para as chaves de DF-e (NF-e/CT-e) daquela apuração, e grava os pagamentos retornados em `pagamentos_fornecedores`. Realiza UJ-1. `[ASSUMPTION: o disparo ocorre na mesma goroutine/fluxo que hoje processa o webhook RFB e grava rfb_creditos — reaproveitando o padrão de services/rfb_processor.go]`.

**Requisitos Funcionais:**

#### FR-1: Disparo automático pós-apuração
O sistema deve, ao concluir o processamento de uma apuração RFB, extrair as chaves de DF-e (NF-e/CT-e) dos créditos recém-gravados e disparar automaticamente a sincronização de pagamentos com o SAP. Realiza UJ-1.

**Consequências (testáveis):**
- Uma apuração RFB concluída sem nenhum crédito novo não dispara chamada ao SAP (evita chamadas vazias).
- Chaves de NFS-e são excluídas do lote enviado ao SAP no v1 (ver §5 Não-Metas).
- Se o disparo automático falhar (SAP indisponível), a apuração RFB em si não é afetada — a falha fica isolada ao processo de sincronização de pagamentos.
- `[ASSUMPTION: se o processamento RFB que grava rfb_creditos for reprocessado (reentrega de webhook, retry de infraestrutura) para a mesma apuração, o disparo de FR-1 verifica se já existe uma execução de sincronização em andamento ou concluída para aquele conjunto de chaves antes de disparar uma nova — evita chamadas redundantes à API SAP (reforça SM-C2)]`.

#### FR-2: Consulta em lote respeitando limites da API SAP
O sistema deve consultar `SearchPayments` em lotes de chaves, respeitando o limite máximo de chaves por requisição e o rate limit configurado.

**Consequências (testáveis):**
- O sistema usa lotes de até **500 chaves** por requisição — decisão de produto confirmada (o valor mais conservador entre os dois presentes na spec v0.1, que traz 5000 nas seções 2.1/4.2 e 500 na seção 7 e nos casos de teste 08/09). A unificação formal do valor do lado da spec SAP fica com o time FI/Basis e não bloqueia mais o desenvolvimento do FB_APU02.
- Requisições respeitam um rate limit configurável (sugestão inicial da spec SAP: 60 req/min por consumidor).
- Erros HTTP 429 disparam retry com backoff exponencial; erro 400 por lote grande demais reduz automaticamente o tamanho do lote e tenta novamente.
- `[ASSUMPTION: uma chave individual malformada (dígito verificador inválido) dentro de um lote é filtrada e reportada antes do envio, sem invalidar o lote inteiro — a spec não deixa explícito o comportamento da API para esse caso combinado (chave malformada dentro de uma requisição em lote)]`.
- Erro HTTP 401/403 (falha de autenticação/autorização) **não** aciona retry — indica problema de credencial, não falha transitória; dispara imediatamente o alerta de FR-7 sinalizando "credencial SAP inválida" para a empresa, distinto do alerta de falhas recorrentes genéricas.
- `[ASSUMPTION: N = 3 tentativas]` — após 3 tentativas malsucedidas (429/500), o lote é marcado como falho no log de execução (FR-7) sem interromper o processamento dos demais lotes.

#### FR-3: Persistência dos pagamentos com origem rastreável
O sistema deve gravar cada pagamento retornado pelo SAP como uma linha em `pagamentos_fornecedores`, marcando `origem = 'sap_api'` e preenchendo os novos campos de status (ver FR-4).

**Consequências (testáveis):**
- A chave de deduplicação de `pagamentos_fornecedores` passa a ser `(company_id, chave_doc, num_doc_pagamento, bukrs)` em vez de `(company_id, chave_doc, data_pagamento, valor_pagamento)`, usando o `clearingDocument` do SAP como identificador estável de cada liquidação. **Confirmado como necessário** (não mais suposição): pagamentos parciais da mesma nota — os chamados **"pagamentos por conta"** — podem legitimamente ter o mesmo valor no mesmo dia, o que colidiria com a constraint atual baseada em `(data_pagamento, valor_pagamento)`.
- Reprocessar a mesma chave (nova tentativa de sincronização) não duplica pagamentos já gravados (upsert idempotente).
- Cada item do array `payments[]` retornado pelo SAP vira uma linha própria em `pagamentos_fornecedores` (mantém o modelo atual de "uma linha por parcela").

#### FR-4: Dados do SAP como indicador auxiliar — RFB permanece fonte única da decisão de extinção
O sistema deve tratar os dados de pagamento retornados pelo SAP (`paymentStatus`, `matchType`) como **indicador de apoio** para o analista. A decisão de que um crédito está `extinto` continua sendo **exclusivamente** derivada de `rfb_creditos.valor_cbs_nao_extinto` — exatamente como hoje. O FB_APU02 nunca antecipa, substitui ou contraria essa decisão com base em dado do SAP.

**Consequências (testáveis):**
- `PAGO_TOTAL` e `PAGO_PARCIAL` geram linha(s) em `pagamentos_fornecedores` e acionam o indicador "pagamento localizado" na tela — **não alteram `status_conciliacao` por si só**.
- `status_conciliacao` continua determinado 100% por `rfb_creditos.valor_cbs_nao_extinto` (`extinto` quando zero, `pendente`/`aguardando_pagamento` quando maior que zero — ver FR-11), independentemente do que o SAP retornar para aquela chave.
- `EM_ABERTO` e `NAO_LOCALIZADO` **não** geram linha em `pagamentos_fornecedores`, mas ficam registrados no log de execução (FR-7) e contribuem para o estado `aguardando_pagamento` (FR-11).
- Se a soma de pagamentos localizados para uma chave já **bate com o valor da nota**, mas a RFB ainda não zerou `valor_cbs_nao_extinto`, a tela sinaliza essa divergência como **"pagamento localizado, aguardando confirmação da RFB"** — evidência para Ana decidir se contesta o crédito junto à RFB, mas o sistema **não** reclassifica o crédito como `extinto` por conta própria.
- Quando `matchType = FALLBACK` com ambiguidade sinalizada pelo SAP (`fallbackNote`, mais de um candidato), o indicador de pagamento é marcado como **evidência fraca** (ver FR-12) — isso nunca altera `status_conciliacao`, apenas a confiabilidade do indicador auxiliar.

**Notas:** valores de adiantamento (`UMSKZ`) que "aparecem naturalmente" no `payments[]` do SAP (spec §6.3) podem inflar o indicador de "valor coberto" sem representar, de fato, uma liquidação completa — como esse indicador é apenas auxiliar (nunca decide extinção), o pior caso é Ana ver um indicador otimista demais antes de confirmar com a RFB, não um crédito fechado incorretamente. Ainda assim, registrado como Open Question #7 para decidir se o indicador deve excluir adiantamentos do cálculo de "valor coberto".

### 4.2 Cadastro e Gestão de Credenciais SAP
**Descrição:** tela administrativa para cadastrar, testar e gerenciar as credenciais de acesso à API SAP por empresa, seguindo o padrão já existente de `RFBCredentials`/`CGIBSCredentials`. Realiza UJ-3.

**Requisitos Funcionais:**

#### FR-5: Cadastro de credenciais SAP por empresa
Um administrador pode cadastrar, editar e testar credenciais OAuth2 (client id/secret) de acesso à API SAP para uma empresa, com o segredo armazenado criptografado.

**Consequências (testáveis):**
- Segredos são criptografados com `ENCRYPTION_KEY` antes de persistir, nunca retornados em texto plano por nenhuma rota de leitura.
- Um botão de "testar conexão" executa uma chamada mínima à API SAP e reporta sucesso/falha sem persistir nenhum dado de negócio.
- Apenas usuários com papel admin acessam esta tela (`withAuth(..., "admin")`).
- `[ASSUMPTION: v1 suporta apenas OAuth2 client credentials; a spec também permite autenticação via certificado X.509 como alternativa, mas essa opção não é adotada nesta versão — revisitar se o time Basis exigir X.509 como padrão de conectividade do RISE]`.

#### FR-6: Mapeamento empresa ↔ código(s) de empresa SAP
Um administrador pode associar um `company_id` do FB_APU02 a um ou mais códigos de empresa SAP (BUKRS).

**Consequências (testáveis):**
- `[ASSUMPTION: um company_id pode mapear para N BUKRS; a sincronização consulta todos os BUKRS configurados e agrega os resultados — ver Open Questions #2]`.
- Uma empresa sem nenhum BUKRS configurado nunca dispara chamada ao SAP (permanece 100% no fluxo CSV).
- Falha em 1 de N `BUKRS` configurados não impede a sincronização dos demais — cada `BUKRS` tem seu próprio resultado de sucesso/falha no log de execução (FR-7), e apenas o(s) `BUKRS` que falhou entra em retry (não a execução inteira da empresa).
- A chave de deduplicação de FR-3 inclui `bukrs` justamente para evitar colisão caso dois BUKRS do mesmo `company_id` gerem, por coincidência, o mesmo número de documento de compensação.

### 4.3 Monitoramento da Sincronização
**Descrição:** visibilidade operacional sobre as execuções de sincronização, análoga ao padrão já existente para o ERP Bridge (`erp_bridge_runs`/`ERPBridgeLogs.tsx`). Realiza UJ-1 (edge case) e UJ-2.

**Requisitos Funcionais:**

#### FR-7: Histórico de execuções de sincronização
O sistema deve registrar e expor o histórico de execuções da sincronização com o SAP: empresa, data/hora, quantidade de chaves enviadas, quantidade encontrada por `paymentStatus`, erros e lotes com falha.

**Consequências (testáveis):**
- Cada execução (disparada por FR-1) gera um registro consultável via API e tela, com status `sucesso` / `parcial` / `falha`, incluindo o tempo total de execução (derivado de início/fim) como campo de auditoria.
- `[ASSUMPTION: N = 3 execuções]` — falhas recorrentes (3 execuções seguidas com erro) disparam um alerta por e-mail reaproveitando `services/email.go`; falha de autenticação (401/403, ver FR-2) dispara alerta imediato específico, sem esperar as 3 ocorrências.

#### FR-8: Exibição de origem e motivo na tela de conciliação
A tela `/rfb/pagamentos-fornecedores` deve exibir a origem do pagamento (`sap_api` vs `csv`) e, quando não houver pagamento localizado ou o match for ambíguo, o motivo correspondente retornado pelo SAP.

**Consequências (testáveis):**
- Um item com `matchType = FALLBACK` e ambiguidade sinalizada pelo SAP exibe um indicador visual distinto de um match direto por `CHAVE`.
- Um item `NAO_LOCALIZADO` no ciclo mais recente de sincronização exibe a data da última tentativa, com mensagem distinta de um item `EM_ABERTO` (fatura localizada mas ainda sem nenhuma liquidação) — "não conseguimos localizar o documento" é uma mensagem diferente de "localizamos o documento, mas ele ainda não foi pago".

### 4.4 Completude e Confiabilidade da Conciliação
**Descrição:** garante que todo crédito da RFB apareça na tela (mesmo sem nenhum pagamento localizado ainda) e que evidências fracas (matches ambíguos) tenham um espaço de revisão dedicado, em vez de ficarem diluídas entre os créditos já resolvidos. Realiza UJ-1 e UJ-2.

**Requisitos Funcionais:**

#### FR-11: Créditos sem pagamento localizado aparecem na tela
A tela de conciliação deve exibir **todo** crédito retornado pela RFB (`rfb_creditos`), mesmo quando nenhum pagamento — nem CSV, nem SAP — foi localizado para aquela chave.

**Consequências (testáveis):**
- `[ASSUMPTION: implica inverter a direção da consulta de conciliação — hoje ela parte de pagamentos_fornecedores e só depois cruza com rfb_creditos (backend/handlers/rfb_pagamentos_fornecedores.go), o que hoje torna um crédito sem nenhum pagamento invisível na tela. A consulta precisa passar a partir de rfb_creditos]`.
- Um crédito com `valor_cbs_nao_extinto > 0` e nenhum pagamento localizado aparece com o novo status `aguardando_pagamento`, distinto de `pendente` (que exige ao menos um pagamento já localizado).
- Os cards de sumário (incluindo "CBS Extinto") passam a refletir a totalidade dos créditos retornados pela RFB, não apenas o subconjunto que já tinha algum pagamento registrado.

#### FR-12: Revisão dedicada de matches ambíguos
A tela de conciliação deve oferecer um filtro/contador dedicado para créditos cujo indicador de pagamento veio de um match `FALLBACK` ambíguo, distinto do filtro geral de "pendente".

**Consequências (testáveis):**
- Um crédito com indicador de pagamento vindo de `matchType = FALLBACK` com ambiguidade sinalizada pelo SAP aparece no filtro/contador dedicado, independentemente do seu `status_conciliacao` atual (mesmo créditos já `extinto` segundo a RFB podem aparecer aqui, para auditoria da qualidade do dado de pagamento).
- Este filtro faz parte do fluxo normal de revisão de Ana (UJ-1/UJ-2), não uma auditoria opcional separada — a intenção é que a evidência fraca seja parte do trabalho de rotina, não algo que exige memória ou disciplina extra do analista.

### 4.5 Convivência com Importação Manual (CSV)
**Descrição:** o fluxo de import manual via CSV (`/importacoes/pagamentos-fornecedores`) permanece ativo como fallback, sem regressão de funcionalidade, para empresas sem integração SAP ainda configurada ou como contingência de correção manual.

**Requisitos Funcionais:**

#### FR-9: Coexistência de origens sem duplicidade
O sistema deve permitir que uma mesma chave de DF-e tenha pagamentos vindos tanto de CSV quanto da API SAP ao longo do tempo, sem duplicar valores na conciliação.

**Consequências (testáveis):**
- A chave de deduplicação adaptada em FR-3 previne duplicidade quando o `num_doc_pagamento` informado no CSV coincide com o `clearingDocument` retornado pelo SAP para a mesma liquidação.
- `[ASSUMPTION: em caso de conflito de valor para a mesma chave de deduplicação entre CSV e SAP, o registro de origem sap_api prevalece na exibição, mantendo o registro csv histórico para auditoria]`.
- Uma parcela de CSV legada sem `num_doc_pagamento` preenchido pode não deduplicar contra uma parcela SAP que descreve o mesmo pagamento real, inflando o indicador de "valor coberto" exibido para a chave. Como esse indicador é apenas auxiliar (FR-4 — nunca decide `status_conciliacao`), o pior efeito é um indicador otimista demais, não um crédito fechado incorretamente; ainda assim, o sistema sinaliza visualmente quando `total_pago` de uma chave excede `valor_nota` (indício de duplicidade ou match errado) para que Ana possa investigar.

#### FR-10: Isolamento multi-tenant nas chamadas ao SAP
Nenhuma chamada ou resposta da integração SAP pode misturar dados de empresas fora do escopo autorizado da empresa que originou a consulta.

**Consequências (testáveis):**
- A sincronização de uma empresa só envia chaves e só grava pagamentos associados ao(s) `BUKRS` explicitamente configurado(s) para aquele `company_id` (FR-6).
- Testado de forma equivalente ao caso de teste 11 da spec SAP ("fatura de empresa fora do escopo autorizado" → `NAO_LOCALIZADO`, sem vazamento de dados entre empresas).

## 5. Não-Metas (Explícitas)

- Este PRD não cobre o desenvolvimento da API no lado SAP — responsabilidade do time FI/Basis, documentada na spec técnica anexa (fora deste documento).
- Não inclui cálculo ou validação de valores de CBS/IBS — permanece responsabilidade da RFB e do motor de apuração já existente.
- **Suporte a NFS-e fica fora do v1** — o layout de decomposição da chave nacional de 50 dígitos ainda não está confirmado pelo time SAP (questão em aberto da própria spec técnica, seção 10). Só NF-e e CT-e entram no lote automático nesta primeira versão.
- Não inclui tratamento de adiantamentos (razão especial) — também fora do escopo da API SAP v1.
- Não inclui um workflow formal de anotação/aprovação para créditos pendentes (UJ-2 menciona a decisão de Ana, mas registrar essa decisão no sistema é uma iniciativa futura).
- Não inclui a tabela de-para completa de `settlementMode` → modalidade do art. 27 além de armazenar o valor textual retornado pelo SAP — a definição fiscal dessa tabela é responsabilidade do time fiscal do lado SAP (spec, seção 6.4, "Não obrigatório").
- Não inclui dashboards analíticos avançados sobre a saúde da integração SAP além do histórico básico de execuções (FR-7).

## 6. Escopo do MVP

### 6.1 Em Escopo
- Sincronização automática de pagamentos SAP (NF-e/CT-e) disparada pós-apuração RFB.
- Consulta em lote respeitando limites e rate limit da API SAP, com retry.
- Evolução de schema em `pagamentos_fornecedores` (origem, payment_status, match_type, settlement_mode) e ajuste da chave de deduplicação.
- Cadastro de credenciais SAP por empresa e mapeamento `company_id` ↔ `BUKRS`.
- Histórico de execuções de sincronização com alerta em falhas recorrentes.
- Exibição de origem e motivo na tela de conciliação existente.
- Créditos sem nenhum pagamento localizado aparecem na tela (inversão da consulta de conciliação) e revisão dedicada de matches ambíguos.
- Manutenção do fluxo CSV como fallback, sem regressão.
- Isolamento multi-tenant garantido nas duas pontas (configuração e consulta).
- **RFB permanece a única fonte de verdade para `status_conciliacao`** — os dados do SAP nunca decidem, por si só, que um crédito está extinto.

### 6.2 Fora de Escopo para o MVP
- Suporte a NFS-e (bloqueado por questão em aberto do lado SAP — layout da chave).
- Tratamento de adiantamentos e razão especial.
- Tabela de-para completa `settlementMode` → modalidade art. 27.
- Workflow de anotação/aprovação de decisão do analista sobre créditos pendentes. `[NOTE FOR PM: revisitar quando o volume de exceções manuais justificar um fluxo formal.]`
- Rollout simultâneo para todas as empresas do grupo — ver Rollout faseado.

## 7. Métricas de Sucesso

**Primárias**
- **SM-1**: Cobertura e velocidade da evidência de pagamento — percentual de créditos `pendente`/`aguardando_pagamento` (entre as chaves NF-e/CT-e do escopo automático) que passam a ter um indicador de pagamento localizado automaticamente via SAP dentro de X dias após a apuração, sem intervenção manual, reduzindo o tempo até Ana ter informação suficiente para decidir (aguardar a RFB ou contestar). **Não mede se o FB_APU02 fechou créditos como extinto** — essa decisão é sempre da RFB. Valida FR-1, FR-2, FR-3, FR-4, FR-11.
- **SM-2**: Redução do número de uploads de CSV mensais nas empresas com SAP configurado (medido por contagem de registros em `pagamentos_imports`, que já são exclusivamente eventos de import manual — não há necessidade de filtro por `origem`, coluna que não existe nessa tabela), comparando antes/depois do rollout por empresa. Valida FR-9.

**Secundárias**
- **SM-3**: Taxa de sucesso das chamadas à API SAP (percentual de lotes concluídos sem erro terminal) — indicador de saúde da integração. Valida FR-2, FR-7.

**Contra-métricas (não otimizar)**
- **SM-C1**: Taxa de créditos com indicador de pagamento vindo de match `FALLBACK` ambíguo que Ana efetivamente abre na visão dedicada de revisão (FR-12) antes de agir não deve cair perto de zero — indicaria que o indicador de evidência fraca está sendo ignorado ou recebendo confiança automática demais, mesmo sem o sistema decidir nada sozinho. Contrabalança SM-1 (não vale acelerar a cobertura de evidência se ninguém revisar a evidência fraca).
- **SM-C2**: Volume de chamadas à API SAP (lotes/dia) não deve crescer descontroladamente para "melhorar" SM-1 artificialmente — contrabalança SM-1 e reforça o rate limit de FR-2.

## 8. Riscos e Mitigações

| Risco | Impacto | Mitigação |
|---|---|---|
| Spec SAP ainda é rascunho v0.1, pendente validação FI/Basis — contrato pode mudar antes do build | Retrabalho na integração | Isolar o cliente SAP em serviço dedicado (baixo acoplamento, análogo a `services/rfb.go`); revisitar FRs quando a spec fechar v1.0 |
| Ainda que o FB_APU02 já opere com lote de 500 (FR-2), a spec do lado SAP segue inconsistente (500 vs 5000) até o time FI/Basis corrigi-la | Baixo — decisão já mitigada do nosso lado | Nenhuma ação adicional necessária no FB_APU02; acompanhar a correção da spec como item de follow-up externo |
| Match ambíguo via fallback (lançamento direto FI para NF-e/CT-e, ex. FB60/FB01) pode vincular pagamento à nota errada, mesmo quando o valor "bate" | **Baixo/médio** — como FR-4 nunca decide `status_conciliacao` a partir do SAP, o pior efeito é Ana confiar demais num indicador de pagamento impreciso, não um crédito fiscal fechado incorretamente | RFB continua a única fonte de verdade para extinção (FR-4); indicador marcado como evidência fraca; filtro dedicado de revisão (FR-12) traz o item para o fluxo normal de trabalho de Ana, em vez de depender de auditoria opcional |
| Indisponibilidade da API SAP (janela de disponibilidade da spec é 7h-22h em dias úteis, além de janelas de manutenção do RISE) | Atraso na conciliação; uma apuração RFB concluída à noite/fim de semana não encontra a API SAP disponível | Fallback CSV mantido (FR-9); retry com backoff (FR-2); alerta em falhas recorrentes (FR-7); disparo de FR-1 fora da janela aguarda o próximo horário disponível em vez de falhar repetidamente |
| Estorno/reclassificação contábil contando como pagamento se o filtro de tipo de documento (BLART) do lado SAP falhar ou estiver mal configurado | Baixo — infla o indicador auxiliar de "valor coberto" (FR-4), não a decisão de extinção (sempre da RFB) | Filtro de BLART é responsabilidade do lado SAP (spec §6.3); FB_APU02 depende desse filtro estar corretamente configurado — ver Open Question #6; sinalização visual quando `total_pago > valor_nota` (FR-9) ajuda a detectar o sintoma |
| Adiantamentos (`UMSKZ`) podem inflar o indicador auxiliar de "valor coberto" (FR-4) sem representar, de fato, extinção do débito da operação | Baixo — afeta apenas a precisão do indicador que Ana usa para decidir, não a decisão oficial de extinção | Validar com o time fiscal se o indicador deve excluir adiantamentos do cálculo — ver Open Question #7 |
| Mapeamento incorreto `company_id` ↔ `BUKRS` | Vazamento de dados entre empresas/tenants | FR-10 + testes de isolamento equivalentes ao caso de teste 11 da spec SAP; dedup key inclui `bukrs` (FR-3/FR-6) |
| **🚧 BLOQUEADOR DE CRONOGRAMA:** padrão de exposição externa do SAP RISE (Cloud Connector/API Management/WAF) ainda não definido pelo time Basis/Segurança | Nenhuma FR de sincronização funciona sem essa definição — diferente das demais Open Questions, esta bloqueia o início do desenvolvimento, não só um detalhe paralelo | Levantar com Basis/Segurança **antes do kickoff de implementação**, com dono nomeado e prazo — ver Open Question #4 |
| Sem trilha de auditoria para o indicador de pagamento: `rfb_creditos` é sobrescrito via UPSERT sem histórico | Se um indicador de pagamento se mostrar equivocado meses depois (ex.: Ana contestou um crédito com base num indicador de evidência fraca que era falso), não há como reconstruir qual match/soma gerou aquele indicador na época | Registrar em `pagamentos_fornecedores`/`sap_sync_runs` (addendum §B/§C) os dados suficientes para reconstruir o indicador histórico — ver addendum; risco de baixo impacto porque o sistema não toma decisão fiscal, apenas exibe evidência |

## 9. Requisitos Não Funcionais (Cross-Cutting)

- **Desempenho:** a sincronização de uma apuração típica deve completar dentro do SLA da própria API SAP (lote de 500 chaves, dentro do orçamento de 160s p95 informado para lotes maiores na spec) mais overhead de rede/retry do lado FB_APU02; alvo de disponibilidade dos dados na tela em até 24h após a apuração concluir (a confirmar com o time fiscal).
- **Disponibilidade:** a API SAP opera em horário comercial estendido — 7h às 22h, dias úteis — além de janelas de manutenção do ambiente RISE. O disparo automático (FR-1) deve tolerar esse horário: uma apuração concluída fora da janela aguarda o próximo horário disponível em vez de falhar repetidamente.
- **Segurança:** credenciais SAP criptografadas em repouso (`ENCRYPTION_KEY`), OAuth2 client credentials, TLS 1.2+, acesso à tela de credenciais restrito a admin.
- **Confiabilidade:** falha na sincronização SAP não pode impactar o fluxo de apuração RFB nem o fluxo de import CSV — processos independentes e isolados.
- **Observabilidade:** toda execução de sincronização é logada com correlationId, seguindo o padrão `log.Printf("[ComponentName] ...")` já usado no projeto; histórico consultável (FR-7).
- **Multi-tenancy:** nenhuma consulta ou gravação cruza `company_id` (ver FR-10); segue o mesmo modelo de isolamento explícito por `WHERE company_id = $1` já usado no restante do backend.

## 10. Constraints e Guardrails

**Compliance**
- LC 214/2025, arts. 27, 28 e 46 (liquidação financeira condiciona apropriação de crédito) é o driver regulatório desta iniciativa.
- LC 214/2025, art. 47, é o fundamento de por que esta iniciativa importa: o crédito de IBS/CBS é apropriável quando o débito da operação anterior for **extinto**. **O FB_APU02 não faz essa determinação legal por conta própria** — a RFB continua sendo a única fonte de verdade sobre extinção (`rfb_creditos.valor_cbs_nao_extinto`, ver FR-4). Os dados de pagamento do SAP servem apenas como evidência de apoio para o analista decidir se e quando contestar um crédito junto à RFB.
- Ato Conjunto RFB/CGIBS nº 1/2025 estabelece caráter informativo da apuração assistida em 2026 — janela de calibração antes de a exigência se tornar vinculante.
- Dados de pagamento tratados são de pessoa jurídica (CNPJ fornecedor); se fornecedores pessoa física (CPF) entrarem no escopo no futuro, avaliar minimização de dados e base legal com o DPO (herdado da spec SAP, seção 8).

**Privacidade e Retenção**
- Segue a mesma política de retenção já aplicada a `nfe_entradas`/`rfb_creditos` — sem descarte automático.

## 11. Integração e Dependências

- **API SAP "Consulta de Pagamentos por Chave de DF-e"** (spec v0.1, anexo técnico) — dependência externa, time FI/Basis, status "em elaboração". Este PRD assume que a API expõe `PaymentByDFe` (individual) e `SearchPayments` (lote) conforme especificado.
- **`rfb_creditos`** — dependência interna já existente; a sincronização SAP consome as chaves de DF-e já gravadas pelo processamento RFB (`services/rfb_processor.go`).
- **Padrão de exposição externa do ambiente RISE** (Cloud Connector, API Management, WAF) — ainda não definido; bloqueia o desenho de conectividade de rede do lado Go (Open Question #4).
- **`services/email.go`** — reaproveitado para alertas de falha recorrente (FR-7).

## 12. Rollout e Gestão de Mudança

- Rollout faseado por empresa: iniciar com 1-2 empresas piloto que já tenham a API SAP disponível e credenciais configuráveis; demais empresas permanecem 100% no fluxo CSV até onboarding (UJ-3).
- Nenhuma migração retroativa obrigatória — pagamentos já importados via CSV permanecem intocados; a nova chave de deduplicação (FR-3) é compatível com dados existentes.
- Comunicação ao time fiscal sobre a mudança de comportamento da tela (menos necessidade de upload manual) antes do rollout de cada empresa piloto.
- Como o FB_APU02 nunca decide `status_conciliacao` por conta própria (FR-4), não há necessidade de um "kill-switch" para reverter créditos fechados incorretamente — a RFB é sempre quem decide. Ainda assim, o piloto deve validar que o indicador de pagamento (payment_status/match_type/settlement_mode) pode ser corrigido/reprocessado manualmente se um lote de sincronização gravar dado incorreto, antes de expandir para as demais empresas.

## 13. Perguntas em Aberto

> As perguntas #6 e #7 têm severidade reduzida por afetarem apenas a precisão do indicador auxiliar (ver §1 Princípio de design) — nunca uma decisão fiscal.

1. Layout e decomposição da chave NFS-e nacional (já fora do MVP, mas necessário para uma v2 com suporte a NFS-e).
2. Confirmar se, na estrutura real da Ferreira Costa, um `company_id` mapeia para 1 ou N códigos de empresa SAP (`BUKRS`).
3. Volumetria mensal esperada de chaves para dimensionar SLA e rate limit (pergunta que a própria spec SAP também deixa em aberto).
4. Padrão de exposição externa no ambiente RISE (Cloud Connector / API Management / WAF) — depende de definição conjunta com Basis e Segurança.
5. Tabela de-para `settlementMode` → modalidade do art. 27 — quando/quem define, e se o FB_APU02 precisa expor essa modalidade na tela de conciliação em uma iteração futura.
6. Lista de tipos de documento contábil (`BLART`) considerados pagamento válido (estornos/reclassificações filtrados do lado SAP) — definição responsabilidade do time fiscal/FI do lado SAP (spec §6.3 e §10); o FB_APU02 depende dessa configuração para a precisão do indicador auxiliar de FR-4 (não mais uma decisão fiscal, dado que a RFB decide extinção).
7. Valores de adiantamento (`UMSKZ`, razão especial) que "aparecem naturalmente" no retorno do SAP devem contar para o indicador de "valor coberto" de FR-4, ou o motor de conciliação precisa desconsiderá-los? Afeta apenas a precisão do indicador exibido a Ana, não uma decisão fiscal.
8. Definição de kill-switch operacional: como reprocessar/corrigir um lote de sincronização que gravou indicador de pagamento incorreto, antes da expansão do rollout além do piloto (ver §12 Rollout).

## 14. Índice de Suposições

- §4.1 FR-1 — disparo ocorre na mesma goroutine/fluxo do processamento RFB existente.
- §4.1 FR-1 — reentrega/reprocessamento do webhook RFB não deve redisparar uma sincronização redundante para o mesmo conjunto de chaves.
- §4.1 FR-2 — chave individual malformada dentro de um lote é filtrada/reportada antes do envio, sem invalidar o lote inteiro.
- §4.1 FR-2 — N = 3 tentativas antes de marcar um lote como falho.
- §4.1 FR-3 — chave de deduplicação `(company_id, chave_doc, num_doc_pagamento, bukrs)` — implementação específica confirmada como necessária pelo cenário de "pagamentos por conta", o identificador exato ainda é uma suposição de design.
- §4.2 FR-5 — v1 adota apenas OAuth2 client credentials; X.509 (alternativa presente na spec) não é adotado nesta versão.
- §4.2 FR-6 — um `company_id` pode mapear para N `BUKRS`.
- §4.3 FR-7 — N = 3 execuções seguidas com erro antes do alerta de falha recorrente.
- §4.4 FR-11 — mudança de direção da consulta de conciliação (de `pagamentos_fornecedores` para `rfb_creditos`) necessária para exibir créditos sem pagamento localizado.
- §4.5 FR-9 — em conflito de valor entre CSV e SAP para a mesma liquidação, a origem `sap_api` prevalece na exibição.

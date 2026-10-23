---
title: 'NFS-e: listagem + telas de consulta (tomadas/prestadas) + ativação do menu'
type: 'feature'
created: '2026-10-23'
status: 'done'
review_loop_iteration: 0
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-captura-plano.md', '{project-root}/_bmad-output/implementation-artifacts/spec-nfse-agendamento-persistencia.md']
baseline_commit: 'fcbbbb0827b6ecdf0e4d7c532798b44d730a832b'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `nfse_entradas`/`nfse_saidas` são gravadas desde a sub-spec #7, mas nunca lidas — zero endpoint de listagem, zero tela. O botão de disparo manual (`POST /api/nfse/buscar`, existe desde #7) não está em nenhuma UI.

**Approach:** espelhar o padrão já validado de `NfeEntradasListHandler`/`NfeSaidasListHandler` + `ConsultaNFesEntradas.tsx` pra criar `/api/nfse-entradas`/`/api/nfse-saidas` + 2 componentes de consulta, ligados na aba "NFS-e" de `NotasImportadasModulo.tsx` (`CONSULTA_POR_TIPO`). **Correção de escopo da investigação** (não é decisão de produto, é fato do código): as abas "NFS-e" em `DFesModulo.tsx` e `MalhaFinaModulo.tsx` servem propósitos DIFERENTES — upload manual de XML e detecção de divergência, respectivamente — nenhum dos dois foi construído nesta série de sub-specs (a captura é automática via mTLS, não upload; não existe lógica de malha fina pra NFS-e). Só a aba de `NotasImportadasModulo.tsx` (consulta) é ativada aqui; as outras duas continuam `enabled: false`, documentadas como funcionalidades futuras e distintas.

## Boundaries & Constraints

**Always:** `/api/nfse-entradas`, `/api/nfse-entradas/competencias`, `/api/nfse-saidas`, `/api/nfse-saidas/competencias` espelhando exatamente o padrão de paginação/filtro/competência de `NfeEntradasListHandler`/`NfeEntradasCompetenciasHandler` (`nfe_entradas.go`); componentes `ConsultaNFSeEntradas.tsx`/`ConsultaNFSeSaidas.tsx` espelhando a estrutura de `ConsultaNFesEntradas.tsx` (filtros, tabela ordenável, paginação, drawer de detalhe, cards de totalizador no topo); cards de totalizador incluem TODOS os tributos do schema (ISS, PIS, COFINS, INSS, IRRF, CSLL) com um aviso visível (ex: banner) de que os valores estão zerados até o parser real do payload ADN ser ajustado (Design Notes de `spec-nfse-cliente-adn.md` — não esconder que são placeholder); botão "Buscar Agora" (chama `POST /api/nfse/buscar`) adicionado em `NFSeCertificado.tsx`, não nas novas telas de consulta; ativar SOMENTE a aba "NFS-e" de `NotasImportadasModulo.tsx` (`enabled: true`, `CONSULTA_POR_TIPO['nfse-entrada']`/`['nfse-saida']`); `AppSidebar.tsx` ganha itens "NFS-e Entradas"/"NFS-e Saídas" apontando pras novas rotas (`disabled: true` removido); `Painel.tsx` ganha card "NFS-e" com status real (contagem de registros ou `'empty'` dinâmico, não mais hardcoded).

**Ask First:** nenhuma — as decisões de produto da spec-mãe (fora de escopo: parser real, ligação com apuração) já resolvidas; a única questão nova (onde ativar o menu) foi resolvida por investigação de código, não por preferência.

**Never:** ativar a aba "NFS-e" em `DFesModulo.tsx` (upload manual — funcionalidade distinta, não construída) ou `MalhaFinaModulo.tsx` (detecção de divergência — funcionalidade distinta, não construída); mudar o parser de payload em `services/nfse_adn.go`; ligar os totais de NFS-e à apuração de crédito CBS/IBS (decisão já tomada: fica pra depois).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Lista sem nenhum documento capturado | `nfse_entradas`/`nfse_saidas` vazias pra empresa | Tabela vazia com mensagem "nenhum documento encontrado", totais zerados, sem erro | — |
| Botão "Buscar Agora" clicado | Certificado ativo cadastrado | Chama `POST /api/nfse/buscar`, mostra resultado (recebidos/novos/ignorados) via toast | Erros 400/429/502 já mapeados pelo backend, exibidos como toast de erro |
| Botão "Buscar Agora" sem certificado | Sem `nfse_certificados` pra empresa | Botão desabilitado ou mensagem clara antes de tentar (evita round-trip desnecessário) | — |
| Filtro por competência sem resultado | Mês/ano sem documentos | Tabela vazia, totais zerados, sem erro | — |

</frozen-after-approval>

## Code Map

- `backend/handlers/nfe_entradas.go`, `nfe_saidas.go` -- `NfeEntradasListHandler`/`NfeEntradasCompetenciasHandler` e equivalentes, padrão exato a replicar
- `backend/main.go:604-613` -- bloco de registro de rotas NF-e, replicar pra `/api/nfse-entradas`/`/api/nfse-saidas`
- `backend/migrations/133_nfse_certificados_e_schema.sql` -- schema de `nfse_entradas`/`nfse_saidas`, colunas a expor na listagem
- `frontend/src/pages/ConsultaNFesEntradas.tsx` (507 linhas) -- estrutura completa a espelhar: filtros, tabela ordenável, paginação, drawer de detalhe, cards de totalizador
- `frontend/src/pages/NotasImportadasModulo.tsx:18-39` -- `CONSULTA_POR_TIPO`, `TIPOS` (`nfse: enabled: false` → `true`), `DIRECOES_DISPONIVEIS`
- `frontend/src/pages/DFesModulo.tsx:26`, `MalhaFinaModulo.tsx:19` -- aba "NFS-e" NÃO ativada nesta sub-spec (propósitos distintos, ver Intent)
- `frontend/src/components/AppSidebar.tsx:106` ("NFS-e Saídas", `disabled: true`) -- habilitar + adicionar "NFS-e Entradas" equivalente
- `frontend/src/pages/Painel.tsx:33` -- card "NFS-e" com status hardcoded `'empty'`, trocar por dado real
- `frontend/src/pages/NFSeCertificado.tsx` (417 linhas) -- adicionar botão "Buscar Agora" chamando `POST /api/nfse/buscar`

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/nfse_list.go` (novo) -- `NFSeEntradasListHandler`, `NFSeEntradasCompetenciasHandler`, `NFSeSaidasListHandler`, `NFSeSaidasCompetenciasHandler`, espelhando paginação/filtro/competência de `nfe_entradas.go`/`nfe_saidas.go`.
- [x] `backend/main.go` -- registrar `/api/nfse-entradas`, `/api/nfse-entradas/competencias`, `/api/nfse-saidas`, `/api/nfse-saidas/competencias`.
- [x] `backend/handlers/nfse_list_test.go` -- cobrir a I/O Matrix (lista vazia, filtro por competência sem resultado, isolamento entre tenants).
- [x] `frontend/src/pages/ConsultaNFSeEntradas.tsx`, `ConsultaNFSeSaidas.tsx` (novos) -- espelhando `ConsultaNFesEntradas.tsx`, com banner de aviso sobre valores tributários placeholder.
- [x] `frontend/src/pages/NotasImportadasModulo.tsx` -- `nfse: enabled: true`, `CONSULTA_POR_TIPO['nfse-entrada']`/`['nfse-saida']`, `DIRECOES_DISPONIVEIS['nfse']`.
- [x] `frontend/src/components/AppSidebar.tsx` -- "NFS-e Entradas"/"NFS-e Saídas" habilitados, apontando pras novas rotas.
- [x] `frontend/src/pages/Painel.tsx` -- card "NFS-e" com status/contagem real (nova chamada leve, ex: `GET /api/nfse-entradas?page_size=1` ou um endpoint de resumo, à escolha da implementação).
- [x] `frontend/src/pages/NFSeCertificado.tsx` -- botão "Buscar Agora" (`POST /api/nfse/buscar`), desabilitado sem certificado ativo, resultado via toast.

**Acceptance Criteria:**
- Given uma empresa sem nenhum documento NFS-e capturado, when a tela de consulta é aberta, then mostra lista vazia e totais zerados sem erro.
  - ✅ `backend/handlers/nfse_list_test.go:TestNFSeEntradasListHandler_SemDocumentos_ListaVaziaSemErro` e `TestNFSeSaidasListHandler_SemDocumentos_ListaVaziaSemErro` (HTTP 200, `total=0`, `items=[]`, todos os totais em 0). No frontend, `ConsultaNFSeEntradas.tsx`/`ConsultaNFSeSaidas.tsx` renderizam "Nenhum documento encontrado..." quando `items.length === 0` e só mostram os cards de totalizador quando `total > 0`.
- Given o botão "Buscar Agora" clicado com certificado ativo, when a busca retorna, then o resultado (recebidos/novos/ignorados) aparece via toast, usando as mesmas mensagens de erro já mapeadas pelo backend (400/429/502).
  - ✅ `NFSeCertificado.tsx:handleBuscarAgora` chama `POST /api/nfse/buscar`, exibe `toast.success` com recebidos/novos/ignorados no sucesso e `toast.error(data?.error)` no erro — o backend já mapeia 400/429/502 com mensagem pronta em `mapExecutarCicloNFSeErr` (`backend/handlers/nfse_busca.go`), reaproveitada sem alteração. Botão fica desabilitado (`certificadoIndisponivel`) quando não há certificado ativo, evitando o round-trip — cenário já coberto por `TestBuscarNFSeHandler_SemCertificado_Retorna400SemChamadaRede`/`TestBuscarNFSeHandler_CertificadoVencido_Retorna400SemChamadaRede` (nfse_busca_test.go, não alterado nesta spec).
- Given uma empresa B, when ela lista NFS-e via `/api/nfse-entradas`, then nunca vê documentos da empresa A (mesmo padrão de tenant scoping de `GetEffectiveCompanyID` já usado em `nfe_entradas.go`).
  - ✅ `backend/handlers/nfse_list_test.go:TestNFSeEntradasListHandler_IsolamentoEntreEmpresas` e `TestNFSeSaidasListHandler_IsolamentoEntreEmpresas` (empresa A grava 1 documento, empresa B consulta o mesmo endpoint e recebe `total=0`/`items=[]`).
- Given as abas "NFS-e" em `DFesModulo.tsx`/`MalhaFinaModulo.tsx`, when o usuário as visita, then continuam desabilitadas (nenhuma mudança nelas nesta sub-spec).
  - ✅ Verificado estaticamente: nenhuma alteração foi feita em `frontend/src/pages/DFesModulo.tsx` nem em `frontend/src/pages/MalhaFinaModulo.tsx` nesta sub-spec (confirmado via `git status` — só os 6 arquivos modificados e 4 novos listados acima mudaram).

## Design Notes

Os valores tributários (ISS/PIS/COFINS/etc.) exibidos nos cards de totalizador vão aparecer zerados até o parser de `services/nfse_adn.go` ser ajustado com um payload real confirmado (ver Design Notes de `spec-nfse-cliente-adn.md`) — por isso o banner de aviso é um requisito, não um nice-to-have: evita que o usuário interprete "zero" como "sem crédito" quando na verdade é "captura ainda não confirmada".

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: compila sem erro
- `cd backend && go test ./handlers/... -run NFSe -v` -- expected: todos os testes passam
- `cd frontend && npx tsc -p tsconfig.app.json --noEmit` -- expected: zero erros novos (sempre com `-p tsconfig.app.json`)

**Manual checks (se CLI não cobrir):**
- Abrir `/importacoes` (ou rota equivalente de `NotasImportadasModulo`) e confirmar que a aba NFS-e aparece habilitada, com as 2 direções (entrada/saída) navegáveis.
- Confirmar visualmente que `DFesModulo`/`MalhaFinaModulo` continuam com a aba NFS-e desabilitada.

## Spec Change Log

**Loopback 1 (2026-10-23)** — revisão adversarial (Blind Hunter + Edge Case Hunter, 2 rodadas) sobre a 1ª implementação achou 19 itens "patch" (todos aplicados, sem decisão do usuário necessária — nenhum tinha tradeoff de produto/segurança real):
- Backend: erro do SUM de totais não checado (totais silenciosamente zerados em falha de banco); `rows.Err()` não checado; sem limite superior em `page`; `float64` NaN/Inf não sanitizado antes do JSON; `nfseCompetencias` sem checagem de método; `table` concatenado em SQL sem allowlist defensivo; arquivo de teste desformatado; sem teste do fallback de `sort_by` fora da allowlist.
- Frontend (`Painel.tsx`): falha parcial de fetch tratada como sucesso/zero; status `'error'` nunca usado (colapsa com `'empty'`); sem proteção contra resposta fora de ordem na troca de empresa.
- Frontend (`ConsultaNFSeEntradas.tsx`/`ConsultaNFSeSaidas.tsx`, mesmos patches nos 2): efeito de competências sem proteção contra resposta fora de ordem; filtro de mês/ano não resetado na troca de empresa; página não resetada na troca de empresa; totais/contador continuam mostrando dado antigo durante erro; `navigator.clipboard` sem guarda.
- Frontend (`NFSeCertificado.tsx`): "Buscar Agora" sem timeout/AbortController; botão não considera `saving`/`deleting` em voo; resposta ok-mas-inválida reportada como sucesso com zeros.

**Achados "defer" (registrados em `deferred-work.md`):** card do Painel usa 2 chamadas pesadas (COUNT+SUM+SELECT) só pra um número de resumo; estilo visual do aviso de valores zerados inconsistente em 3 lugares; `ConsultaNFSeEntradas`/`ConsultaNFSeSaidas` são quase duplicatas sem extração de código compartilhado (mesmo padrão do par NF-e); sidebar com rótulos idênticos em 2 seções com motivos de desabilitação diferentes; zero cobertura de teste de frontend (convenção já existente no projeto, não lacuna desta sub-spec).

## Suggested Review Order

**Backend: listagem com as mesmas garantias de robustez já aplicadas em specs anteriores**

- `nfseList` — paginação, sanitização de NaN/Inf, allowlist de tabela/sort_by, tratamento de erro completo (SUM + `rows.Err()`).
  [`nfse_list.go:134`](../../backend/handlers/nfse_list.go#L134)

- `nfseSanitizeFloat` — por que NUMERIC do Postgres pode virar NaN e quebrar o JSON.
  [`nfse_list.go:56`](../../backend/handlers/nfse_list.go#L56)

**Frontend: evitar "zero" enganoso (o mesmo princípio do banner de tributos, agora também no card resumo)**

- Falha parcial agora rejeita de verdade, e `'error'` é um status visualmente distinto de "zero confirmado".
  [`Painel.tsx:56`](../../frontend/src/pages/Painel.tsx#L56)

- "Buscar Agora" com timeout/cancelamento — não fica pendurado indefinidamente nem finge sucesso numa resposta inválida.
  [`NFSeCertificado.tsx:218`](../../frontend/src/pages/NFSeCertificado.tsx#L218)

**Frontend: troca de empresa não deixa dado da empresa anterior vazar**

- Padrão `ignore`/reset de filtro/reset de página aplicado nos 2 componentes de consulta (idêntico nos dois).
  [`ConsultaNFSeEntradas.tsx`](../../frontend/src/pages/ConsultaNFSeEntradas.tsx)
  [`ConsultaNFSeSaidas.tsx`](../../frontend/src/pages/ConsultaNFSeSaidas.tsx)

**Ativação do menu (escopo corrigido pela investigação, não por preferência)**

- Só a aba de consulta ativada — upload manual e malha fina ficam de fora, são features distintas não construídas.
  [`NotasImportadasModulo.tsx`](../../frontend/src/pages/NotasImportadasModulo.tsx)

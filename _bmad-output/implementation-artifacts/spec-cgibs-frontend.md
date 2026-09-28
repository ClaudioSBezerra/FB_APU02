---
title: 'CGIBS: frontend — Importar Movimento (contrato novo) + Extrato/Conta Corrente'
type: 'feature'
created: '2026-09-28'
status: 'done'
baseline_commit: '9640802'
review_loop_iteration: 1
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-cgibs-conta-corrente-plano.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `CGIBSApuracao.tsx` fala o contrato antigo (`data.requests`, `req.status`, `req.resumo`, POST sem corpo) — o backend (4 sub-specs já commitadas) fala outro (`data.solicitacoes`, `situacao_solicitacao`, POST exige `{data_ini,data_fim}`, sem `resumo` embutido). Sem esta sub-spec, a tela quebra (lista sempre vazia, botão sempre 400). Também falta a tela de verdade pro modelo de conta corrente — hoje "Créditos IBS" fica desabilitada sem sentido, já que o ledger já combina débito+crédito por operação.

**Approach:** reescrever `CGIBSApuracao.tsx` pro contrato novo (seletor de período pra solicitar, histórico com os estados reais da CGIBS); criar `CGIBSExtrato.tsx` (nova tela: resumo agregado por período + lista de operações + drill-down de lançamentos), habilitando a aba hoje desabilitada "Créditos IBS" como "Extrato IBS" — decisão de produto já resolvida (o modelo real não separa mais débito/crédito).

## Boundaries & Constraints

**Always:**
- `CGIBSApuracao.tsx`: novo `CGIBSSolicitacao` (id, cnpj_base, id_solicitacao_externo?, tipo_solicitacao, situacao_solicitacao, data_solicitacao, data_transacao_ini, data_transacao_fim, qtd_operacoes, qtd_arq_vinculados, error_message?, created_at, updated_at) substitui `CGIBSRequest`/`CGIBSResumo`; ler `data.solicitacoes`.
- Badges por `situacao_solicitacao`: `solicitada`/`enviada` (amarelo, "em andamento"), `gerada` (azul, "arquivo pronto"), `cancelada`/`expirada` (cinza), mais um estado visual local "erro" quando `error_message` preenchido (vermelho) — não é um valor de `situacao_solicitacao`, é derivado no frontend.
- "Solicitar Apuração IBS" abre 2 campos de data (início/fim do período) antes de confirmar; POST `{data_ini, data_fim}` (formato `YYYY-MM-DD`, o que o `<input type="date">` já dá nativamente).
- Tratar respostas por código: 201 sucesso; 400 mensagem de validação do backend; 409 "já existe uma solicitação em andamento" (ou "existem arquivos vinculados", no caso do DELETE) — mostrar a mensagem do backend, não genérica; 502 mostrar `error_message`/detalhe; 503 mantém o aviso de piloto já existente.
- `handleClearErrors` filtra por `error_message` presente (não mais `status==='error'`).
- Manter o banner de piloto e o cronograma (ainda válidos, não são o problema).
- `CGIBSExtrato.tsx` (novo): seletor de período (`data_ini`/`data_fim`, default mês corrente) → `GET /api/cgibs/resumo?data_ini=&data_fim=` (cards: total de operações, e os 7 valores agrupados em 3 blocos visuais — "Recursos financeiros" [disponível/a transferir], "Créditos" [a apropriar/não utilizado/utilizado], "Débitos" [em aberto/extinto]); lista paginada via `GET /api/cgibs/operacoes` (chave de acesso, CNPJ fornecedor/adquirente, datas, botão "ver lançamentos"); drill-down via `GET /api/cgibs/operacoes/{id}/lancamentos` (tabela: data do lançamento, código/descrição do movimento, os 7 valores).
- `navigation.ts`: trocar `{label:'Créditos IBS', path:'#', disabled:true}` por `{label:'Extrato IBS', path:'/cgibs/extrato'}` (habilitada); manter "Pagamentos IBS"/"Pgtos Fornecedores"/"Concluir Apuração" desabilitadas (APIs correspondentes não existem na CGIBS ainda); adicionar `/cgibs/extrato` em `cgibsPaths` (linha ~108) pra `getActiveModule` reconhecer.
- `App.tsx`: rota `/cgibs/extrato` → `CGIBSExtrato`.

**Never:** mexer em `CGIBSPainel.tsx`/`CGIBSDebitos.tsx` (cálculo interno estimado, continuam válidos e intocados); implementar Pagamentos/Pgtos Fornecedores/Concluir Apuração (APIs não existem); mudar backend.

</frozen-after-approval>

## Code Map

- `frontend/src/pages/CGIBSApuracao.tsx` -- reescrever (279 linhas atuais)
- `frontend/src/pages/CGIBSExtrato.tsx` (novo)
- `frontend/src/lib/navigation.ts:35-44,87,108-109` -- módulo `cgibs`
- `frontend/src/App.tsx` -- rota nova
- `backend/handlers/cgibs_apuracao.go`, `cgibs_operacoes.go`, `cgibs_resumo.go` -- contrato já implementado (referência, não mexer)

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/pages/CGIBSApuracao.tsx` -- reescrever conforme Boundaries.
- [x] `frontend/src/pages/CGIBSExtrato.tsx` -- nova tela conforme Boundaries.
- [x] `frontend/src/lib/navigation.ts` -- habilitar "Extrato IBS", atualizar `cgibsPaths`.
- [x] `frontend/src/App.tsx` -- registrar rota.

**Acceptance Criteria:**
- Given o backend real (já commitado), when a tela "Importar Movimento" carrega, then lista o histórico de `cgibs_solicitacoes` sem erro de console (sem depender de `data.requests`).
- Given o usuário preenche um período e clica "Solicitar", when o backend responde 409 (já em andamento), then a mensagem mostrada é a do backend, não um erro genérico.
- Given a tela "Extrato IBS", when carregada com um período sem lançamentos, then mostra zeros (não erro/tela quebrada).

## Verification

**Commands:**
- `cd frontend && npx tsc --noEmit` -- sem erro de tipo.
- `npm run lint` (se configurado) -- sem novos erros.

**Manual checks (sem CLI):**
- Rodar `npm run dev`, logar como usuário com empresa que tenha credencial CGIBS de teste, navegar pelas 2 telas, confirmar que não há chamadas quebradas no console (Network tab) mesmo sem `CGIBS_*_URL` configuradas no ambiente local (deve mostrar os erros 503/vazio de forma amigável, não estourar).

## Spec Change Log

**Revisão adversarial (rodada 1, patch, sem loopback):**
- Condição de corrida na paginação de `GET /api/cgibs/operacoes`: cliques rápidos em próxima/anterior podiam fazer uma resposta atrasada de uma página antiga sobrescrever a tabela — corrigido com uma guarda de "última página pedida" (`ultimaPaginaPedidaRef`) que descarta respostas obsoletas.
- A lista de operações em `CGIBSExtrato.tsx` não é filtrada pelo período do seletor (o endpoint `/api/cgibs/operacoes` não suporta isso) — mas os dois blocos (cards de resumo período-filtrado + tabela não filtrada) convivem na mesma tela sob o mesmo seletor, o que pode confundir. Não é um erro de contrato (a spec nunca pediu filtro de período nessa lista), mas foi adicionada uma legenda explícita no card avisando que a lista é independente do período selecionado.
- `npx tsc --noEmit` limpo após o patch.

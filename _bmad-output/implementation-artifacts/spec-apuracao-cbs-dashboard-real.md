---
title: 'Apuração CBS: substituir a tela "Gestão de Créditos e Débitos IBS/CBS" mocada por dados reais da RFB'
type: 'feature'
created: '2026-10-08'
status: 'done'
baseline_commit: 'f561a64'
review_loop_iteration: 1
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-apuracao-mensal-cbs-ibs.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `frontend/src/pages/GestaoCredIBSCBS.tsx` — que é literalmente a **tela inicial do sistema** (`/` redireciona para `/rfb/gestao-creditos`) — é inteiramente mocada: KPIs com números fixos, tabela de lançamentos com dados inventados (misturando IBS+CBS na mesma linha), "Conciliação de Pagamentos" ligada a um SAP fictício, e botões "Sugerir Composição"/"APURAR" sem nenhum backend por trás. O usuário pediu explicitamente a versão real, só da CBS (RFB) — o IBS já tem suas próprias telas reais (`CGIBSExtrato.tsx`, `CGIBSApuracao.tsx`).

**Approach:** o backend real já existe quase todo — `GET /api/rfb/debitos` (`ListarDebitosHandler`) e `GET /api/rfb/creditos/lista` (`ListarCreditosHandler`) já são endpoints reais, paginados, com resumo agregado. Falta só estender `GET /api/rfb/resumo-mensal` (criado na spec de apuração mensal) com os campos que faltam pros KPIs, e reescrever a tela consumindo esses 3 endpoints — removendo as partes sem lastro real (conciliação SAP fictícia, botões de ação sem backend).

## Boundaries & Constraints

**Always:**
- Manter a rota (`/rfb/gestao-creditos`) e o label do menu ("Gestão CBS RFB") — não criar página nova nem mexer em `navigation.ts`/`App.tsx`.
- Título da tela muda para refletir escopo real: **"Apuração CBS — Receita Federal"** (tirar "IBS /" — IBS já tem telas próprias reais, misturar aqui seria enganoso).
- `backend/handlers/rfb_resumo_mensal.go` (`RFBResumoMensalHandler`): adicionar ao `RFBResumoMensal` os campos que faltam pros KPIs (nomes exatos, mapeando pras colunas já existentes em `rfb_resumo`/`rfb_creditos_resumo`, migrations 054/096 — não mudar schema):
  - `total_debitos int` (`rfb_resumo.total_debitos`)
  - `total_creditos int` (`rfb_creditos_resumo.total_creditos`)
  - `valor_cbs_total_debito float64` (`rfb_resumo.valor_cbs_total`)
  - `valor_cbs_total_credito float64` (`rfb_creditos_resumo.valor_cbs_total` — mapeia pro KPI "Crédito Potencial")
  - Os 2 campos já existentes (`valor_cbs_nao_extinto` → "Débitos a Pagar"/débito em aberto, `valor_cbs_extinto_credito` → "Crédito Confirmado") continuam como estão, sem renomear (não quebrar o consumidor já existente, a seção "Apuração Mensal CBS" desta mesma tela).
  - Mesmo padrão de COALESCE/tratamento de linha ausente já usado no handler.
- `frontend/src/pages/GestaoCredIBSCBS.tsx` — reescrita completa:
  - **KPIs** (4 cards): "Crédito Potencial" (`valor_cbs_total_credito`), "Crédito Confirmado" (`valor_cbs_extinto_credito`), "Débitos a Pagar" (`valor_cbs_nao_extinto`), "Saldo Líquido Atual" (`saldo_a_pagar_aproximado`, já existe) — todos do `GET /api/rfb/resumo-mensal?periodo=` já consumido pela seção "Apuração Mensal CBS" existente (reaproveitar o mesmo fetch/estado de período, não duplicar a chamada).
  - **Tabela de lançamentos**: busca `GET /api/rfb/debitos?periodo=&page_size=50` e `GET /api/rfb/creditos/lista?periodo=&page_size=50` (período convertido de `mm/aaaa` pro formato `AAAAMM` que esses 2 endpoints esperam — ver `normalizePeriodoFiltro`/contrato já usado em `RFBDebitos.tsx`/`RFBCreditosCBS.tsx`), mescla as duas listas num único array com uma tag `tipo: 'debito'|'credito'` e ordena por `data_dfe_emissao` desc. Tabs "Todos"/"Créditos"/"Débitos" filtram esse array já carregado (sem nova chamada); tab "Pendências" filtra por `situacao_debito`/`situacao_credito` que não indique conclusão — usar a mesma lógica/palavras-chave de status já usada em `RFBDebitos.tsx` (ler esse arquivo pra copiar a convenção exata, não inventar uma nova).
  - Colunas da tabela: Tipo (badge crédito/débito), Documento (`numero_dfe`||`chave_dfe` truncada), Emitente/Adquirente (CNPJ formatado via `formatCNPJ` de `@/lib/formatFilial`, mostrando quem não é a própria empresa — emitente pro crédito, adquirente pro débito, já que o outro lado é sempre a própria empresa), Valor CBS (`valor_cbs_total`), Situação (texto cru de `situacao_debito`/`situacao_credito`, sem inventar enum), Tipo Apuração (`tipo_apuracao` — corrente/ajuste/extemporâneo, substitui a coluna fictícia "Vinculado"). Remover a coluna de ações (dropdown "Ver detalhes/Confirmar/Vincular") — não existe ação real nenhuma dessas hoje.
  - **Remover inteiramente**: card "Conciliação de Pagamentos" (SAP fictício) e card "Painel Apuração Assistida" com botões "Sugerir Composição"/"Utilizar Créditos Disponíveis"/"APURAR" — nenhum tem backend real. No lugar do segundo, um card simples "Resumo da Apuração" (mesmos 3 números do KPI, formato compacto) + um link real para `/rfb/apuracao` (Importar Movimento, onde o usuário de fato aciona uma solicitação real à RFB) e outro para `/rfb/pagamentos-fornecedores` (Pgtos Fornecedores, já real).
  - Filtro de período da tela principal: substituir o `Select` fixo ("Maio 2024"/"Abril 2024"/"Março 2024") pelos mesmos seletores de mês/ano já usados na seção "Apuração Mensal CBS" (reaproveitar `mesCBS`/`anoCBS`/`mesesLabel`/`anosDisponiveis` já existentes nesse arquivo) — um único seletor de período pra tela inteira, não dois incoerentes.
  - Filtro "Empresa" (mock "Matriz DF"/"Filial SP"): remover — a empresa efetiva já vem do `X-Company-ID`/`CompanySwitcher` global do app (mesmo padrão de toda a aplicação), não é um filtro desta tela.
  - Estados de loading/vazio em todas as seções (mesmo padrão das outras telas RFB/CGIBS já no projeto — `toast.error` em falha de fetch, mensagem amigável se lista vazia).

**Never:** mudar `ListarDebitosHandler`/`ListarCreditosHandler` (já funcionam, só consumir); inventar dado ou ação sem backend real (é exatamente o problema desta spec); mexer nas telas `RFBDebitos.tsx`/`RFBCreditosCBS.tsx`/`PainelApuracaoCBS.tsx` (continuam como telas de detalhe separadas, esta é só a visão geral); tocar em qualquer coisa de IBS/CGIBS nesta spec.

</frozen-after-approval>

## Code Map

- `backend/handlers/rfb_resumo_mensal.go` (`RFBResumoMensalHandler`) — estender resposta
- `backend/handlers/rfb_debitos_lista.go` (`ListarDebitosHandler`), `backend/handlers/rfb_creditos.go` (`ListarCreditosHandler`) — consumir, não alterar
- `frontend/src/pages/GestaoCredIBSCBS.tsx` — reescrita
- `frontend/src/pages/RFBDebitos.tsx` — referência de convenção de status/formatação (ler antes de escrever)
- `frontend/src/lib/formatFilial.ts` (`formatCNPJ`) — reaproveitar

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/rfb_resumo_mensal.go` — campos novos + teste.
- [x] `frontend/src/pages/GestaoCredIBSCBS.tsx` — reescrita conforme Boundaries.

**Acceptance Criteria:**
- Given a tela carregada sem nenhum dado real (empresa nova, sem débitos/créditos CBS), when renderizada, then mostra estados vazios amigáveis em todas as seções, sem erro de console.
- Given débitos e créditos reais no período selecionado, when a tabela carrega, then todos os valores vêm da API (nenhum número fixo/mock restante no arquivo).
- Given o usuário troca o período (mês/ano), when a mudança acontece, then KPIs e tabela atualizam juntos a partir do mesmo seletor.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test -count=1 ./handlers/... -run RFBResumoMensal`
- `cd frontend && npx tsc --noEmit`

**Manual check:** `grep -n "Mock\|fmt(1_280_500\|850_300\|940_700\|90_400" frontend/src/pages/GestaoCredIBSCBS.tsx` deve vir vazio (confirma que nenhum dado mocado sobrou).

## Spec Change Log

**Revisão adversarial (rodada 1, patch, sem loopback):**
- Troca rápida de período (mês/ano) não tinha guarda de sequência — resposta atrasada de um período antigo podia sobrescrever KPIs/tabela do período recém-selecionado (mesma classe de bug já corrigida antes em `CGIBSExtrato.tsx`). Corrigido com refs de "último período pedido" por fetch.
- Tabela de lançamentos busca no máximo 50 débitos + 50 créditos sem paginação, mas não avisava quando o período real tinha mais documentos que isso (KPIs, vindos do resumo agregado, cobrem o período inteiro) — adicionado aviso "mostrando os N mais recentes de M" quando a lista está truncada.
- `go build`/`go vet`/`go test` e `tsc --noEmit` limpos após os patches.

---
title: 'CGIBS/IBS: plano de reconstrução para o modelo real (conta corrente fiscal)'
type: 'feature'
created: '2026-09-28'
status: 'ready-for-dev'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** o módulo CGIBS existente (`102_cgibs_tables.sql`, `cgibs_*.go`) foi montado em março/2026 copiando a estrutura da RFB (débito/crédito por período, "tiquete") — documentação da época estava errada. O manual técnico real da CGIBS (MOC v1.10, set/2026) descreve um modelo completamente diferente: **conta corrente fiscal por operação** (vinculada à chave de acesso do documento fiscal), com lançamentos incrementais carregando 7 valores simultâneos (recurso financeiro disponível/a transferir, crédito a apropriar/não utilizado/utilizado, débito em aberto/extinto). Hoje o botão "Solicitar Apuração IBS" sempre retorna 503 fixo; nenhuma tabela CGIBS é escrita — não há dado real a preservar.

**Approach:** esta spec é só planejamento — mapeia o redesenho necessário e as decisões que precisam do usuário. Implementação vira sub-specs sequenciais (schema → habilitação/webhook → obter arquivo/parser → solicitação/listagem/cancelamento + UI), cada uma um `bmad-quick-dev` separado com revisão adversarial, mesmo padrão usado na migração RFB v1→v2.

## Boundaries & Constraints

**Always:** tratar como greenfield de dados — `102_cgibs_tables.sql` nunca foi escrita por nenhum handler, sem risco de perda; migrations só aditivas (numeração a partir de 131); reaproveitar PADRÕES de código já validados no projeto (OAuth2/HMAC/claim atômico/agendamento do módulo RFB) sem copiar o SCHEMA da RFB; manter `CGIBSPainel.tsx`/`CGIBSDebitos.tsx` (cálculo interno estimado a partir de `nfe_saidas`/`nfe_entradas`/`cte_entradas`, com aviso de estimativa) intactos — não são o problema, continuam válidos em paralelo à integração real.

**Decisões resolvidas (2026-09-28):**
1. **Piloto**: Ferreira Costa NÃO está confirmada no piloto CGIBS (123 empresas, jan-mar/2026) — implementar contra a especificação do MOC, sem validação ponta a ponta por ora. Os riscos técnicos documentados abaixo (autenticação, Obter Arquivo) ficam pendentes até termos acesso real ou uma cópia legível do PDF.
2. **`cgibs_credentials`**: estender com `webhook_url` + `token_contrib` (schema atual é aproveitável).
3. **Tabelas vazias (`cgibs_requests`/`cgibs_debitos`/`cgibs_resumo`)**: dropar e substituir pelo schema de conta corrente fiscal — sem risco, nunca foram escritas.
4. **UI de Créditos/Pagamentos IBS**: decisão adiada para a sub-spec de UI (`spec-cgibs-solicitacao-ui.md`), quando o parser já estiver rodando com dado de exemplo/real.

**Riscos técnicos a validar (não são decisão do usuário, documentar e seguir):**
- **Autenticação real** (OAuth2 Bearer como RFB, ou `ClientID`/`ClientSecret` simples no corpo de cada request) — as seções 4.3/4.4 do MOC (Convenções Gerais/Autenticação) são páginas de diagrama/imagem no PDF, não extraíram como texto (sem `pdftoppm`/OCR neste ambiente). Recomendação: desenhar o cliente HTTP de forma que trocar o mecanismo de auth depois seja um ponto único de mudança (mesmo princípio já usado em `rfb.go`), sem travar a decisão agora.
- **"Obter Arquivo" (5.3)**: request/response não ficaram claros na extração — mesma causa. Confirmar quando tivermos acesso real (piloto) ou uma cópia legível do PDF.
- **Tabela de códigos `MOV`**: o próprio manual da CGIBS diz que ainda não existe ("usar texto de descrição enquanto não existe") — gravar o código bruto + texto livre, sem enum fechado.

**Never:** implementar as 7 APIs sem especificação técnica da própria CGIBS (Consulta de Operação, Consulta de Documento Fiscal, Desabilitação via API, Consulta de Saldo, Emissão/Consulta de Documento de Arrecadação); alterar migrations existentes; tocar no módulo RFB.

</frozen-after-approval>

## Code Map

- `backend/handlers/cgibs_credentials.go`, `cgibs_apuracao.go`, `cgibs_debitos.go` -- estrutura atual (referência do que existe, sem integração real)
- `backend/migrations/102_cgibs_tables.sql` -- schema atual a ser substituído (comentário confirma cópia do padrão RFB)
- `backend/services/rfb.go`, `backend/handlers/rfb_apuracao.go` (`RFBWebhookHandler`), `backend/services/rfb_scheduler.go` -- padrão de código de referência (client HTTP, webhook HMAC, claim atômico, agendamento) — replicar padrão, não schema
- `frontend/src/lib/navigation.ts`, `CGIBSPainel.tsx`, `CGIBSApuracao.tsx`, `CGIBSDebitos.tsx`, `CGIBSCredentials.tsx` -- telas existentes

## Tasks & Acceptance

**Execution:**
- [ ] Resolver as 4 decisões "Ask First" com o usuário -- registrar respostas nesta spec.
- [x] `spec-cgibs-schema.md` (implementação) -- **loopback 1:** 1ª tentativa dropava `cgibs_requests`/`cgibs_debitos`/`cgibs_resumo` na mesma migration; revisão adversarial (2x) achou que `cgibs_apuracao.go`/`limpeza_total.go` ainda leem essas tabelas — dropar quebraria no deploy. Emenda: tabelas novas criadas AO LADO das antigas, sem nenhum DROP; o drop fica pra `spec-cgibs-solicitacao-ui.md` (abaixo), quando esses 2 arquivos forem reescritos. Rodada também incorporou FKs `ON DELETE SET NULL`, CHECKs de enum/consistência, `company_id` direto nas tabelas filhas, `BIGINT` nos IDs externos.
- [x] `spec-cgibs-habilitacao-webhook.md` (implementação) -- cliente HTTP de Habilitação do Contribuinte + endpoint webhook receptor (payload de notificação de arquivo), depende do schema.
- [x] `spec-cgibs-obter-arquivo-parser.md` (implementação) -- Obter Arquivo + parser do `extrato_cc` (header/operacoes/lançamentos) gravando no schema novo; ponto de validação dos riscos técnicos documentados acima.
- [x] `spec-cgibs-nova-solicitacao-drop-legado.md` (implementação, split de `spec-cgibs-solicitacao-ui.md`) -- Nova Solicitação/Cancelamento/Listagem no cliente HTTP; reescreve `cgibs_apuracao.go` (os 4 handlers) e `limpeza_total.go` pro schema novo; endpoints novos de leitura do extrato (operações/lançamentos); SÓ ENTÃO migration nova dropando `cgibs_requests`/`cgibs_debitos`/`cgibs_resumo`. Backend puro, sem frontend.
- [ ] `spec-cgibs-frontend.md` (implementação, split de `spec-cgibs-solicitacao-ui.md`) -- `CGIBSApuracao.tsx` (tira o 503 fixo, fluxo assíncrono tipo RFB), decisão de UI da pergunta 4 (unificar em tela de Extrato/Conta Corrente). Depende da sub-spec de backend acima.

**Acceptance Criteria:**
- Given as 4 sub-specs, when abertas individualmente, then cada uma é auto-suficiente (Code Map + decisões já resolvidas aqui, sem reler esta spec inteira).
- Given a ordem de dependência (schema → habilitação/webhook → obter-arquivo/parser → solicitação/UI), when implementadas fora de ordem, then a de parser falha por depender de colunas que só existem após a de schema.

## Design Notes

Diferença estrutural chave vs RFB: a RFB agrega por `(company_id, data_apuracao)`; o IBS agrega por `(company_id, chave_acesso)` — cada operação é uma "conta corrente" com histórico próprio de lançamentos, não um total por período. Resumos por período (se necessários pra UI) precisam ser calculados via agregação SQL sobre os lançamentos, seguindo o mesmo princípio já validado no fix `2ada5df` da RFB (nunca somar em memória o payload de uma única resposta).

## Verification

**Manual checks (nesta rodada, sem CLI):**
- Confirmar que as 4 perguntas foram respondidas e refletidas nesta spec antes de abrir qualquer sub-spec de implementação.

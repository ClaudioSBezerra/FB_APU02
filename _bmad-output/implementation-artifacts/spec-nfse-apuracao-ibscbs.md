---
title: 'NFS-e na apuração própria (Painel de Apuração) + Malha Fina NFS-e x RFB'
type: 'feature'
created: '2026-10-26'
status: 'done'
review_loop_iteration: 0
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-ibscbs-xml.md']
baseline_commit: '3b367df'
issue: 'https://github.com/ClaudioSBezerra/FB_APU02/issues/14'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** as NFS-e capturadas do ADN (#12/#13) já estão na base própria (`nfse_saidas`/`nfse_entradas`, vindas dos XMLs) com IBS/CBS, mas a apuração própria (Painel de Apuração, `handlers/apuracao_painel.go`) só soma `nfe_saidas`/`nfe_entradas`/`cte_entradas`, e a Malha Fina não tem bloco NFS-e.

**Princípio (usuário, 26/10):** base própria (nossos XMLs) × base RFB (download) são COMPARADAS — nunca misturar. NFS-e NÃO é gravada em `rfb_*`/`cgibs_*`. Decisão (26/10, AskUserQuestion): ler `nfse_*` direto, sem copiar dados.

**Approach:** NFS-e vira 4ª fonte do Painel de Apuração (prestadas = débito, tomadas = crédito); Malha Fina ganha bloco NFS-e: linhas RFB de NFS-e cuja chave NOT EXISTS em `nfse_*` da empresa (mesmo padrão dos blocos NF-e/CT-e).

## Boundaries & Constraints

**Always:**
- Painel de Apuração: incluir `nfse_saidas` (débito) e `nfse_entradas` (crédito) nos períodos (UNION de `mes_ano`) e nas somas, com as colunas da #13: `ibs_uf`, `ibs_mun`, `ibs_total`, `cbs` (NULL = 0 na soma). Excluir `cancelada = true`. Período por `mes_ano` (competência), formato MM/YYYY como as demais fontes. Seguir exatamente a forma como NF-e/CT-e aparecem na resposta (nova seção/linha "NFS-e" ao lado de NF-e/CT-e, entrando nos totais de débito/crédito); expor separado pra tela mostrar a origem.
- Frontend do Painel: exibir a linha/coluna NFS-e no mesmo padrão visual de NF-e/CT-e; tsc com `-p tsconfig.app.json`.
- Malha Fina NFS-e: identificar NFS-e na base RFB pela chave de 50 dígitos (`length(chave_dfe) = 50`) — e, se o código/processador RFB já usar um `modelo_dfe` específico pra NFS-e, aceitar também. Saídas: `rfb_debitos` sem chave em `nfse_saidas`; entradas: `rfb_creditos` sem chave em `nfse_entradas`. Endpoints e resumo no mesmo padrão de `/api/malha-fina/nfe-*` e `/cte` (handlers + MV `mv_malha_fina_resumo` recriada em migration NOVA incluindo os blocos `nfse-saidas`/`nfse-entradas`, mantendo os 3 blocos atuais idênticos à migration 125). Frontend da Malha Fina: aba/bloco NFS-e no padrão existente.
- Conferir onde a MV é atualizada (REFRESH) e garantir que os novos blocos entram no mesmo refresh.

**Ask First:** nenhuma.

**Never:** gravar NFS-e em `rfb_debitos`/`rfb_creditos`/`rfb_resumo`/`cgibs_*`; alterar a apuração RFB/CGIBS; editar migrations existentes; mudar a lógica dos blocos NF-e/CT-e.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected |
|----------|--------------|----------|
| Nota 30 prestada 10/2026 | cbs 209.52, ibs 23.28 | Painel 10/2026: débito NFS-e CBS 209.52, IBS 23.28, somado ao total de débitos |
| Nota tomada | cbs 100 | entra no crédito NFS-e |
| Nota cancelada | cancelada=true | fora das somas |
| Sem grupo IBSCBS | NULL | soma 0, nota não quebra |
| RFB tem NFS-e que não temos | rfb_debitos chave 50 ausente em nfse_saidas | aparece na Malha Fina NFS-e saídas |
| RFB e nós temos a mesma | chave presente | não aparece na Malha Fina |
| Chave 44 (NF-e) na RFB | — | continua só nos blocos NF-e |

</frozen-after-approval>

## Code Map

- `backend/handlers/apuracao_painel.go` + teste
- `backend/handlers/malha_fina*.go` (descobrir arquivo) + teste
- `backend/migrations/136_mv_malha_fina_nfse.sql` (novo)
- `backend/main.go` -- rotas malha-fina NFS-e
- `frontend/src/pages/` -- Painel de Apuração e Malha Fina (descobrir arquivos; `MalhaFinaPanel.tsx`)

## Tasks & Acceptance

**Execution:**
- [x] Painel: NFS-e como 4ª fonte (backend + frontend)
- [x] Malha Fina NFS-e: migration MV, handlers, rotas, frontend
- [x] testes da matriz

**Acceptance Criteria:**
- Given a nota 30, when o Painel de Apuração de 10/2026 é aberto, then aparece NFS-e com débito CBS R$ 209,52 e IBS R$ 23,28 somados aos débitos.
- Given uma NFS-e na RFB sem XML nosso, when a Malha Fina é aberta, then ela aparece no bloco NFS-e.

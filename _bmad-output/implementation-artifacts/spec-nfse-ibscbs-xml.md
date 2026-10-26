---
title: 'NFS-e: capturar IBS/CBS e base/alíquota do ISS do XML + reprocessar notas gravadas'
type: 'feature'
created: '2026-10-26'
status: 'done'
review_loop_iteration: 0
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-adn-parser-real.md']
baseline_commit: '25a4e7203f00483ebfdb149a156d64fe144edc24'
issue: 'https://github.com/ClaudioSBezerra/FB_APU02/issues/13'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** a captura (#12) funciona, mas as telas NFS-e Prestadas/Tomadas não mostram IBS nem CBS — o parser não lê o grupo `IBSCBS` e as tabelas não têm colunas pra isso. `base_iss`/`aliquota_iss` também ficam zerados embora venham no XML. O aviso "Valores tributários ainda não confirmados" ficou obsoleto.

**Approach:** com um XML real (layout 1.01, nota 30 da Fortes Bezerra, 01/10/2026) como fixture, extrair os campos IBS/CBS e ISS, gravar em colunas novas, reprocessar as notas já gravadas a partir de `xml_gzip_b64` e exibir nas telas.

## Boundaries & Constraints

**Always:**
- Caminhos (nomes locais, namespace `http://www.sped.fazenda.gov.br/nfse`):
  - `infNFSe/valores/vBC`→`base_iss`, `infNFSe/valores/pAliqAplic`→`aliquota_iss`.
  - `infNFSe/IBSCBS/cLocalidadeIncid`→`ibscbs_cod_localidade_incid`; `IBSCBS/valores/vBC`→`ibscbs_base`; `valores/uf/pIBSUF`→`ibs_uf_aliquota`, `valores/uf/pAliqEfetUF`→`ibs_uf_aliquota_efetiva`; `valores/mun/pIBSMun`→`ibs_mun_aliquota`, `pAliqEfetMun`→`ibs_mun_aliquota_efetiva`; `valores/fed/pCBS`→`cbs_aliquota`, `pAliqEfetCBS`→`cbs_aliquota_efetiva`; `totCIBS/vTotNF`→`ibscbs_valor_total_nf`; `totCIBS/gIBS/vIBSTot`→`ibs_total`; `gIBS/gIBSUFTot/vIBSUF`→`ibs_uf`; `gIBS/gIBSMunTot/vIBSMun`→`ibs_mun`; `totCIBS/gCBS/vCBS`→`cbs`.
  - `infNFSe/DPS/infDPS/IBSCBS/{finNFSe, cIndOp, indDest}` e `IBSCBS/valores/trib/gIBSCBS/{CST, cClassTrib}` → `ibscbs_fin_nfse`, `ibscbs_ind_op`, `ibscbs_ind_dest`, `ibscbs_cst`, `ibscbs_cclass_trib`.
- Migration 135 aditiva (`ADD COLUMN IF NOT EXISTS`) nas duas tabelas `nfse_entradas`/`nfse_saidas`: colunas acima como `NUMERIC(15,2)` (valores), `NUMERIC(7,4)` (alíquotas), `VARCHAR` (códigos), todas NULL = "grupo ausente no XML" (distinto de 0); mais `xml_parser_versao SMALLINT NOT NULL DEFAULT 0`.
- Mesmo parsing robusto do #12 (decimal com vírgula, valor inválido logado e contado). Grupo `IBSCBS` ausente = colunas NULL, nunca erro.
- Captura nova grava os campos e `xml_parser_versao = 2`. Reprocessamento: ao iniciar o scheduler NFS-e, em lotes pequenos (ex.: 200 linhas por transação), reextrai de `xml_gzip_b64` as linhas com `xml_parser_versao < 2`, atualiza só os campos extraídos do XML (nunca `cancelada`/`data_cancelamento`/`nsu`) e marca versão 2; falha numa linha loga e marca versão 2 pra não repetir em loop. Roda em goroutine, não bloqueia o boot.
- `nfse_list.go`: expõe os campos novos na listagem e adiciona totais `total_ibs`, `total_cbs` (excluindo canceladas, como os demais).
- Frontend `ConsultaNFSeSaidas.tsx` e `ConsultaNFSeEntradas.tsx`: remover o alerta "Valores tributários ainda não confirmados"; cards de total IBS e CBS; colunas IBS e CBS na tabela; no detalhe da nota, bloco IBS/CBS (base, alíquotas UF/Mun/CBS, valores, CST, cClassTrib, local de incidência). Valor NULL exibe "—".

**Ask First:** nenhuma.

**Never:** ligar NFS-e à apuração de crédito/débito CBS/IBS (escopo futuro); grupos de crédito presumido / tributação regular / diferimento (`gIBSCredPres`, `gTribRegular`, `vDif*`) — não existem no XML de amostra, ficam pra quando aparecerem; PIS/COFINS retido x apurado (adiado); alterar migrations existentes.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| XML com IBSCBS (fixture real) | nota 30 | base 23280.00, IBS UF 23.28, IBS Mun 0.00, CBS 209.52, CST 000, cClassTrib 000001, base ISS 24000, alíq. 3.00 | — |
| XML sem IBSCBS | nota antiga | colunas IBS/CBS NULL; ISS preenchido | — |
| Linha antiga já gravada | `xml_parser_versao=0` | reprocessada no boot, versão 2, cancelada preservada | falha → log, versão 2 |
| Valor inválido | `vCBS` = "abc" | NULL + log com NSU + contador | — |

</frozen-after-approval>

## Code Map

- `backend/services/nfse_adn_xml.go` -- structs/extração IBSCBS e vBC/pAliqAplic
- `backend/services/nfse_processor.go` -- INSERT/UPSERT com colunas novas e `xml_parser_versao`
- `backend/services/nfse_reprocessar.go` (novo) -- reprocessamento em lotes
- `backend/services/nfse_scheduler.go` -- dispara o reprocessamento uma vez no start
- `backend/migrations/135_nfse_ibscbs.sql` (novo)
- `backend/handlers/nfse_list.go` -- campos + totais
- `frontend/src/pages/ConsultaNFSeSaidas.tsx`, `ConsultaNFSeEntradas.tsx`

## Tasks & Acceptance

**Execution:**
- [x] fixture com o XML real (sem assinatura) + testes de extração
- [x] migration 135
- [x] parser + processor gravando campos e versão
- [x] reprocessamento + teste (preserva cancelada)
- [x] list handler + testes
- [x] frontend (tsc com `-p tsconfig.app.json`)

**Acceptance Criteria:**
- Given a nota 30 já gravada, when o backend sobe, then a tela Prestadas mostra IBS R$ 23,28 e CBS R$ 209,52 e base/alíquota ISS preenchidas.
- Given uma nota nova capturada, when gravada, then já sai com IBS/CBS e `xml_parser_versao=2`.
- Given notas canceladas, when totais calculados, then IBS/CBS delas não somam.

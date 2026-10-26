---
title: 'NFS-e: parser real do ADN (LoteDFe), cursor NSU, XML e cancelamento'
type: 'bugfix'
created: '2026-10-26'
status: 'done'
review_loop_iteration: 0
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-cadeia-icp-brasil.md']
baseline_commit: 'dc9ffeece2dda164e8ece49e45856e3dc5958800'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** desde o #11 o mTLS com o ADN funciona, mas "Buscar Agora" retorna "0 recebidos": `parseNFSeADNPayload` foi escrito com um formato suposto (`documentos`/`loteVazio`/`ultimoNSU`) que não existe — o JSON real decodifica pra struct vazia sem erro. Também não há cursor persistido (toda busca recomeçaria do NSU 0, sob rate limit agressivo), nem extração dos valores do XML, nem tratamento de cancelamento.

**Approach:** implementar o contrato real (cópia do Swagger oficial em `scratchpad/adn.json`; teste de produção de terceiros), percorrer a fila por NSU a partir do último NSU salvo, extrair do XML os campos fiscais, guardar o XML e marcar notas canceladas. Decisões do usuário (26/10): gravar TODO o histórico (o ADN não filtra por data), guardar o XML, marcar cancelamento.

## Boundaries & Constraints

**Always:**
- Requisição `GET {base}/contribuintes/DFe/{NSU}?lote=true`. Resposta JSON PascalCase `LoteDistribuicaoNSUResponse` {`StatusProcessamento`, `LoteDFe[]`, `Alertas[]`, `Erros[]`, `TipoAmbiente`, `VersaoAplicativo`, `DataHoraProcessamento`}, item `DistribuicaoNSU` {`NSU` int64, `ChaveAcesso`, `TipoDocumento`, `TipoEvento`, `ArquivoXml`, `DataHoraGeracao`}, mensagens {`Codigo`, `Descricao`, `Complemento`, `Mensagem`, `Parametros`}. Parser continua isolado em `services/nfse_adn.go`.
- HTTP 200 `DOCUMENTOS_LOCALIZADOS` = lote; HTTP 404 com `NENHUM_DOCUMENTO_LOCALIZADO` = fim da fila (sucesso, não erro); HTTP 400 `REJEICAO` = erro com códigos de `Erros` no log (`%q`); 429 e 5xx = transitório: respeitar `Retry-After` se presente, backoff com poucas tentativas, depois encerrar o ciclo salvando o progresso. Pausa curta entre lotes (≈1s) pra não provocar 429.
- Cursor: próximo NSU = maior `NSU` do lote. Coluna nova `nfse_certificados.ultimo_nsu BIGINT NOT NULL DEFAULT 0` (migration 134, aditiva). Cada lote é gravado em UMA transação junto com a atualização de `ultimo_nsu` — interrupção no meio nunca perde nem duplica. Ao atingir o limite de lotes/tempo do ciclo, encerra com sucesso parcial (o próximo ciclo continua), não erro.
- `ArquivoXml` = base64 de gzip: decodificar com limite de tamanho descomprimido (5 MiB por documento, anti zip-bomb); XML namespace `http://www.sped.fazenda.gov.br/nfse`, raiz `NFSe/infNFSe`.
- `TipoDocumento=NFSE`: extrair `ChaveAcesso`→`identificador_nfse`; `infNFSe/nNFSe`; `infNFSe/emit/CNPJ`; `infNFSe/DPS/infDPS/{dhEmi→data_emissao, dCompet→mes_ano (competência, MM/YYYY), prest/CNPJ, toma/CNPJ, valores/vServPrest/vServ→valor_servicos}`; `infNFSe/valores/vISSQN`→`iss` (e `iss_retido`=vISSQN quando `valores/trib/tribMun/tpRetISSQN` ∈ {2,3}); `valores/trib/tribFed/piscofins/vPis`→`pis`, `vCofins`→`cofins`, `tribFed/vRetIRRF`→`irrf`, `vRetCSLL`→`csll`, `vRetCP`→`inss`. Campo ausente = 0, nunca erro. Direção: a empresa é prestadora se a raiz (8 dígitos) de `emit/CNPJ`/`prest/CNPJ` = raiz do CNPJ do certificado → `nfse_saidas`; tomadora se raiz de `toma/CNPJ` = raiz → `nfse_entradas`; ambos → as duas (padrão já existente); nenhum → ignorado.
- Guardar o `ArquivoXml` como recebido (gzip+base64) em coluna nova `xml_gzip_b64 TEXT` nas duas tabelas, mais `nsu BIGINT`, `numero_nfse VARCHAR(20)`, `cancelada BOOLEAN NOT NULL DEFAULT false`, `data_cancelamento TIMESTAMPTZ` (migration 134).
- `TipoDocumento=EVENTO` com `TipoEvento` da família cancelamento (`CANCELAMENTO`, `CANCELAMENTO_POR_SUBSTITUICAO`, `CANCELAMENTO_POR_OFICIO` e afins conforme enum do Swagger): marcar `cancelada=true`/`data_cancelamento` na(s) nota(s) da mesma `ChaveAcesso` da empresa; se a nota ainda não existe, só logar. Demais tipos (`DPS`, `CNC`, `PEDIDO_REGISTRO_EVENTO`, outros eventos) = ignorados (contados).
- Log de diagnóstico por lote: status, quantidade, faixa de NSU, `TipoAmbiente`, códigos de `Alertas`/`Erros` — nunca o XML.
- As telas/endpoints de listagem e totais (`nfse_list.go`) excluem notas canceladas dos totais.

**Ask First:** nenhuma — decisões tomadas pelo usuário em 26/10/2026.

**Never:** consultar por data (não existe na API); gravar XML descomprimido; reprocessar retroativamente o que já existe; mudar a lógica de certificado/mTLS (#10/#11); ligar NFS-e à apuração de crédito CBS/IBS.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Fila com documentos | 200, 50 itens, depois 404 | Grava, `ultimo_nsu` = maior NSU, encerra no 404 | — |
| Fila vazia | 404 `NENHUM_DOCUMENTO_LOCALIZADO` | 0 recebidos, sucesso | — |
| Rejeição | 400 `REJEICAO` com `Erros` | Erro mapeado (502 manual), códigos no log | `%q` |
| Rate limit | 429 com/sem `Retry-After` | Backoff; esgotado, salva progresso e encerra | sucesso parcial |
| Evento de cancelamento | `EVENTO`/`CANCELAMENTO` | Nota marcada cancelada | nota inexistente → log |
| XML corrompido/gigante | gzip inválido ou > 5 MiB | Documento ignorado, lote segue | log com NSU |
| Interrupção no meio | erro no lote 3 | Lotes 1–2 gravados, `ultimo_nsu` do lote 2 | — |

</frozen-after-approval>

## Code Map

- `backend/services/nfse_adn.go` -- substituir `NFSeADNDocumento`/`NFSeADNResponse`/`parseNFSeADNPayload`/`BuscarDFeADN`/`BuscarTodosDocumentosADN` pelo contrato real; parser de XML (encoding/xml, nomes locais) no mesmo arquivo ou `nfse_adn_xml.go`
- `backend/services/nfse_processor.go` -- `ExecutarCicloNFSe`, `persistirDocumentosNFSe`, `inserirNFSeDocumento`: ler/gravar `ultimo_nsu`, gravar por lote em transação, eventos de cancelamento
- `backend/migrations/134_nfse_cursor_xml_cancelamento.sql` (novo) -- colunas novas (aditivas)
- `backend/handlers/nfse_list.go` -- totais excluem `cancelada`; listagem expõe `cancelada`/`numero_nfse`
- `scratchpad/adn.json` -- Swagger de referência (`/tmp/claude-1000/-home-claudio-projetos-FB-APU02/6f20dcb4-4ede-451b-92a7-72367c193e87/scratchpad/adn.json`)

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/134_nfse_cursor_xml_cancelamento.sql` -- colunas da Boundaries.
- [x] `backend/services/nfse_adn.go` (+ arquivo de XML se preferir) -- tipos e parser reais, paginação, 404/400/429/5xx, gzip+base64 com limite, extração XML.
- [x] `backend/services/nfse_processor.go` -- cursor, transação por lote, direção por raiz de CNPJ, cancelamento, contagens.
- [x] `backend/handlers/nfse_list.go` -- excluir canceladas dos totais, expor campos.
- [x] Testes: fixtures JSON no formato real + XML de NFS-e realista (gzip+base64 gerado no teste) servidos por `httptest` mTLS; cobrir a I/O Matrix, direção (saída/entrada/ambas/nenhuma por raiz), extração de cada campo fiscal, cursor persistido e retomada.

**Acceptance Criteria:**
- Given a fila do ADN com 120 NFS-e, when "Buscar Agora" roda, then 120 são processadas em 3 lotes e `ultimo_nsu` = maior NSU. — `TestExecutarCicloNFSe_120Documentos_CursorPersistidoERetomada`, `TestNFSePercorrerFilaADN_120Documentos_3LotesAteFim`
- Given `ultimo_nsu` = 120 salvo, when roda de novo, then a primeira chamada é `/DFe/120?lote=true`. — `TestExecutarCicloNFSe_120Documentos_CursorPersistidoERetomada` (2º ciclo), `TestExecutarCicloNFSe_429Esgotado_SucessoParcialComProgresso`
- Given uma NFS-e tomada com PIS/COFINS/IRRF/CSLL/INSS/ISS retido no XML, when gravada, then as colunas trazem esses valores. — `TestExecutarCicloNFSe_DirecaoCamposFiscaisEIgnorados`, `TestNFSeExtrairDadosNFSeXML_TodosOsCampos`, `TestNFSeExtrairDadosNFSeXML_TpRetISSQN`
- Given uma nota cancelada, when listada, then aparece como cancelada e fora dos totais. — `TestExecutarCicloNFSe_Cancelamento`, `TestNFSeEntradasListHandler_CanceladaListadaMasForaDosTotais`

**I/O Matrix → testes:** fila com documentos/fila vazia — `TestExecutarCicloNFSe_120Documentos_*`, `TestExecutarCicloNFSe_FilaVazia_*`, `TestNFSeBuscarLoteDFeADN_404NenhumDocumento_*`; rejeição — `TestNFSeBuscarLoteDFeADN_400Rejeicao_*`, `TestMapExecutarCicloNFSeErr_RejeicaoETransitorio_Retornam502`; rate limit — `TestNFSePercorrerFilaADN_429ComRetryAfter_*`, `_Backoff_TetoEExponencial`, `_429Esgotado_SucessoParcial`, `TestExecutarCicloNFSe_429Esgotado_*`; cancelamento — `TestExecutarCicloNFSe_Cancelamento`; XML corrompido/gigante — `TestExecutarCicloNFSe_DirecaoCamposFiscaisEIgnorados`, `TestNFSeDecodificarArquivoXmlADN_ZipBomb_Rejeitado`, `_Invalidos`; interrupção — `TestExecutarCicloNFSe_ErroNoLote3_PreservaLotesAnteriores`, `TestNFSePercorrerFilaADN_ErroNoLote3_*`.

**Notas de implementação:** conflito (`company_id, identificador_nfse`) faz DO UPDATE dos campos extraídos/XML/NSU/número, nunca de `cancelada`/`data_cancelamento`; `novos` conta só inserções (`RETURNING xmax = 0`). Família cancelamento = `CANCELAMENTO`, `CANCELAMENTO_POR_SUBSTITUICAO`, `CANCELAMENTO_DEFERIDO_ANALISE_FISCAL`, `CANCELAMENTO_POR_OFICIO` (fora: `SOLICITACAO_CANCELAMENTO_ANALISE_FISCAL`, `CANCELAMENTO_INDEFERIDO_ANALISE_FISCAL`). `ErrNFSeADNLimiteLotes` removido (limite de lotes/tempo = sucesso parcial); novos sentinelas `ErrNFSeADNRejeicao`, `ErrNFSeADNTransitorio` (502 no handler).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: tudo passa

**Manual checks (se CLI não cobrir):**
- Após deploy, "Buscar Agora" na empresa de teste: log de diagnóstico mostra o status real e a contagem; notas aparecem em Notas Importadas → NFS-e.

## Change Log

- 2026-10-26 — Revisão adversarial (Blind Hunter + Edge Case Hunter), 1 rodada de patch:
  - SAVEPOINT por documento; tabela de quarentena `nfse_adn_falhas` (falha de decode, enum desconhecido/numérico, erro de banco) gravada na mesma transação do cursor.
  - Cancelamento: notas antes de eventos no lote; `nfse_cancelamentos_pendentes` aplicada quando a nota chega; herança de `cancelada` entre entradas/saídas.
  - Upsert só atualiza quando `EXCLUDED.nsu >= nsu` gravado.
  - Parser: ISO-8859-1/windows-1252, base64 tolerante, decimal com vírgula, dhEmi com offset sem dois-pontos; corpo acima do limite vira erro explícito.
  - Erros de rede (timeout/reset/DNS/EOF) classificados como transitórios.
  - Ciclo devolve contagens + `Parcial()`/motivo; handler 200 parcial / 502 sem progresso / 409 em andamento; agendador só marca o dia quando a fila esgota (retry de erro após 15 min); log `cursor_sem_avanco`.
  - Manual 4 min (WriteTimeout 300s), agendador 5 min; trava por empresa in-process.
  - `nfse_certificados.cnpj_titular` + reset do cursor quando o CNPJ do certificado muda.
  - Frontend: toast mostra atualizados/falhas e avisa busca parcial.
  - Adiados registrados em deferred-work.md (float64 monetário, base/alíquota, PIS/COFINS retido, regra raiz CNPJ, data do evento, xmax, captura assíncrona, reprocessar falhas).

## Suggested Review Order

1. `backend/migrations/134_nfse_cursor_xml_cancelamento.sql`
2. `backend/services/nfse_adn.go` (contrato, retry, encerramento)
3. `backend/services/nfse_adn_xml.go` (decode e extração)
4. `backend/services/nfse_processor.go` (savepoint, quarentena, cancelamento, upsert)
5. `backend/services/nfse_scheduler.go`, `backend/handlers/nfse_busca.go`, `backend/handlers/nfse_credentials.go`
6. `frontend/src/pages/NFSeCertificado.tsx`

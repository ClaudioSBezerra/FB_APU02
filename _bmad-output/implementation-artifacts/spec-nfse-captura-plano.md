---
title: 'NFS-e: plano de captura automatizada (tomadas e prestadas) para apuração de crédito CBS/IBS'
type: 'feature'
created: '2026-10-23'
status: 'ready-for-dev'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** NFS-e (tomadas e prestadas) é 100% placeholder no FB_APU02 hoje — abas `enabled: false`/`disabled: true` em `DFesModulo.tsx`, `NotasImportadasModulo.tsx`, `MalhaFinaModulo.tsx`, `AppSidebar.tsx`, status `'empty'` em `Painel.tsx`. Não existe captação automática nem no SAP (confirmado pelo usuário: "volume é enorme", "hoje é manual"). Sem o XML da NFS-e não dá pra apurar crédito de PIS/COFINS/ISS de serviços tomados, que vai alimentar CBS/IBS a partir de 2027.

**Approach:** esta spec é só planejamento — mapeia o redesenho necessário e as decisões que precisam do usuário, nos moldes da spec-mãe do CGIBS (`spec-cgibs-conta-corrente-plano.md`). Implementação vira sub-specs sequenciais (armazenamento de certificado → cliente mTLS + ADN Nacional → schema/persistência → agendamento → telas), cada uma um `bmad-quick-dev` separado com revisão adversarial.

## Boundaries & Constraints

**Always:** reaproveitar PADRÕES de código já validados (`rfb.go` cliente HTTP, `rfb_scheduler.go` agendamento, `crypto.EncryptField`/`DecryptField` para o segredo do certificado) sem copiar schema de nenhum módulo existente; migrations só aditivas (numeração a partir de 133); NÃO copiar nenhum trecho de código do repositório de referência `github.com/Rammeta/cont-ai` (sem LICENSE = todos os direitos reservados) — usar só como referência de arquitetura (ADN Nacional REST por NSU; SP SOAP `PedidoConsultaNFePeriodo`); manter `pagamentos_fornecedores.go` (campo `tipo_doc=NFSE` de import manual CSV) intacto — módulo separado, não é o problema.

**Decisões resolvidas (2026-10-23):**
1. **Município**: fase 1 cobre só o Portal Nacional ADN (municípios aderidos ao padrão nacional). Municípios com layout próprio (ex. São Paulo, SOAP `PedidoConsultaNFePeriodo`) ficam para uma sub-spec de extensão posterior (`spec-nfse-municipio-sp.md`), condicionada a levantamento de onde a FC de fato toma/presta serviço.
2. **Armazenamento do certificado A1**: mesmo mecanismo já validado no projeto — `crypto.EncryptField`/`DecryptField` (AES-256-GCM), `.pfx` binário em base64 + senha, ambos criptografados, coluna `TEXT` em tabela nova `nfse_certificados`. Nenhum armazenamento novo fora do banco.
3. **Ligação com apuração**: nesta fase, os valores capturados (PIS/COFINS/ISS/retenções) ficam só em captura + visualização — não somam automaticamente no saldo a pagar de CBS/IBS (`apuracao_calendario.go`/`rfb_resumo_mensal.go`). Mesma cautela já aplicada à lacuna de tributos legados NF-e/CT-e; ligação automática fica para decisão futura, só depois de validar os dados capturados.
4. **Piloto**: CNPJ 38.149.716/0001-28 confirmado como empresa-piloto. **Alerta operacional**: mesmo CNPJ já envolvido em incidente de config de teste esquecida na RFB (`project_rfb_debito_cnpj_duplicado_master`) — checar que não há credencial/config de teste antiga residual antes de habilitar.

**Never:** implementar DANFSE em PDF nesta fase (exibição, não essencial pra crédito); tocar em CT-e/NFC-e (já cobertos); tratar "notas de débito/crédito" como parte desta spec — não confirmado com o usuário o que significa; se for NF-e complementar/ajuste, é fluxo do pipeline de NF-e normal, fora de escopo aqui.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Certificado A1 vencido | Agendador tenta mTLS com cert expirado | Captura falha, marcada como erro na empresa | Alerta visual na tela de credenciais, análogo a `rfb_credentials` |
| Município fora do ADN Nacional | Empresa só opera em município com layout próprio | Captura ADN retorna vazio/404 pra esse CNPJ | Documentar como limitação conhecida, não erro silencioso |
| Nova Solicitação concorrente | Mesma empresa consulta NSU 2x simultaneamente | Segunda chamada não duplica NFS-e já gravada | `UNIQUE` em (company_id, chave/identificador NFS-e) |

</frozen-after-approval>

## Code Map

- `frontend/src/pages/DFesModulo.tsx:26`, `NotasImportadasModulo.tsx:22`, `MalhaFinaModulo.tsx:19` -- abas NFS-e hoje `enabled: false`, candidatas a ativar
- `frontend/src/components/AppSidebar.tsx:105,143-144` -- itens "NFS-e Entradas/Saídas" hoje `disabled: true`
- `frontend/src/pages/Painel.tsx:33` -- card NFS-e hoje status `'empty'`
- `backend/handlers/pagamentos_fornecedores.go` -- import manual CSV com `tipo_doc=NFSE`; módulo separado, não mexer
- `backend/crypto/crypto.go` -- `EncryptField`/`DecryptField` (AES-256-GCM via `ENCRYPTION_KEY`), reaproveitar para certificado A1 e senha
- `backend/migrations/051_create_rfb_credentials.sql`, `115_sap_credentials.sql` -- padrão de tabela de credencial por empresa com campo criptografado
- `backend/migrations/103_nfe_tributos_icms_pis_cofins.sql` -- padrão de colunas tributárias (`base_X`/`X` por tributo) a espelhar em `nfse_entradas`/`nfse_saidas`
- `backend/services/rfb.go` -- cliente HTTP de referência (aqui a troca é Bearer→mTLS com `tls.Config.Certificates` carregado do cert decriptado por empresa)
- `backend/services/rfb_scheduler.go:298` (`StartRFBScheduler`) -- padrão de agendamento diário por empresa a replicar
- `backend/handlers/rfb_resumo_mensal.go:55`, `backend/services/apuracao_calendario.go` -- ponto de possível ligação futura com saldo CBS/IBS (fora de escopo nesta fase, ver Ask First #3)
- `backend/migrations/132_cgibs_drop_legado.sql` -- última migration; próxima numeração a partir de 133
- Última migration confirmada: `132` -- próxima disponível: `133`

## Tasks & Acceptance

**Execution:**
- [x] Resolver as 4 decisões "Ask First" com o usuário — registradas no bloco "Decisões resolvidas (2026-10-23)" acima.
- [ ] `spec-nfse-certificado-schema.md` (implementação) -- tabela `nfse_certificados` (cert .pfx + senha criptografados, validade, status) + tabelas `nfse_entradas`/`nfse_saidas` (espelhando padrão de `nfe_entradas`/`nfe_saidas` + colunas de retenção ISS/PIS/COFINS/INSS/IRRF/CSLL) + tela de cadastro/alerta de validade de certificado.
- [ ] `spec-nfse-cliente-adn.md` (implementação) -- cliente HTTP mTLS contra Portal Nacional ADN (REST por NSU), depende do schema de certificado.
- [ ] `spec-nfse-agendamento-persistencia.md` (implementação) -- agendador diário por empresa (análogo a `rfb_scheduler.go`) + persistência com dedup.
- [ ] `spec-nfse-frontend.md` (implementação) -- ativa as abas hoje desabilitadas, telas de consulta de NFS-e tomadas/prestadas.
- [ ] `spec-nfse-municipio-sp.md` (implementação, condicional a levantamento de municípios) -- extensão SOAP para municípios fora do padrão ADN Nacional.

**Acceptance Criteria:**
- Given as sub-specs listadas, when abertas individualmente, then cada uma é auto-suficiente (Code Map + decisões já resolvidas aqui, sem reler esta spec inteira).
- Given a ordem de dependência (certificado/schema → cliente ADN → agendamento/persistência → frontend), when implementadas fora de ordem, then a de agendamento falha por depender do cliente HTTP que só existe após a sub-spec anterior.

## Design Notes

Diferença estrutural chave vs. RFB/CGIBS: a autenticação aqui não é OAuth2/Bearer nem ClientID/ClientSecret em corpo de request — é **mTLS com certificado digital A1 por empresa**, carregado em `tls.Config.Certificates` a cada chamada (ou cacheado em memória por company_id com invalidação por troca de certificado). Isso exige descriptografar o `.pfx` (protegido por senha própria, também criptografada) em runtime — ponto de atenção de segurança que não existe em nenhum módulo atual do projeto.

## Verification

**Manual checks (nesta rodada, sem CLI):**
- As 4 decisões já estão resolvidas e refletidas nesta spec (ver "Decisões resolvidas"). Antes de abrir `spec-nfse-certificado-schema.md`, só falta o levantamento de municípios citado na decisão 1 (não bloqueia o schema, bloqueia a decisão de incluir SP depois).

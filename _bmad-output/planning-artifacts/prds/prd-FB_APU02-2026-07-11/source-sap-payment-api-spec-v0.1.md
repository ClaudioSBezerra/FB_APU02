# Fonte — Especificação Técnica e Funcional: API de Consulta de Pagamentos por Chave de DF-e

> Transcrição do documento PDF fornecido pelo usuário em 2026-07-11 (via anexo na conversa), preservada aqui para fins de reconciliação e rastreabilidade do PRD. Sistema: SAP S/4HANA Cloud Private Edition (RISE). Versão 0.1 (rascunho para revisão), data 09/07/2026, status "Em elaboração — pendente validação do time funcional FI e Basis". Solicitante: Projeto Reforma Tributária / Conciliação Apuração Assistida.

## 1. Contexto e objetivo
LC 214/2025 condiciona apropriação dos créditos de CBS/IBS à liquidação financeira das operações (art. 27). Na apuração assistida, RFB/CGIBS pré-calculam débitos/créditos a partir dos DF-e, cabendo ao contribuinte validar/ajustar/contestar. A área fiscal precisa confrontar os créditos apresentados pela RFB com os pagamentos efetivamente realizados aos fornecedores no SAP S/4HANA. Documento especifica API no S/4HANA que, dada a chave de um DF-e, retorna dados de pagamento correspondentes. API consumida pelo motor de conciliação (sistema externo ao SAP — o FB_APU02), que orquestra consultas a partir da relação de documentos retornada pela API do Portal Nacional da RTC (RFB).

## 2. Escopo
**Incluído:** consulta individual por chave (44 dígitos NF-e/CT-e, 50 NFS-e); consulta em lote (até 5000 chaves/requisição) para conciliação diária; suporte NF-e (55) e CT-e (57) via Nota Fiscal Writer, NFS-e nacional via fallback; pagamentos totais e parciais com status consolidado; OData V4 (RAP) com OAuth 2.0.
**Excluído:** gravação/alteração no S/4HANA (somente leitura); consulta à API da RFB (responsabilidade do motor de conciliação); cálculo/validação de valores CBS/IBS; empresas (BUKRS) fora do escopo do projeto (lista a definir na configuração).

## 3. Visão geral da solução
RAP (ABAP RESTful Application Programming Model): camada de dados (CDS views resolvendo DF-e → documento contábil → partidas compensadas, ponte J_1BNFE_ACTIVE/J_1BNFLIN → RBKP → BKPF → BSAK); camada de serviço (Service Definition + Service Binding OData V4, entidade somente leitura + function import para lote); segurança (Communication Arrangement OAuth2 client credentials ou X.509, usuário de comunicação somente leitura restrito por BUKRS); exposição externa conforme padrão RISE (Cloud Connector/WAF/API Management, a definir com Basis/Segurança).

## 4. Endpoints
**4.1 Individual:** GET `.../PaymentByDFe(dfeKey='{chave}')`, parâmetro dfeKey (44 ou 50 dígitos, obrigatório), retorno objeto único, idempotente.
**4.2 Lote:** POST `.../SearchPayments`, corpo `{"dfeKeys": [...]}` **até 5000 chaves por requisição**, retorno array na mesma ordem de envio, chaves duplicadas deduplicadas.

## 5. Contrato de dados
**5.1 Objeto resposta:** dfeKey, dfeType (NFE|CTE|NFSE|DESCONHECIDO), matchType (CHAVE|FALLBACK|NAO_LOCALIZADO), companyCode (BUKRS), supplierCNPJ, supplierId (LIFNR), invoiceDocument (BELNR), invoiceFiscalYear (GJAHR), invoiceDate (BLDAT), invoiceAmount, currency (WAERS), paymentStatus (PAGO_TOTAL|PAGO_PARCIAL|EM_ABERTO|NAO_LOCALIZADO), totalPaidAmount, payments[].
**5.2 Item de payments:** clearingDocument (AUGBL), clearingDate (AUGDT), paymentPostingDate (BKPF-BUDAT), paidAmount, paymentMethod (ZLSCH), paymentMethodDesc, settlementMode (mapeamento art. 27), isPartial (true se REBZG preenchido).

## 6. Regras de negócio
**6.1 Fluxo principal (NF-e/CT-e):** decompor chave (posição 21-22: 55=NF-e, 57=CT-e); localizar DOCNUM em J_1BNFE_ACTIVE; apenas documentos autorizados e não cancelados; navegar J_1BNFLIN (REFTYP=LI) → RBKP → BKPF (AWTYP=RMRP); buscar partidas em BSIK (aberto) e BSAK (compensadas).
**6.2 Fallback NFS-e/lançamentos diretos FI (FB60/FB01):** extrair CNPJ emitente + número documento da chave; buscar partidas por CNPJ (LFA1-STCD1) cujo XBLNR contenha o número, janela ± 90 dias (configurável); um candidato → matchType=FALLBACK; múltiplos → melhor candidato + fallbackNote de ambiguidade; nenhum → NAO_LOCALIZADO. **Ponto em aberto explícito na spec:** layout da chave NFS-e nacional difere da NF-e, decomposição a confirmar antes do desenvolvimento.
**6.3 Pagamentos parciais/residuais/adiantamentos:** partidas com REBZG apontando fatura original agregadas ao documento; seguir cadeia REBZG até fatura raiz; adiantamentos (razão especial, UMSKZ) fora do escopo v1, aparecem naturalmente se vinculados na compensação; compensações que não são pagamento (estornos/reclassificações) filtradas por BLART configurável (sugestão: apenas KZ e ZP).
**6.4 Mapeamento forma de pagamento → modalidade art. 27 (NÃO obrigatório):** tabela de-para Z (ZLSCH → modalidade) mantida pelo time fiscal, contempla split payment, recolhimento pelo adquirente, demais meios; valores não mapeados retornam NAO_MAPEADO.
**6.5 paymentStatus:** PAGO_TOTAL (soma pagamentos ≥ valor fatura e sem saldo em BSIK); PAGO_PARCIAL (há liquidação e saldo remanescente); EM_ABERTO (nenhuma liquidação); NAO_LOCALIZADO (chave não resolvida).

## 7. Códigos de retorno
200 (mesmo p/ NAO_LOCALIZADO — não é erro); 400 (chave malformada ou **lote > 500 chaves**); 401/403 (auth); 429 (rate limit, header Retry-After); 500 (erro interno, correlationId).

## 8. Requisitos não funcionais
Desempenho: consulta individual ≤2s p95; lote de 5000 chaves ≤160s p95 (validar com massa realista no QAS). Volumetria: estimativa mensal a levantar com área fiscal. Disponibilidade: horário comercial estendido 7h-22h dias úteis + janelas de manutenção RISE. Segurança: OAuth2 client credentials ou X.509, usuário somente leitura restrito por BUKRS, TLS 1.2+. LGPD: dados de PJ; se PF (CPF) entrar no escopo, avaliar minimização/base legal com DPO. Auditoria: log de aplicação (BAL) por requisição — consumidor, qtd chaves, tempo resposta, correlationId. Rate limiting: sugestão inicial 60 req/min por consumidor.

## 9. Critérios de aceite (12 casos de teste)
01 match direto pago integral; 02 dois pagamentos parciais; 03 fatura em aberto; 04 NF-e cancelada → NAO_LOCALIZADO; 05 chave DV inválido → 400; 06 fallback candidato único; 07 fallback múltiplos candidatos; **08 lote com 500 chaves mistas → 200**; **09 lote com 501 chaves → 400**; 10 estorno filtrado (BLART fora da lista); 11 fatura de empresa fora do escopo → NAO_LOCALIZADO (sem vazamento entre empresas); 12 consumidor sem autorização → 401/403.

## 10. Questões em aberto (do próprio documento SAP, resolver antes do build do lado SAP)
- Confirmar layout/decomposição da chave NFS-e nacional para o fallback.
- Definir lista de empresas (BUKRS) no escopo e tipos de documento de pagamento (BLART) válidos.
- Validar com o time fiscal a tabela de-para ZLSCH → modalidade art. 27, incluindo split payment futuro.
- Definir padrão de exposição externa no RISE (Cloud Connector, API Management, WAF) com Basis e Segurança.
- Levantar volumetria mensal esperada de chaves para dimensionar SLA e rate limit.
- Confirmar nomes de campos das tabelas do NF Writer (J_1BNFDOC/J_1BNFE_ACTIVE) na release instalada.

## 11. Referências
LC 214/2025, arts. 27, 28 e 46; Ato Conjunto RFB/CGIBS nº 1/2025; esboço de CDS views entregue anteriormente (zcds_pagamentos_fornecedores_nfe.abap); documentação da API do Portal Nacional da RTC.

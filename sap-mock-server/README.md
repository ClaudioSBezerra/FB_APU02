# SAP Mock Server

Simula a API SAP "Consulta de Pagamentos por Chave de DF-e" (spec técnica v0.1, 09/07/2026, time FI/Basis) enquanto a API real não existe. **Não é código de produção** — ferramenta de desenvolvimento para destravar a Story 1.2 e a Epic 2 (sincronização automática de pagamentos) do PRD "Carga de Pagamentos SAP S/4HANA".

## Como rodar

```bash
cd sap-mock-server
go run .
# ou numa porta diferente:
SAP_MOCK_PORT=9090 go run .
```

Sem dependências externas — só biblioteca padrão do Go.

## Como usar com o FB_APU02

1. Rode o mock server (padrão: porta `8090`).
2. Na tela `/config/sap-credenciais` (Story 1.1), cadastre uma credencial de teste com:
   - **Base URL:** `http://localhost:8090`
   - **Client ID / Client Secret:** qualquer valor não vazio (veja seção de erros forçados abaixo para casos especiais)
   - **BUKRS:** qualquer código, ex. `1000`

Qualquer código que consumir essa API (Story 1.2 "testar conexão", ou o motor de sincronização da Epic 2) vai funcionar contra o mock exatamente como funcionaria contra a API real, seguindo o mesmo contrato.

## Endpoints

| Método | Path | Descrição |
|---|---|---|
| `POST` | `/token` | OAuth2 client_credentials — `Authorization: Basic` com client_id/secret |
| `GET` | `/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/PaymentByDFe(dfeKey='{chave}')` | Consulta individual |
| `POST` | `/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments` | Consulta em lote (`{"dfeKeys": [...]}`, máx. 500) |
| `GET` | `/health` | Healthcheck |

## Cenários determinísticos

A mesma chave sempre produz a mesma resposta (soma dos dígitos da chave `mod 5` decide o cenário) — útil para testes repetíveis:

| Cenário | `matchType` | `paymentStatus` |
|---|---|---|
| 0 | `CHAVE` | `PAGO_TOTAL` (1 pagamento cobrindo o valor total) |
| 1 | `CHAVE` | `PAGO_PARCIAL` (2 parcelas — "pagamento por conta") |
| 2 | `CHAVE` | `EM_ABERTO` (sem nenhum pagamento) |
| 3 | `NAO_LOCALIZADO` | `NAO_LOCALIZADO` |
| 4 | `FALLBACK` | `PAGO_TOTAL` + `fallbackNote` (match ambíguo simulado) |

Chaves NF-e/CT-e (44 dígitos) têm `dfeType` e `supplierCNPJ` decompostos da própria chave (posições 21-22 = modelo, 7-20 = CNPJ do emitente), igual à spec real. Chaves de 50 dígitos voltam como `NFSE`. Qualquer outro tamanho é rejeitado com 400 (individual) ou marcado `DESCONHECIDO`/`NAO_LOCALIZADO` sem derrubar o lote (batch).

## Forçando casos de erro

| Trigger | Efeito |
|---|---|
| `client_id = "erro_401"` no `/token` | 401 (credencial inválida) |
| Chave iniciando com `ERR429` | 429 com `Retry-After` |
| Chave iniciando com `ERR500` | 500 |
| Chave com tamanho diferente de 44/50 | 400 (consulta individual) |
| Lote com mais de 500 chaves | 400 |
| `SAP_MOCK_SIMULATE_RATE_LIMIT=true` | a cada 10ª requisição (qualquer endpoint autenticado) retorna 429, para testar retry/backoff sem precisar de chaves mágicas |

## Quando a API real existir

Basta trocar o `base_url` da credencial de `http://localhost:8090` para a URL real do SAP RISE — nenhum código do FB_APU02 precisa mudar, já que o mock replica o contrato exato da spec (incluindo o limite de lote de 500 decidido no PRD FR-2).

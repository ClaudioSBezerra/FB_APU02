---
quick_id: 260516-da8
task: Abortar solicitações RFB travadas + auto-abort scheduler (> 5 horas)
date: 2026-05-16
---

# Context: Abortar Solicitações RFB Travadas

## Problema

Requests RFB presas desde 2026-05-09 em estados intermediários (`requested`, `webhook_received`, `downloading`, `reprocessing`). Sem mecanismo de timeout, ficam em looping indefinidamente bloqueando reprocessamento.

## Decisões

| # | Decisão | Escolha | Motivo |
|---|---------|---------|--------|
| D-01 | Requests sem raw_json após abort | Botão "Re-solicitar" inline | Reset para `pending` → scheduler re-envia à RFB API sem criar nova linha |
| D-02 | Requests existentes presas (2026-05-09) | Migration SQL no deploy | Corrige imediatamente; não espera scheduler |
| D-03 | Threshold de timeout | 5 horas | Conforme requisito do usuário |
| D-04 | Frequência do scheduler | A cada 5 minutos | Balanceia reatividade vs carga DB |

## Comportamento por estado após abort

| Status stuck | Tem raw_json? | Botão exibido |
|--------------|---------------|---------------|
| `requested` | Não (sem webhook) | Re-solicitar |
| `webhook_received` | Não (sem download) | Re-solicitar |
| `downloading` | Incerto | Reprocessar (tenta; erro se ausente) |
| `reprocessing` | Sim | Reprocessar |

**Discriminador no frontend:** `has_raw_json: bool` retornado pelo endpoint list.

## Fluxo "Re-solicitar"

1. Request está em `error` + `has_raw_json = false`
2. Usuário clica "Re-solicitar"
3. POST /api/rfb/apuracao/resolicitar → reset: `status='pending'`, limpa `error_code`, `error_message`, `raw_json`, `tiquete_download`
4. Scheduler pega na próxima varredura e re-envia à RFB API
5. Estado volta ao ciclo normal

## Estado visual no frontend

- Request em estado intermediário com `updated_at > 5h` → badge laranja "Travado" + botão "Abortar"
- Request em `error` com `has_raw_json = false` → botão "Re-solicitar" (azul)
- Request em `error` com `has_raw_json = true` → botão "Reprocessar" (já existe)

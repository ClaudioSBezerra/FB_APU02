---
title: 'ERP Bridge: confirmação antes de Regenerar API Key'
type: 'bugfix'
created: '2026-07-30'
status: 'done'
review_loop_iteration: 0
context: []
route: 'one-shot'
---

# ERP Bridge: confirmação antes de Regenerar API Key

## Intent

**Problem:** O botão "Regenerar" da API Key do Daemon (`ERPBridgeCredenciais.tsx`) trocava a chave imediatamente ao clicar, sem nenhum aviso de que isso invalida na hora a chave que o daemon do ERP Bridge no servidor do cliente está usando. Foi exatamente essa a causa raiz de um incidente de produção (30/07/2026, empresa FCCORP): a chave foi regenerada, o `config.yaml` do servidor AWS ficou com a chave antiga, e toda importação passou a falhar com HTTP 401 "API key inválida" — falha que ficou invisível na tela porque `bridge.py` só loga o erro localmente, nunca o propaga ao backend.

**Approach:** Adicionar `window.confirm(...)` antes de disparar `generateApiKeyMutation.mutate()`, seguindo o mesmo padrão já usado em `ERPBridgeConfig.tsx` para Abortar/Cancelar. Só dispara a confirmação quando já existe uma chave (`apiKey` truthy — ação "Regenerar"); na primeira geração (`apiKey` vazio, botão "Gerar chave") não há daemon rodando ainda com chave antiga, então não há risco a avisar.

## Suggested Review Order

**Correção principal**

- Guarda de confirmação no botão "Regenerar" — `if (!apiKey || window.confirm(...)) generateApiKeyMutation.mutate()`, seguindo o idioma já usado nos confirms de `ERPBridgeConfig.tsx`.
  [`ERPBridgeCredenciais.tsx:238`](../../frontend/src/pages/ERPBridgeCredenciais.tsx#L238)

**Achados da revisão adversarial — registrados em `deferred-work.md`, nenhum bloqueia esta correção**

- Guarda depende de estado local (`apiKey`), não do servidor — risco de multi-aba, baixa probabilidade, nunca pior que o comportamento anterior (sem confirmação).
- `window.confirm` nativo reforça um padrão ad-hoc já usado em outras 3 páginas, enquanto o projeto já tem um `AlertDialog` (Radix/shadcn) tematizado e não usado em nenhuma página — migração maior, fora de escopo.
- Instrução "atualize o config.yaml no servidor" duplicada em 3 lugares do mesmo componente (ajuda estática, toast, e agora a confirmação).
- Guarda vive só no `onClick`, não centralizada na mutation — um futuro segundo ponto de disparo a pularia.
- Sem teste automatizado (consistente com a política já aceita no projeto).

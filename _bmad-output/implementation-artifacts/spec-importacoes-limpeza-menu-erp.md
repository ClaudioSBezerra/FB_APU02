---
title: 'Importações VIA ERP: remove itens de XML sem backend e abre acesso ERP para todos'
type: 'chore'
created: '2026-07-17'
status: 'done'
route: 'one-shot'
---

# Importações VIA ERP: remove itens de XML sem backend e abre acesso ERP para todos

## Intent

**Problem:** O módulo "Importações VIA ERP" tinha 3 itens desabilitados (NFS-e Entradas/Saídas, CT-e Saídas) sem backend, e os 2 itens reais (Importar via ERP, Logs ERP) eram visíveis só para admin — deixando o módulo praticamente vazio para usuário comum.

**Approach:** Remover os 3 itens desabilitados de `navigation.ts`. Remover `adminOnly: true` de "Importar via ERP"/"Logs ERP" para abrir a visibilidade no menu para todos os usuários. Durante o Blind Hunter, identificado que as rotas correspondentes em `App.tsx` ainda estavam envolvidas em `<AdminRoute>` — removido também, para manter coerência entre visibilidade no menu e acesso à rota (confirmado com o usuário antes de aplicar).

## Suggested Review Order

- Ponto único de configuração: 3 itens desabilitados removidos, `adminOnly` removido dos 2 itens restantes.
  [`navigation.ts:27`](../../frontend/src/lib/navigation.ts#L27)

- Rotas correspondentes destravadas de `<AdminRoute>` para coerência com a nova visibilidade no menu — achado do Blind Hunter, corrigido nesta mesma entrega.
  [`App.tsx:279`](../../frontend/src/App.tsx#L279)

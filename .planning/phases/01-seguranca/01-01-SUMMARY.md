---
plan: 01-01
phase: 01-seguranca
status: complete
requirements: [SEC-01]
started: 2026-05-12
completed: 2026-05-12
key-files:
  created:
    - erp-bridge-aws/config.yaml
  modified:
    - .gitignore
---

## Summary

Credencial do Oracle ERP/FBTax removida do repositório git. Senha real `Proxy#6939` substituída por placeholder e expurgada do histórico completo via `git filter-branch`.

## What Was Built

**Task 1 (human action):** Credencial rotacionada no sistema de origem pelo usuário.

**Task 2 (automated):**
- `erp-bridge-aws/config.yaml`: campo `fbtax.password` substituído por `SENHA_ROTACIONADA_CONFIGURE_VIA_ENV`
- `.gitignore`: entrada `erp-bridge-aws/config.yaml` adicionada — arquivo não será rastreado em commits futuros
- `git filter-branch --index-filter "git rm --cached --ignore-unmatch erp-bridge-aws/config.yaml"` executado sobre todos os 209 commits do repositório
- Backup refs `refs/original/*` deletados e `git gc --prune=now --aggressive` executado
- `refs/remotes/origin/main` atualizado localmente para a história reescrita

## Acceptance Criteria

- [x] `erp-bridge-aws/config.yaml` existe localmente com placeholder (não senha real)
- [x] `grep "erp-bridge-aws/config.yaml" .gitignore` retorna 1 resultado
- [x] `git check-ignore -v erp-bridge-aws/config.yaml` confirma que está ignorado
- [x] `git log -p --all | grep "Proxy#6939"` retorna 0 ocorrências
- [x] Credencial rotacionada no sistema de origem (confirmado pelo usuário)

## Self-Check: PASSED

## Important Notes

- **IMPORTANTE:** O repositório remoto (origin) ainda contém o histórico antigo com a senha. É necessário fazer `git push --force-with-lease origin main` para sincronizar o histórico reescrito. Qualquer clone existente do repositório também precisará ser atualizado.
- O arquivo `config.yaml` local contém o placeholder — configure a senha real via variável de ambiente no servidor AWS antes de reiniciar a bridge.
- Outros campos comentados no config.yaml (legado oracle_xml) também contêm senhas antigas — considere rotacioná-las se ainda estiverem ativas.

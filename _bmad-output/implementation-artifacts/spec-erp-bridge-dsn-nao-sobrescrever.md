---
title: 'ERP Bridge: DSN do Oracle não é mais sobrescrito pela tela'
type: 'bugfix'
created: '2026-08-04'
status: 'done'
review_loop_iteration: 0
context: []
route: 'one-shot'
---

# ERP Bridge: DSN do Oracle não é mais sobrescrito pela tela

## Intent

**Problem:** O campo "Oracle DSN" salvo na tela (Configurações → ERP Bridge → Cred. ERP Bridge) era buscado pelo `bridge.py` a cada execução (`fetch_credentials()`) e sobrescrevia o `config.yaml` local do servidor — mesmo já corrigido manualmente. Isso causou um incidente real hoje: corrigir o DSN na tela pro Oracle de produção (FCCORP, `10.131.1.118:1521/fccorp`), reiniciar o daemon, e mesmo assim continuar conectando no DSN antigo de teste (`10.136.1.211:1521/fcjpateste`, João Pessoa) — mais de 1h de depuração até isolar a causa.

**Approach:** Parar de sobrescrever `oracle_dsn` a partir da API — `usuario`/`senha` continuam vindo do backend normalmente (credenciais rotacionáveis, criptografadas); o DSN passa a ser controlado só pelo `config.yaml` local do servidor. A revisão adversarial encontrou a lógica de aplicar credenciais duplicada em 2 lugares em `main()` (modo daemon e modo CLI/normal) — só um tinha sido corrigido inicialmente. Consolidado numa função só (`apply_fetched_credentials`), eliminando a duplicação que já tinha causado esse tipo de drift.

## Suggested Review Order

- Função nova consolidando a lógica antes duplicada nos 2 call sites — DSN nunca mais sobrescrito, comentário explica o porquê.
  [`bridge.py:1250`](../../erp-bridge-aws/bridge.py#L1250)

- Call site do modo daemon, agora 1 linha.
  [`bridge.py:1311`](../../erp-bridge-aws/bridge.py#L1311)

- Call site do modo CLI/normal, agora 1 linha — achado de revisão: esse era o segundo lugar com a mesma lógica, não corrigido na primeira passada.
  [`bridge.py:1338`](../../erp-bridge-aws/bridge.py#L1338)

**Achados da revisão adversarial — registrados em `deferred-work.md`, nenhum bloqueia esta correção**

- Campo "Oracle DSN" continua na tela mas virou no-op pra sap_s4hana — nada avisa o usuário disso.
- Trocar erp_type remotamente agora exige um passo manual extra (editar config.yaml) não documentado.
- Perda da capacidade de rotacionar DSN remotamente (útil pra DR/migração) — tradeoff aceito mas não documentado antes.
- `oracle_dsn` continua em texto puro no banco (não criptografado) — pré-existente, motivo de reconsiderar se o campo deve sair do endpoint de credenciais.
- Clientes sap_s4hana sem `oracle.dsn` no config.yaml vão bater no erro de fail-fast já existente, sem migração proativa.
- Sem teste automatizado — consistente com a política já aceita pra este diretório.

## Nota operacional

Esta correção só tem efeito depois que o `bridge.py` atualizado for copiado pro servidor e o serviço reiniciado. O `config.yaml` do servidor precisa ter `oracle.dsn: "10.131.1.118:1521/fccorp"` (produção FCCORP) — ação do usuário, fora deste workflow.

-- Migration 117: Novo status de falha de credencial em sap_sync_runs (Story 2.2, AC #6)
-- Distingue falha de credencial (401/403, sem retry) de falha transitória esgotada
-- (429/500 após 3 tentativas, status 'falha', já existente desde a migration 116).
-- sap_sync_runs.status não tinha CHECK constraint (migration 116) — adicionada aqui
-- para fechar o enum e evitar typos silenciosos em código futuro.

-- NOT VALID evita lock exclusivo bloqueante validando a tabela inteira de uma
-- vez; VALIDATE CONSTRAINT roda em seguida com um lock mais brando (SHARE
-- UPDATE EXCLUSIVE), sem bloquear leituras/escritas concorrentes.
ALTER TABLE sap_sync_runs
    ADD CONSTRAINT sap_sync_runs_status_check
    CHECK (status IN ('em_andamento', 'concluido', 'falha', 'falha_credencial'))
    NOT VALID;

ALTER TABLE sap_sync_runs
    VALIDATE CONSTRAINT sap_sync_runs_status_check;

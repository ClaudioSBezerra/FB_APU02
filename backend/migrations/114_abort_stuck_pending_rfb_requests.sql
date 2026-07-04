-- Migration 114: aborta requests RFB órfãos em status 'pending' há mais de 5 horas.
--
-- 'pending' é setado pelo "Re-solicitar" mas nenhum job consome esse status —
-- o request fica órfão, aparece como "Travado" na UI e o Abortar manual (que não
-- cobria 'pending') retornava 404. O handler e o auto-abort do scheduler passam
-- a cobrir 'pending'; esta migration limpa os já travados. Idempotente.

UPDATE rfb_requests
SET status        = 'error',
    error_code    = 'TIMEOUT',
    error_message = 'Abortada automaticamente: solicitação sem resposta por mais de 5 horas',
    updated_at    = CURRENT_TIMESTAMP
WHERE status = 'pending'
  AND updated_at < NOW() - INTERVAL '5 hours';

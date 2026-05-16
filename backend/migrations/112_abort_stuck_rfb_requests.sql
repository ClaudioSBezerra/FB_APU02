-- Aborta requests RFB presas há mais de 5 horas em estados intermediários.
-- Executado uma vez no deploy; idempotente.
UPDATE rfb_requests
SET status        = 'error',
    error_code    = 'TIMEOUT',
    error_message = 'Abortada automaticamente: solicitação sem resposta por mais de 5 horas',
    updated_at    = CURRENT_TIMESTAMP
WHERE status IN ('requested', 'webhook_received', 'downloading', 'reprocessing')
  AND updated_at < NOW() - INTERVAL '5 hours';

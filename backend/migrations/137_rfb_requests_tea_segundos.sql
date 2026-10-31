-- Tempo estimado de atendimento devolvido pela RFB na abertura (API CBS v2, campo tEASegundos).
-- NULL = sem estimativa (a RFB devolve -1) ou solicitação v1. Usado pelo poller para decidir
-- quando fazer a primeira consulta de situação (ver services/rfb_poller.go).
ALTER TABLE rfb_requests ADD COLUMN IF NOT EXISTS tea_segundos INTEGER;

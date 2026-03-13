-- Migration 063: Add tiquete_download column to rfb_requests
-- RFB webhook returns two separate tíquetes:
--   tiqueteSolicitacao = used to identify the request
--   tiqueteDownload    = the actual tíquete to use for file download
ALTER TABLE rfb_requests
  ADD COLUMN IF NOT EXISTS tiquete_download VARCHAR(255);

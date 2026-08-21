-- Migration 128: Liga/desliga a captura do cronograma de parcelas (Grupo Y)
-- no ERP Bridge. Split payment para pagamento parcelado (LC 214/2025) ainda
-- não tem data de início — enquanto não for necessário, o bridge nem conecta
-- nas bases legadas por filial pra buscar isso. Desligado por padrão.
ALTER TABLE erp_bridge_config
  ADD COLUMN IF NOT EXISTS gerar_cronograma_parcelas BOOLEAN NOT NULL DEFAULT false;

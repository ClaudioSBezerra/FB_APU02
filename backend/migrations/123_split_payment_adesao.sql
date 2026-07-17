-- Migration 123: Adesão voluntária ao Split Payment (Fase 1, LC 214/2025, vigência 2027)
ALTER TABLE parceiros
  ADD COLUMN IF NOT EXISTS aderiu_split_payment BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS data_adesao_split_payment DATE;

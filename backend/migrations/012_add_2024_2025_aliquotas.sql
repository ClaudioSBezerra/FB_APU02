-- Remove 2024, 2025, 2026 to keep only 2027-2033
DELETE FROM tabela_aliquotas WHERE ano IN (2024, 2025, 2026);
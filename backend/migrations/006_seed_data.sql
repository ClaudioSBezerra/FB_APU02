-- Seed data for testing Mercadorias report
-- Clean up existing test data to ensure fresh insert (Cascade will delete related reg_c100 records)
DELETE FROM import_jobs WHERE id = 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11';

-- Insert a fake job with UUID
INSERT INTO import_jobs (id, filename, status, created_at, company_name, cnpj, dt_ini, dt_fin)
VALUES ('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', 'DADOS_TESTE.txt', 'completed', NOW(), 'EMPRESA DEMO LTDA', '00000000000191', '2024-01-01', '2024-01-31');

-- Insert fake C100 (Saídas) - Adjusted columns to match reg_c100 definition
INSERT INTO reg_c100 (job_id, ind_oper, ind_emit, cod_part, cod_mod, cod_sit, ser, num_doc, chv_nfe, dt_doc, dt_e_s, vl_doc, vl_icms, vl_pis, vl_cofins, vl_piscofins)
VALUES ('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', '1', '0', 'P001', '55', '00', '1', '1001', 'KEY1', '2024-01-10', '2024-01-10', 15000.00, 2700.00, 247.50, 1140.00, 1387.50);

-- Insert fake C100 (Entradas) - Adjusted columns to match reg_c100 definition
INSERT INTO reg_c100 (job_id, ind_oper, ind_emit, cod_part, cod_mod, cod_sit, ser, num_doc, chv_nfe, dt_doc, dt_e_s, vl_doc, vl_icms, vl_pis, vl_cofins, vl_piscofins)
VALUES ('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', '0', '1', 'F001', '55', '00', '1', '2001', 'KEY2', '2024-01-15', '2024-01-15', 8000.00, 1440.00, 132.00, 608.00, 740.00);

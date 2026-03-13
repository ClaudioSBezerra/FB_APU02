-- Seed Future Aliquotas for 2027-2033 Transition
-- Values are estimated based on Constitutional Amendment 132/2023 transition rules
-- Note: CBS is federal, IBS is state/municipal.

INSERT INTO tabela_aliquotas (ano, perc_reduc_icms, perc_ibs_uf, perc_ibs_mun, perc_cbs) VALUES
(2027, 0.0,  0.05, 0.05, 8.80), -- CBS Full (8.8%), IBS Test (0.1%), ICMS Full
(2028, 0.0,  0.05, 0.05, 8.80), -- CBS Full, IBS Test, ICMS Full
(2029, 10.0, 1.0,  1.0,  8.80), -- ICMS 90% (Reduc 10%), IBS Scaling up
(2030, 20.0, 2.0,  2.0,  8.80), -- ICMS 80% (Reduc 20%)
(2031, 30.0, 3.0,  3.0,  8.80), -- ICMS 70% (Reduc 30%)
(2032, 40.0, 4.0,  4.0,  8.80), -- ICMS 60% (Reduc 40%)
(2033, 100.0, 9.0, 8.7,  8.80)  -- ICMS 0% (Reduc 100%), IBS Full (17.7%), CBS Full (8.8%)
ON CONFLICT (ano) DO UPDATE SET
perc_reduc_icms = EXCLUDED.perc_reduc_icms,
perc_ibs_uf = EXCLUDED.perc_ibs_uf,
perc_ibs_mun = EXCLUDED.perc_ibs_mun,
perc_cbs = EXCLUDED.perc_cbs;

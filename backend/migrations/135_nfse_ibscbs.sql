-- Migration 135: NFS-e — IBS/CBS do XML (leiaute 1.01) + versão do parser.
--
-- spec-nfse-ibscbs-xml.md. Puramente aditiva (133/134 continuam intactas). Mesmas colunas em
-- nfse_entradas e nfse_saidas.
--
-- Colunas IBS/CBS: NULL = grupo/elemento ausente no XML (nota anterior à reforma, ou valor
--   presente mas não interpretável) — distinto de 0.
--   Origem (infNFSe/IBSCBS): cLocalidadeIncid, valores/vBC, valores/uf/{pIBSUF,pAliqEfetUF},
--   valores/mun/{pIBSMun,pAliqEfetMun}, valores/fed/{pCBS,pAliqEfetCBS}, totCIBS/vTotNF,
--   totCIBS/gIBS/{vIBSTot, gIBSUFTot/vIBSUF, gIBSMunTot/vIBSMun}, totCIBS/gCBS/vCBS.
--   Origem (infNFSe/DPS/infDPS/IBSCBS): finNFSe, cIndOp, indDest, valores/trib/gIBSCBS/{CST,cClassTrib}.
--
-- xml_parser_versao: versão do parser que extraiu os campos de xml_gzip_b64. Linhas com versão
--   menor que a atual (services.NFSeXMLParserVersao = 2) são reextraídas no boot do backend
--   (services.ReprocessarXMLNFSe). Linhas existentes começam em 0.

ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_cod_localidade_incid VARCHAR(20);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_base                 NUMERIC(15,2);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibs_uf_aliquota             NUMERIC(7,4);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibs_uf_aliquota_efetiva     NUMERIC(7,4);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibs_mun_aliquota            NUMERIC(7,4);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibs_mun_aliquota_efetiva    NUMERIC(7,4);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS cbs_aliquota                NUMERIC(7,4);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS cbs_aliquota_efetiva        NUMERIC(7,4);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_valor_total_nf       NUMERIC(15,2);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibs_total                   NUMERIC(15,2);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibs_uf                      NUMERIC(15,2);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibs_mun                     NUMERIC(15,2);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS cbs                         NUMERIC(15,2);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_fin_nfse             VARCHAR(20);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_ind_op               VARCHAR(20);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_ind_dest             VARCHAR(20);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_cst                  VARCHAR(20);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS ibscbs_cclass_trib          VARCHAR(20);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS xml_parser_versao           SMALLINT NOT NULL DEFAULT 0;

ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_cod_localidade_incid VARCHAR(20);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_base                 NUMERIC(15,2);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibs_uf_aliquota             NUMERIC(7,4);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibs_uf_aliquota_efetiva     NUMERIC(7,4);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibs_mun_aliquota            NUMERIC(7,4);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibs_mun_aliquota_efetiva    NUMERIC(7,4);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS cbs_aliquota                NUMERIC(7,4);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS cbs_aliquota_efetiva        NUMERIC(7,4);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_valor_total_nf       NUMERIC(15,2);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibs_total                   NUMERIC(15,2);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibs_uf                      NUMERIC(15,2);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibs_mun                     NUMERIC(15,2);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS cbs                         NUMERIC(15,2);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_fin_nfse             VARCHAR(20);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_ind_op               VARCHAR(20);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_ind_dest             VARCHAR(20);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_cst                  VARCHAR(20);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS ibscbs_cclass_trib          VARCHAR(20);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS xml_parser_versao           SMALLINT NOT NULL DEFAULT 0;

-- Reprocessamento no boot: localiza as linhas pendentes sem varrer a tabela inteira.
CREATE INDEX IF NOT EXISTS idx_nfse_entradas_parser_pendente ON nfse_entradas(id) WHERE xml_parser_versao < 2 AND xml_gzip_b64 IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_nfse_saidas_parser_pendente   ON nfse_saidas(id)   WHERE xml_parser_versao < 2 AND xml_gzip_b64 IS NOT NULL;

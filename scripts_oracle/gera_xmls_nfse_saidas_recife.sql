-- =====================================================
-- GERA_XMLS_NFSE_SAIDAS_RECIFE.SQL  (v1 - PL/SQL Developer Command Window)
-- Gera um arquivo .xml por NFS-e em C:\TEMP
-- Formato Nacional (Recife/PE) - XML com declaracao encoding="utf-8"
-- Substitui encoding="utf-8" por encoding="windows-1252" na saida
-- Uso: @C:\TEMP\gera_xmls_nfse_saidas_recife.sql
-- =====================================================

ACCEPT V_DATA PROMPT 'Data inicial de emissao (DD/MM/YYYY) [01/01/2026]: '

SET SERVEROUTPUT ON SIZE UNLIMITED FORMAT TRUNCATED
SET FEEDBACK    OFF
SET HEADING     OFF
SET VERIFY      OFF
SET ECHO        OFF
SET TRIMSPOOL   ON
SET LINESIZE    32767

-- -------------------------------------------------------
-- FASE 1: Gera o driver script via DBMS_OUTPUT + SPOOL
-- -------------------------------------------------------
SPOOL C:\TEMP\_driver_xmls_nfse_recife.sql

DECLARE
    v_data  DATE   := TO_DATE('&V_DATA', 'DD/MM/YYYY');
    v_count NUMBER := 0;
BEGIN
    -- Cabecalho: configuracoes do driver
    DBMS_OUTPUT.PUT_LINE('SET HEADING OFF');
    DBMS_OUTPUT.PUT_LINE('SET FEEDBACK OFF');
    DBMS_OUTPUT.PUT_LINE('SET PAGESIZE 0');
    DBMS_OUTPUT.PUT_LINE('SET ECHO OFF');
    DBMS_OUTPUT.PUT_LINE('SET TRIMSPOOL ON');
    DBMS_OUTPUT.PUT_LINE('SET DEFINE OFF');
    DBMS_OUTPUT.PUT_LINE('SET LONG 1000000000');
    DBMS_OUTPUT.PUT_LINE('SET LONGCHUNKSIZE 32767');
    DBMS_OUTPUT.PUT_LINE('SET LINESIZE 32767');

    -- Um bloco por NFS-e encontrada
    FOR rec IN (
        SELECT x.num_nota
        FROM   sfc_nfse x
        WHERE  x.numero_nfse > 0
          AND  TRUNC(x.data_emissao_nfse) >= v_data
        ORDER BY x.num_nota
    ) LOOP
        v_count := v_count + 1;
        DBMS_OUTPUT.PUT_LINE('SPOOL "C:\TEMP\' || rec.num_nota || '.xml"');
        -- Formato Nacional ja tem <?xml version="1.0" encoding="utf-8"?>
        -- embutido no conteudo; substitui pelo encoding correto do banco
        DBMS_OUTPUT.PUT_LINE(
            'SELECT REGEXP_REPLACE(xml_nfse, ''encoding="[Uu][Tt][Ff]-8"'', ''encoding="windows-1252"'') FROM sfc_nfse' ||
            ' WHERE num_nota = ''' || rec.num_nota || '''' ||
            ' AND rownum = 1;'
        );
        DBMS_OUTPUT.PUT_LINE('SPOOL OFF');
    END LOOP;

    DBMS_OUTPUT.PUT_LINE('-- total: ' || v_count || ' NFS-e(s)');
END;
/

SPOOL OFF

-- -------------------------------------------------------
-- FASE 2: Executa o driver gerado
-- SET DEFINE OFF evita erro com & dentro do XML
-- -------------------------------------------------------
SET DEFINE OFF

@C:\TEMP\_driver_xmls_nfse_recife.sql

-- Restaura configuracoes
SET DEFINE      ON
SET LONG        80
SET LINESIZE    100
SET FEEDBACK    6
SET HEADING     ON
SET VERIFY      ON

PROMPT .
PROMPT Geracao concluida. Verifique os arquivos XML em C:\TEMP\

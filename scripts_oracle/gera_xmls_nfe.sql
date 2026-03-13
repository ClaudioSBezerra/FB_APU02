-- =====================================================
-- GERA_XMLS_NFE.SQL  (v2 - PL/SQL Developer Command Window)
-- Gera um arquivo .xml por NF-e em C:\TEMP
-- Uso: @C:\TEMP\gera_xmls_nfe.sql
-- =====================================================

ACCEPT V_DATA PROMPT 'Data de emissao (DD/MM/YYYY): '

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
SPOOL C:\TEMP\_driver_xmls.sql

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

    -- Um bloco por NF-e encontrada
    FOR rec IN (
        SELECT nfe
        FROM   sfc_nfe
        WHERE  data_emissao    = v_data
          AND  ultima_operacao = 'S'
        ORDER BY nfe
    ) LOOP
        v_count := v_count + 1;
        DBMS_OUTPUT.PUT_LINE('SPOOL "C:\TEMP\' || rec.nfe || '.xml"');
        DBMS_OUTPUT.PUT_LINE(
            'SELECT nota_xml FROM sfc_nfe' ||
            ' WHERE nfe = ''' || rec.nfe || '''' ||
            ' AND ultima_operacao = ''S''' ||
            ' AND rownum = 1;'
        );
        DBMS_OUTPUT.PUT_LINE('SPOOL OFF');
    END LOOP;

    DBMS_OUTPUT.PUT_LINE('-- total: ' || v_count || ' NF-e(s)');
END;
/

SPOOL OFF

-- -------------------------------------------------------
-- FASE 2: Executa o driver gerado
-- SET DEFINE OFF evita erro com & dentro do XML
-- -------------------------------------------------------
SET DEFINE OFF

@C:\TEMP\_driver_xmls.sql

-- Restaura configuracoes
SET DEFINE      ON
SET LONG        80
SET LINESIZE    100
SET FEEDBACK    6
SET HEADING     ON
SET VERIFY      ON

PROMPT .
PROMPT Geracao concluida. Verifique os arquivos XML em C:\TEMP\

-- =====================================================
-- IMPORTA_CTE_ENTRADA.SQL  (v1 - PL/SQL Developer Command Window)
-- Gera um arquivo .xml por CT-e de entrada em C:\TEMP
-- Uso: @C:\TEMP\importa_cte_entrada.sql
-- =====================================================

ACCEPT V_DATA PROMPT 'Data de captura XML (DD/MM/YYYY): '

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
SPOOL C:\TEMP\_driver_xmls_cte_entrada.sql

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

    -- Um bloco por CT-e de entrada encontrado
    FOR rec IN (
        SELECT C.CHAVE_CTE
        FROM   SFC_CTE_IMP C
        WHERE  TRUNC(C.DATA_IMPORTACAO) = v_data
        ORDER BY C.CHAVE_CTE
    ) LOOP
        v_count := v_count + 1;
        DBMS_OUTPUT.PUT_LINE('SPOOL "C:\TEMP\' || rec.CHAVE_CTE || '.xml"');
        -- Declara encoding para evitar erro de parse em caracteres acentuados
        DBMS_OUTPUT.PUT_LINE('PROMPT <?xml version="1.0" encoding="windows-1252"?>');
        DBMS_OUTPUT.PUT_LINE(
            'SELECT C.XML_CTE FROM SFC_CTE_IMP C' ||
            ' WHERE C.CHAVE_CTE = ''' || rec.CHAVE_CTE || '''' ||
            ' AND ROWNUM = 1;'
        );
        DBMS_OUTPUT.PUT_LINE('SPOOL OFF');
    END LOOP;

    DBMS_OUTPUT.PUT_LINE('-- total: ' || v_count || ' CT-e(s)');
END;
/

SPOOL OFF

-- -------------------------------------------------------
-- FASE 2: Executa o driver gerado
-- SET DEFINE OFF evita erro com & dentro do XML
-- -------------------------------------------------------
SET DEFINE OFF

@C:\TEMP\_driver_xmls_cte_entrada.sql

-- Restaura configuracoes
SET DEFINE      ON
SET LONG        80
SET LINESIZE    100
SET FEEDBACK    6
SET HEADING     ON
SET VERIFY      ON

PROMPT .
PROMPT Geracao concluida. Verifique os arquivos XML em C:\TEMP\

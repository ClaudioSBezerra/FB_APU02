-- Migration 132: dropa as 3 tabelas legadas do modelo período/tiquete (copiado da RFB em
-- março/2026, documentação da época estava errada — ver spec-cgibs-conta-corrente-plano.md).
-- Nenhum handler Go escreve nelas desde 131 (cgibs_apuracao.go/limpeza_total.go migraram para
-- cgibs_solicitacoes/cgibs_operacoes/cgibs_lancamentos/cgibs_arquivos em
-- spec-cgibs-nova-solicitacao-drop-legado.md) — confirmado via
-- `grep -rn "cgibs_requests\|cgibs_debitos\|cgibs_resumo" backend/handlers backend/services`
-- retornando vazio antes desta migration ser criada.
--
-- GUARDA DE SEGURANÇA (revisão adversarial, item 1): "nenhuma das 3 tabelas foi escrita em
-- produção" só foi verificado no banco de DEV local — sem checagem em runtime, essa suposição
-- não protege ambientes onde ela for falsa. Cada bloco abaixo aborta a migration inteira
-- (RAISE EXCEPTION — main.go só grava a migration como executada se ela não falhar, então uma
-- falha aqui é retentada no próximo start, não apenas logada) se a respectiva tabela tiver
-- QUALQUER linha. to_regclass trata "tabela já não existe" (reexecução idempotente após um DROP
-- anterior bem-sucedido) como "já foi dropada, seguir sem erro" — nunca falha numa 2ª execução
-- legítima.
--
-- Ordem segue as FKs: cgibs_resumo e cgibs_debitos referenciam cgibs_requests, então a checagem
-- e o DROP de cada uma acontecem antes de tocar em cgibs_requests.

DO $$
DECLARE cnt INTEGER;
BEGIN
    IF to_regclass('public.cgibs_resumo') IS NOT NULL THEN
        SELECT COUNT(*) INTO cnt FROM cgibs_resumo;
        IF cnt > 0 THEN
            RAISE EXCEPTION 'cgibs_resumo tem % linha(s) — abortando DROP, dado real seria perdido. Investigar antes de reexecutar.', cnt;
        END IF;
    END IF;
END $$;
DROP TABLE IF EXISTS cgibs_resumo;

DO $$
DECLARE cnt INTEGER;
BEGIN
    IF to_regclass('public.cgibs_debitos') IS NOT NULL THEN
        SELECT COUNT(*) INTO cnt FROM cgibs_debitos;
        IF cnt > 0 THEN
            RAISE EXCEPTION 'cgibs_debitos tem % linha(s) — abortando DROP, dado real seria perdido. Investigar antes de reexecutar.', cnt;
        END IF;
    END IF;
END $$;
DROP TABLE IF EXISTS cgibs_debitos;

DO $$
DECLARE cnt INTEGER;
BEGIN
    IF to_regclass('public.cgibs_requests') IS NOT NULL THEN
        SELECT COUNT(*) INTO cnt FROM cgibs_requests;
        IF cnt > 0 THEN
            RAISE EXCEPTION 'cgibs_requests tem % linha(s) — abortando DROP, dado real seria perdido. Investigar antes de reexecutar.', cnt;
        END IF;
    END IF;
END $$;
DROP TABLE IF EXISTS cgibs_requests;

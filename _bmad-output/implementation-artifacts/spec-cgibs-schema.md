---
title: 'CGIBS: schema de conta corrente fiscal (substitui modelo período/tiquete)'
type: 'feature'
created: '2026-09-28'
status: 'done'
review_loop_iteration: 1
baseline_commit: '9e92b6b121346f2304878e615421c8d11f1e2f5b'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-cgibs-conta-corrente-plano.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `cgibs_requests`/`cgibs_debitos`/`cgibs_resumo` (migration 102) modelam período+tipo_apuracao copiado da RFB; a API real do IBS é conta corrente fiscal por operação (chave de acesso), com lançamentos incrementais. **Loopback:** 1ª tentativa dropava as 3 tabelas antigas nesta mesma migration — revisão adversarial (2x) achou que `backend/handlers/cgibs_apuracao.go` (3 pontos) e `backend/handlers/limpeza_total.go` ainda leem/referenciam essas tabelas por nome; dropar quebra esses caminhos na hora (`relation does not exist`), mesmo elas nunca tendo sido escritas.

**Approach:** migration `131`: estender `cgibs_credentials` (Habilitação); criar `cgibs_solicitacoes`, `cgibs_arquivos`, `cgibs_operacoes`, `cgibs_lancamentos` **ao lado das** tabelas antigas — **sem dropar nada nesta sub-spec**. O drop de `cgibs_requests`/`cgibs_debitos`/`cgibs_resumo` fica para a sub-spec que reescreve `cgibs_apuracao.go`/`limpeza_total.go` (`spec-cgibs-solicitacao-ui.md`, última da sequência), quando o código parar de referenciá-las. Sem tabela de resumo agregado — resumo por período sempre calculado via SQL ao vivo sobre `cgibs_lancamentos`.

## Boundaries & Constraints

**Always:** migration aditiva `131_cgibs_conta_corrente_fiscal.sql`, **sem nenhum DROP TABLE**; `cgibs_credentials` só ganha colunas novas, sem alterar `company_id TEXT` existente (fora de escopo); tabelas novas usam `company_id UUID REFERENCES companies(id) ON DELETE CASCADE` **diretamente em toda tabela** (inclusive `cgibs_arquivos`/`cgibs_lancamentos`, denormalizado do pai — mesmo padrão já usado em `rfb_debitos`, evita depender de JOIN pra escopo de tenant); FKs para `cgibs_arquivos` (`ultimo_arquivo_id`, `arquivo_id`) usam `ON DELETE SET NULL` (não o default `NO ACTION`, que quebraria o CASCADE de `cgibs_solicitacoes`); CHECK constraints nos campos enum-like (valores exatos do MOC: `tipo_solicitacao IN ('manual','diferencial')`, `situacao_solicitacao IN ('solicitada','gerada','enviada','cancelada','expirada')`, `cgibs_arquivos.status IN ('pendente','baixado','erro')`) e nos campos de consistência que O PRÓPRIO CÓDIGO grava (`data_habilitacao` presente quando `habilitado`, `baixado_em` presente quando `status='baixado'`, `error_message` presente quando situação/status = erro); `id_solicitacao_externo`/`operacao_id_externo`/`lancamento_id_externo`/`numero_sequencial` viram `BIGINT` (IDs de terceiro, fora do nosso controle de crescimento); índice único parcial em `operacao_id_externo` espelhando o de `id_solicitacao_externo`; comentário de cabeçalho + `COMMENT ON COLUMN` explicando a diferença entre `ativo` (credencial configurada) e `habilitado` (CGIBS confirmou o registro) e a proveniência das colunas (seções do MOC).

**Ask First:** nenhuma pergunta nova nesta rodada — decisões já resolvidas no loopback (deixar tabelas antigas por enquanto).

**Never:** CHECK de não-negatividade nos 7 valores monetários do lançamento — dados vêm de terceiro (CGIBS), e rejeitar um INSERT por um valor negativo inesperado (ex: estorno/ajuste que o MOC não documentou) é pior que aceitar e validar depois — preferir perder visibilidade a perder o dado recebido; dropar `cgibs_requests`/`cgibs_debitos`/`cgibs_resumo` nesta sub-spec; alterar migrations existentes; tocar em `services/`/`handlers/` (só schema).

</frozen-after-approval>

## Code Map

- `backend/migrations/131_cgibs_conta_corrente_fiscal.sql` (novo)
- `backend/migrations/102_cgibs_tables.sql` -- schema atual, permanece intacto e em uso por ora
- `backend/handlers/cgibs_apuracao.go` -- ainda lê `cgibs_requests`/`cgibs_resumo` (linhas ~61, 91, 160, 185) — NÃO tocar aqui, só referência do motivo de não dropar
- `backend/handlers/limpeza_total.go:26` -- lista `cgibs_debitos`/`cgibs_requests`/`cgibs_resumo` na limpeza de ambiente — mesma razão

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/131_cgibs_conta_corrente_fiscal.sql` -- DDL completo em Design Notes: extensão de `cgibs_credentials`; criação de `cgibs_solicitacoes`, `cgibs_arquivos`, `cgibs_operacoes`, `cgibs_lancamentos` com `company_id` direto, FKs `ON DELETE SET NULL` onde aplicável, CHECKs de enum/consistência, `BIGINT` nos IDs externos, índices (incluindo o novo de `operacao_id_externo`), comentários.

**Acceptance Criteria:**
- Given a migration aplicada num banco limpo, when inspecionado, then `cgibs_requests`/`cgibs_debitos`/`cgibs_resumo` continuam existindo (intactas) e as 4 tabelas novas + colunas novas em `cgibs_credentials` também existem.
- Given a migration roda 2x, when reexecutada, then não falha e não altera nada.
- Given duas linhas de lançamento com o mesmo `(operacao_id, lancamento_id_externo)`, when a 2ª tenta inserir, then o índice único rejeita.
- Given uma `cgibs_solicitacoes` com `cgibs_arquivos`/`cgibs_operacoes`/`cgibs_lancamentos` vinculados via `ultimo_arquivo_id`/`arquivo_id`, when a solicitação é deletada, then o cascade completa sem erro de FK (os ponteiros viram NULL antes do cascade remover os arquivos).
- Given um insert com `tipo_solicitacao='invalido'`, when executado, then o CHECK rejeita.

## Design Notes

DDL completo (copiar literal):

```sql
-- Migration 131: CGIBS — conta corrente fiscal (substitui o modelo período/tiquete da 102).
-- Tabelas antigas (cgibs_requests/cgibs_debitos/cgibs_resumo) permanecem intactas nesta
-- migration — ainda são lidas por cgibs_apuracao.go/limpeza_total.go; dropar só quando
-- esses arquivos forem reescritos (spec-cgibs-solicitacao-ui.md).
-- Modelo derivado do MOC API Apuração Assistida IBS v1.10 (CGIBS, set/2026), seções 5.1-5.6
-- e ANEXO I (estrutura conta corrente fiscal / extrato_cc).

ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS webhook_url TEXT;
ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS token_contrib TEXT;
ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS habilitado BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS data_habilitacao TIMESTAMP WITH TIME ZONE;
ALTER TABLE cgibs_credentials DROP CONSTRAINT IF EXISTS cgibs_credentials_habilitado_chk;
ALTER TABLE cgibs_credentials ADD CONSTRAINT cgibs_credentials_habilitado_chk
    CHECK (NOT habilitado OR data_habilitacao IS NOT NULL);
COMMENT ON COLUMN cgibs_credentials.ativo IS 'Credencial configurada e habilitada para uso pelo admin (liga/desliga local) — não confundir com habilitado.';
COMMENT ON COLUMN cgibs_credentials.habilitado IS 'CGIBS confirmou o registro via API de Habilitação do Contribuinte (MOC 5.1) — só true após resposta Habilitado=sucesso.';

-- Nova Solicitação (5.5) + notificações de arquivo via webhook (5.2), manual e diferencial
CREATE TABLE IF NOT EXISTS cgibs_solicitacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    cnpj_base VARCHAR(8) NOT NULL,
    id_solicitacao_externo BIGINT,
    tipo_solicitacao VARCHAR(20) NOT NULL DEFAULT 'diferencial'
        CHECK (tipo_solicitacao IN ('manual','diferencial')),
    situacao_solicitacao VARCHAR(20) NOT NULL DEFAULT 'solicitada'
        CHECK (situacao_solicitacao IN ('solicitada','gerada','enviada','cancelada','expirada')),
    data_solicitacao TIMESTAMP WITH TIME ZONE,
    data_transacao_ini DATE,
    data_transacao_fim DATE,
    qtd_operacoes INTEGER DEFAULT 0 CHECK (qtd_operacoes >= 0),
    qtd_arq_vinculados INTEGER DEFAULT 0 CHECK (qtd_arq_vinculados >= 0),
    data_validade_solicitacao DATE,
    resultado TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (data_transacao_ini IS NULL OR data_transacao_fim IS NULL OR data_transacao_ini <= data_transacao_fim)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_solicitacoes_externo ON cgibs_solicitacoes(company_id, id_solicitacao_externo) WHERE id_solicitacao_externo IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cgibs_solicitacoes_company ON cgibs_solicitacoes(company_id, created_at DESC);

-- Arquivos vinculados a uma solicitação (Arquivos[] do webhook / Obter Arquivo, 5.2/5.3)
CREATE TABLE IF NOT EXISTS cgibs_arquivos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    solicitacao_id UUID NOT NULL REFERENCES cgibs_solicitacoes(id) ON DELETE CASCADE,
    numero_sequencial BIGINT NOT NULL CHECK (numero_sequencial > 0),
    nome_arquivo TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','baixado','erro')),
    baixado_em TIMESTAMP WITH TIME ZONE,
    raw_json TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (status <> 'baixado' OR baixado_em IS NOT NULL),
    CHECK (status <> 'erro' OR error_message IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_arquivos_seq ON cgibs_arquivos(solicitacao_id, numero_sequencial);
CREATE INDEX IF NOT EXISTS idx_cgibs_arquivos_company ON cgibs_arquivos(company_id);

-- Operações = conta corrente fiscal por documento fiscal (ANEXO I, nível 2)
CREATE TABLE IF NOT EXISTS cgibs_operacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    operacao_id_externo BIGINT,
    chave_acesso VARCHAR(44) NOT NULL CHECK (length(chave_acesso) = 44),
    dth_emissao TIMESTAMP WITH TIME ZONE,
    dth_autorizacao TIMESTAMP WITH TIME ZONE,
    cnpj_fornecedor VARCHAR(8), -- CNPJ8/raiz, conforme MOC ANEXO I (não é o CNPJ completo de 14)
    cnpj_adquirente VARCHAR(8), -- idem; opcional (MOC: "pode não existir ou ter um registro")
    extrato_hash VARCHAR(40),
    ultimo_arquivo_id UUID REFERENCES cgibs_arquivos(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (cnpj_fornecedor IS NOT NULL OR cnpj_adquirente IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_operacoes_chave ON cgibs_operacoes(company_id, chave_acesso);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_operacoes_externo ON cgibs_operacoes(company_id, operacao_id_externo) WHERE operacao_id_externo IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cgibs_operacoes_fornecedor ON cgibs_operacoes(company_id, cnpj_fornecedor);
CREATE INDEX IF NOT EXISTS idx_cgibs_operacoes_adquirente ON cgibs_operacoes(company_id, cnpj_adquirente);

-- Lançamentos = extrato_cc incremental (ANEXO I, nível 3-4) — 7 valores por lançamento
CREATE TABLE IF NOT EXISTS cgibs_lancamentos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    operacao_id UUID NOT NULL REFERENCES cgibs_operacoes(id) ON DELETE CASCADE,
    lancamento_id_externo BIGINT NOT NULL,
    dth_lancto TIMESTAMP WITH TIME ZONE NOT NULL,
    mov_codigo INTEGER, -- tabela de correspondência ainda não existe (MOC ANEXO I, campo MOV)
    mov_descricao TEXT, -- texto livre enquanto a tabela oficial não existir
    recurso_financeiro_disponivel NUMERIC(15,2) NOT NULL DEFAULT 0,
    recurso_financeiro_a_transferir NUMERIC(15,2) NOT NULL DEFAULT 0,
    credito_a_apropriar NUMERIC(15,2) NOT NULL DEFAULT 0,
    credito_nao_utilizado NUMERIC(15,2) NOT NULL DEFAULT 0,
    credito_utilizado NUMERIC(15,2) NOT NULL DEFAULT 0,
    debito_em_aberto NUMERIC(15,2) NOT NULL DEFAULT 0,
    debito_extinto NUMERIC(15,2) NOT NULL DEFAULT 0,
    arquivo_id UUID REFERENCES cgibs_arquivos(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_lancamentos_externo ON cgibs_lancamentos(operacao_id, lancamento_id_externo);
CREATE INDEX IF NOT EXISTS idx_cgibs_lancamentos_data ON cgibs_lancamentos(operacao_id, dth_lancto DESC);
CREATE INDEX IF NOT EXISTS idx_cgibs_lancamentos_company ON cgibs_lancamentos(company_id);
```

`company_id` em `cgibs_arquivos`/`cgibs_operacoes`/`cgibs_lancamentos` é sempre derivado da `cgibs_solicitacoes.company_id` que originou a cadeia (quem fez a solicitação/tem a credencial) — nunca de `cnpj_fornecedor`/`cnpj_adquirente` (partes fiscais da operação, não o tenant dono do dado).

## Spec Change Log

### Loopback 1 (2026-09-28)

**Trigger (bad_spec):** 2 revisores independentes confirmaram via grep que `cgibs_apuracao.go` (3 pontos) e `limpeza_total.go` ainda referenciam `cgibs_requests`/`cgibs_resumo`/`cgibs_debitos` — o DROP da 1ª tentativa quebraria esses handlers no deploy (erro de relation, não só dado vazio). Mesma classe de erro já cometida (e corrigida) na migração RFB v1→v2: "nunca escrita" não implica "nada referencia".

**Emenda:** DROP removido desta sub-spec inteiramente — tabelas antigas ficam paradas ao lado das novas até a sub-spec de UI reescrever os 2 arquivos que as leem. Aproveitei a rodada pra incorporar de uma vez os achados de patch dos 2 revisores (FK `ON DELETE SET NULL`, CHECKs de enum/consistência, `company_id` direto nas tabelas filhas, `BIGINT` nos IDs externos, índice único de `operacao_id_externo`, comentários) — evita uma 2ª rodada de review só pra essas correções mecânicas.

**KEEP:** todo o resto do desenho original (conta corrente por operação, lançamentos incrementais, sem tabela de resumo cacheado) confirmado correto pelos revisores — só o DROP e os itens de endurecimento mudaram.


**Pós-implementação (2026-09-28):** achei e corrigi um bug meu na própria spec após a implementação (não do agente, que copiou literal como pedido): `CHECK (situacao_solicitacao <> 'cancelada' OR error_message IS NULL OR TRUE)` tinha um `OR TRUE` residual que tornava a constraint sempre verdadeira (no-op morto, não protegia nada). Removida — não correspondia a nenhum invariante real (`situacao_solicitacao` não tem estado "erro" no enum da própria CGIBS, só `cgibs_arquivos.status`, que já tem seu CHECK correto). Reaplicado e reverificado localmente (idempotência + `\d` confirmando a remoção).

## Verification

**Commands:**
- Rodar a migration localmente (boot do backend) e `\d` nas 9 tabelas afetadas (5 novas/alteradas + as 3 antigas confirmando que continuam lá + índices).
- Rodar 2x pra confirmar idempotência.
- Testar o `ON DELETE` completo: criar solicitação→arquivo→operação→lançamento sintéticos, deletar a solicitação, confirmar cascade sem erro de FK.

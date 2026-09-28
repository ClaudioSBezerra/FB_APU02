package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"fb_apu02/crypto"
)

// ─── Tipos tolerantes (campo numérico pode vir como string ou número) ───
//
// Mesma lição já aprendida com FlexString/RFBTime em rfb_processor.go: API de governo, mesmo
// risco. FlexInt64/FlexFloat cobrem os campos ID/MOV e os 7 valores monetários de cada
// lançamento (Design Notes da spec-cgibs-obter-arquivo-parser.md).

// FlexInt64 unmarshals both JSON numbers and JSON strings into an int64.
type FlexInt64 int64

func (fi *FlexInt64) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*fi = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*fi = FlexInt64(n)
		return nil
	}
	// Melhor esforço: aceita "123.0" (número decimal representando um inteiro exato) — mas
	// REJEITA parte fracionária não-zero (ex.: "123.5") em vez de truncar silenciosamente
	// (item 3 da revisão adversarial), e valida a faixa de int64 ANTES de converter (item 4 —
	// converter um float64 fora de math.MinInt64/MaxInt64 pra int64 tem resultado
	// implementation-specific em Go, não pode confiar num truncamento "seguro").
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("FlexInt64: valor %q não é inteiro nem número", s)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("FlexInt64: valor %q não é um número finito", s)
	}
	if math.Trunc(f) != f {
		return fmt.Errorf("FlexInt64: valor %q tem parte fracionária não-zero, não é um inteiro", s)
	}
	if f < math.MinInt64 || f > math.MaxInt64 {
		return fmt.Errorf("FlexInt64: valor %q está fora da faixa representável de int64", s)
	}
	*fi = FlexInt64(int64(f))
	return nil
}

// FlexFloat unmarshals both JSON numbers and JSON strings (e.g. "123.45") into a float64.
type FlexFloat float64

func (ff *FlexFloat) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*ff = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("FlexFloat: valor %q não é numérico: %w", s, err)
	}
	// Item 5 da revisão adversarial: strconv.ParseFloat aceita "NaN"/"Inf"/"-Inf" como texto
	// válido, mas nenhum desses faz sentido num saldo monetário — rejeita explicitamente em vez
	// de deixar um valor não-finito ser gravado (NUMERIC do Postgres nem aceita NaN/Inf, o INSERT
	// falharia de um jeito confuso mais adiante).
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("FlexFloat: valor %q não é um saldo monetário válido (NaN/Inf)", s)
	}
	*ff = FlexFloat(f)
	return nil
}

// ─── Structs do arquivo obtido via ObterArquivo (MOC ANEXO I) ───
//
// JSON raiz é o próprio "header" (sem wrapper) — ver Design Notes da spec.

// CGIBSArquivoJSON é o JSON raiz de um arquivo de conta corrente fiscal (ANEXO I).
type CGIBSArquivoJSON struct {
	NomeArquivo       string                     `json:"nomearquivo"`
	TipoSolicitacao   string                     `json:"tiposolicitacao"`
	DataGeracao       string                     `json:"datageracao"`
	ParametrosGeracao CGIBSParametrosGeracaoJSON `json:"parametrosgeracao"`
	Operacoes         []CGIBSOperacaoJSON        `json:"operacoes"`
}

type CGIBSParametrosGeracaoJSON struct {
	DataInicial  string `json:"datainicial"`
	DataFinal    string `json:"datafinal"`
	QtdOperacoes int    `json:"qtdoperacoes"`
}

// CGIBSOperacaoJSON é a conta corrente fiscal de um documento fiscal (nível 2 do ANEXO I).
//
// ID é ponteiro (item 6 da revisão adversarial): nil = campo ausente no payload, valor presente
// (mesmo que 0) = ID legítimo — um int64 puro não distinguiria "ausente" de "veio 0".
type CGIBSOperacaoJSON struct {
	ID             *FlexInt64          `json:"ID"`
	ChaveAcesso    string              `json:"CHAVE_ACESSO"`
	DthEmissao     string              `json:"DTH_EMISSAO"`
	DthAutorizacao string              `json:"DTH_AUTORIZACAO"`
	CNPJFornecedor string              `json:"CNPJ_FORNECEDOR"`
	CNPJAdquirente string              `json:"CNPJ_ADQUIRENTE"`
	ExtratoCC      *CGIBSExtratoCCJSON `json:"extrato_cc"`
}

type CGIBSExtratoCCJSON struct {
	Hash  string                `json:"hash"`
	Lista []CGIBSLancamentoJSON `json:"lista"`
}

// CGIBSLancamentoJSON é um lançamento incremental do extrato_cc (nível 3-4 do ANEXO I) — 7
// valores simultâneos por lançamento. Nomes de campo JSON tomados literalmente da spec (UPPER_
// SNAKE, incluindo a grafia real "CREDITO_A_PROPRIAR" do MOC, sem o "A" duplicado, embora a
// coluna do banco seja credito_a_apropriar).
//
// ID e Mov são ponteiros pela mesma razão de CGIBSOperacaoJSON.ID (item 6): nil = ausente,
// valor presente (mesmo 0) = dado real. ID do lançamento é obrigatório pelo MOC ("sempre
// presente e único por linha") — ausente vira erro do item isolado em insertCGIBSLancamento,
// nunca um silencioso 0.
type CGIBSLancamentoJSON struct {
	ID                           *FlexInt64 `json:"ID"`
	DthLancto                    string     `json:"DTH_LANCTO"`
	Mov                          *FlexInt64 `json:"MOV"`
	RecursoFinanceiroDisponivel  FlexFloat  `json:"RECURSO_FINANCEIRO_DISPONIVEL_PARA_TRANSFERENCIA"`
	RecursoFinanceiroATransferir FlexFloat  `json:"RECURSO_FINANCEIRO_A_TRANSFERIR"`
	CreditoAApropriar            FlexFloat  `json:"CREDITO_A_PROPRIAR"`
	CreditoNaoUtilizado          FlexFloat  `json:"CREDITO_NAO_UTILIZADO"`
	CreditoUtilizado             FlexFloat  `json:"CREDITO_UTILIZADO"`
	DebitoEmAberto               FlexFloat  `json:"DEBITO_EM_ABERTO"`
	DebitoExtinto                FlexFloat  `json:"DEBITO_EXTINTO"`
}

// cgibsParserTimeLayouts cobre os formatos observados no exemplo do MOC (Design Notes da spec):
// datageracao/DTH_LANCTO sem timezone, DTH_EMISSAO/DTH_AUTORIZACAO com milissegundos+offset.
var cgibsParserTimeLayouts = []string{
	"2006-01-02 15:04:05.000-07:00",
	"2006-01-02T15:04:05.000-07:00",
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// cgibsParserLocation é o fuso assumido para os campos de data/hora do arquivo que NÃO trazem
// offset explícito (ex.: DTH_LANCTO) — item 17 da revisão adversarial. DTH_EMISSAO/
// DTH_AUTORIZACAO no MESMO payload trazem offset explícito ("-03:00"), o que sugere que o
// sistema de origem roda em horário de Brasília; assumimos o mesmo para os campos sem offset em
// vez de UTC (suposição de melhor esforço, não 100% confirmada pelo MOC — ajustar se a CGIBS
// documentar formato diferente). Layouts que JÁ trazem offset no valor continuam corretos com
// ParseInLocation (o offset do próprio valor prevalece; a location só é usada quando o valor não
// tem nenhuma informação de fuso).
var cgibsParserLocation = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		log.Printf("[CGIBS Parser] AVISO: falha ao carregar fuso America/Sao_Paulo (%v) — usando UTC como fallback", err)
		return time.UTC
	}
	return loc
}()

// parseCGIBSParserTime tenta parsear uma data/hora do arquivo nos formatos conhecidos.
// Melhor esforço: devolve nil (sem erro) se não reconhecer — mesmo princípio de RFBTime
// (rfb_processor.go). Um DTH_LANCTO não reconhecido faz o CHAMADOR tratar o lançamento como
// erro isolado (a coluna dth_lancto é NOT NULL), sem abortar o restante do arquivo.
func parseCGIBSParserTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range cgibsParserTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, cgibsParserLocation); err == nil {
			return &t
		}
	}
	log.Printf("[CGIBS Parser] AVISO: data/hora %q não reconhecida em nenhum formato conhecido", s)
	return nil
}

// markCGIBSArquivoErro marca cgibs_arquivos como 'erro' com a mensagem dada, usando uma conexão
// NOVA do pool (`db`, nunca uma tx em andamento). O CHAMADOR é responsável por garantir que
// qualquer transação aberta sobre essa mesma linha já foi desfeita (tx.Rollback() explícito)
// ANTES de chamar esta função — ver abortCGIBSArquivo — caso contrário esta conexão nova trava
// esperando o lock que só o rollback da tx antiga libera (item 1 da revisão adversarial: risco
// de deadlock/esgotamento do pool sem timeout).
//
// Devolve o erro do próprio UPDATE (item 10) em vez de só logar — o chamador incorpora essa
// falha na mensagem de erro final que ProcessarArquivoCGIBS devolve, pra não ficar invisível no
// log do dispatch/goroutine.
func markCGIBSArquivoErro(db *sql.DB, arquivoID, msg string) error {
	_, err := db.Exec(`
		UPDATE cgibs_arquivos SET status = 'erro', error_message = $1 WHERE id = $2
	`, TruncateRunes(msg, 1000), arquivoID)
	if err != nil {
		log.Printf("[CGIBS Parser] AVISO: falha ao marcar arquivo %s como erro: %v (mensagem original: %s)", arquivoID, err, msg)
	}
	return err
}

// abortCGIBSArquivo trata uma falha de infraestrutura do arquivo inteiro (não isolável por
// item/SAVEPOINT, ex.: falha ao abrir a tx, falha na query inicial, SAVEPOINT que não pôde ser
// criado, ROLLBACK TO SAVEPOINT que falhou, commit final que falhou). Desfaz EXPLICITAMENTE a
// transação em andamento (se houver) ANTES de marcar o arquivo como erro numa conexão nova —
// item 1 da revisão adversarial. Chamar Rollback() 2x é seguro em database/sql (a 2ª chamada,
// inclusive a do `defer tx.Rollback()` do chamador, vira no-op).
//
// Sempre devolve um erro não-nil, incorporando a falha do próprio UPDATE de erro quando ela
// também ocorre (item 10) — garante que ProcessarArquivoCGIBS nunca retorna nil nesse caminho.
func abortCGIBSArquivo(db *sql.DB, tx *sql.Tx, arquivoID, msg string) error {
	if tx != nil {
		tx.Rollback()
	}
	if markErr := markCGIBSArquivoErro(db, arquivoID, msg); markErr != nil {
		return fmt.Errorf("cgibs processar arquivo %s: %s (também falhou ao marcar erro: %v)", arquivoID, msg, markErr)
	}
	return fmt.Errorf("cgibs processar arquivo %s: %s", arquivoID, msg)
}

// upsertCGIBSOperacao grava/atualiza uma linha de cgibs_operacoes por (company_id,
// chave_acesso) e devolve o id da linha (nova ou existente). Dados descritivos (dth_emissao/
// dth_autorizacao/cnpj_fornecedor/cnpj_adquirente/extrato_hash) só avançam quando o arquivo novo
// TRAZ um valor (item 14 da revisão adversarial: COALESCE com o valor já salvo — um NULL do
// arquivo novo não pode apagar um valor bom já gravado por um arquivo anterior); já
// ultimo_arquivo_id/updated_at sempre avançam pro arquivo mais recente processado, sem
// COALESCE. Só os lançamentos são append-only de verdade (Boundaries da spec).
// Uma falha aqui (ex.: CHECK de chave_acesso length=44 ou CHECK cnpj_fornecedor/adquirente,
// migration 131) é devolvida ao chamador, que isola o erro via SAVEPOINT sem abortar as demais
// operações do arquivo.
func upsertCGIBSOperacao(tx *sql.Tx, companyID, arquivoID string, op CGIBSOperacaoJSON) (string, error) {
	var dthEmissao, dthAutorizacao interface{}
	if t := parseCGIBSParserTime(op.DthEmissao); t != nil {
		dthEmissao = *t
	}
	if t := parseCGIBSParserTime(op.DthAutorizacao); t != nil {
		dthAutorizacao = *t
	}
	var cnpjFornecedor, cnpjAdquirente interface{}
	if op.CNPJFornecedor != "" {
		cnpjFornecedor = op.CNPJFornecedor
	}
	if op.CNPJAdquirente != "" {
		cnpjAdquirente = op.CNPJAdquirente
	}
	var extratoHash interface{}
	if op.ExtratoCC != nil && op.ExtratoCC.Hash != "" {
		extratoHash = op.ExtratoCC.Hash
	}
	var operacaoIDExterno interface{}
	if op.ID != nil {
		operacaoIDExterno = int64(*op.ID)
	}

	var id string
	err := tx.QueryRow(`
		INSERT INTO cgibs_operacoes (
			company_id, operacao_id_externo, chave_acesso, dth_emissao, dth_autorizacao,
			cnpj_fornecedor, cnpj_adquirente, extrato_hash, ultimo_arquivo_id, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (company_id, chave_acesso) DO UPDATE SET
			dth_emissao = COALESCE(EXCLUDED.dth_emissao, cgibs_operacoes.dth_emissao),
			dth_autorizacao = COALESCE(EXCLUDED.dth_autorizacao, cgibs_operacoes.dth_autorizacao),
			cnpj_fornecedor = COALESCE(EXCLUDED.cnpj_fornecedor, cgibs_operacoes.cnpj_fornecedor),
			cnpj_adquirente = COALESCE(EXCLUDED.cnpj_adquirente, cgibs_operacoes.cnpj_adquirente),
			extrato_hash = COALESCE(EXCLUDED.extrato_hash, cgibs_operacoes.extrato_hash),
			ultimo_arquivo_id = EXCLUDED.ultimo_arquivo_id,
			updated_at = NOW()
		RETURNING id
	`, companyID, operacaoIDExterno, op.ChaveAcesso, dthEmissao, dthAutorizacao,
		cnpjFornecedor, cnpjAdquirente, extratoHash, arquivoID,
	).Scan(&id)
	return id, err
}

// insertCGIBSLancamento insere um lançamento do extrato_cc — append-only, nunca atualiza um
// já visto (ON CONFLICT (operacao_id, lancamento_id_externo) DO NOTHING, conforme o MOC:
// "linha nova complementa, não altera anteriores"). ID do lançamento é obrigatório (MOC:
// "sempre presente e único por linha") — ausente/nil vira erro do item isolado (item 6 da
// revisão adversarial), nunca um silencioso 0.
func insertCGIBSLancamento(tx *sql.Tx, companyID, operacaoID, arquivoID string, lanc CGIBSLancamentoJSON) error {
	if lanc.ID == nil {
		return fmt.Errorf("ID do lançamento ausente (campo obrigatório pelo MOC)")
	}
	dthLancto := parseCGIBSParserTime(lanc.DthLancto)
	if dthLancto == nil {
		return fmt.Errorf("DTH_LANCTO ausente/não reconhecido (%q)", lanc.DthLancto)
	}
	var movCodigo interface{}
	if lanc.Mov != nil {
		movCodigo = int64(*lanc.Mov)
	}
	res, err := tx.Exec(`
		INSERT INTO cgibs_lancamentos (
			company_id, operacao_id, lancamento_id_externo, dth_lancto, mov_codigo, mov_descricao,
			recurso_financeiro_disponivel, recurso_financeiro_a_transferir, credito_a_apropriar,
			credito_nao_utilizado, credito_utilizado, debito_em_aberto, debito_extinto, arquivo_id
		) VALUES ($1, $2, $3, $4, $5, NULL, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (operacao_id, lancamento_id_externo) DO NOTHING
	`, companyID, operacaoID, int64(*lanc.ID), *dthLancto, movCodigo,
		float64(lanc.RecursoFinanceiroDisponivel), float64(lanc.RecursoFinanceiroATransferir),
		float64(lanc.CreditoAApropriar), float64(lanc.CreditoNaoUtilizado), float64(lanc.CreditoUtilizado),
		float64(lanc.DebitoEmAberto), float64(lanc.DebitoExtinto), arquivoID,
	)
	if err != nil {
		return err
	}
	// Item 7 da revisão adversarial: ON CONFLICT DO NOTHING não pode ficar silencioso — não é
	// erro (o lançamento já existia, append-only funcionando como esperado num reprocessamento),
	// mas precisa ficar visível no log, diferente de uma inserção nova bem-sucedida.
	if n, raErr := res.RowsAffected(); raErr == nil && n == 0 {
		log.Printf("[CGIBS Parser] INFO: lançamento externo %d da operação %s (arquivo %s) já existia — ON CONFLICT DO NOTHING, não sobrescrito (append-only)",
			int64(*lanc.ID), operacaoID, arquivoID)
	}
	return nil
}

// ProcessarArquivoCGIBS baixa (services.ObterArquivo) e processa um arquivo de conta corrente
// fiscal (cgibs_arquivos), gravando cgibs_operacoes/cgibs_lancamentos. Disparada em goroutine
// pelo webhook (handlers.CGIBSWebhookHandler) para cada cgibs_arquivos novo ('pendente').
//
// Claim atômico (item 2 da revisão adversarial): TUDO roda dentro de uma única transação,
// aberta logo no início, que faz `SELECT ... FOR UPDATE` na linha de cgibs_arquivos antes de
// qualquer outra coisa. Se o status não for mais 'pendente' (outra chamada concorrente — ex.:
// webhook reentregue pela CGIBS — já reivindicou ou terminou de processar este arquivo), esta
// chamada só commita a tx vazia (libera o lock sem mudar nada) e devolve nil: idempotente, sem
// duplicar operações/lançamentos. Como o schema (migration 131) só aceita status
// 'pendente'|'baixado'|'erro' (sem um valor "processando"), manter a MESMA transação aberta com
// o lock FOR UPDATE por toda a duração do processamento (incluindo a chamada de rede a
// ObterArquivo) é o que impede uma 2ª chamada concorrente de ler 'pendente' e processar de novo
// — é um trade-off aceito (conexão do pool + lock de linha seguram por mais tempo que o
// ideal) para não precisar de coluna nova nem de um mecanismo de claim em 2 etapas.
//
// Erro ao processar uma operação (ou um lançamento) específica não aborta as demais — cada item
// roda sob seu próprio SAVEPOINT dentro da mesma transação do arquivo (mesmo padrão já usado em
// handlers/erp_bridge_batch.go para isolar erro por documento de um lote): uma falha de
// constraint (ex.: chave_acesso com tamanho diferente de 44) só desfaz aquele item via ROLLBACK
// TO SAVEPOINT, sem deixar a transação inteira "abortada" no Postgres. Só um erro de
// infraestrutura (falha ao abrir/commitar a transação, savepoint que não pôde ser criado, etc.)
// aborta o arquivo inteiro — ver abortCGIBSArquivo.
func ProcessarArquivoCGIBS(db *sql.DB, arquivoID string) error {
	log.Printf("[CGIBS Parser] Iniciando processamento do arquivo %s", arquivoID)

	tx, err := db.Begin()
	if err != nil {
		// Item 20: nada foi aberto ainda — abortCGIBSArquivo com tx=nil só marca erro (não há
		// nada pra desfazer).
		return abortCGIBSArquivo(db, nil, arquivoID, "erro ao iniciar transação: "+err.Error())
	}
	defer tx.Rollback()

	var companyID, status string
	var numeroSequencial int64
	var idSolicitacaoExterno sql.NullInt64
	err = tx.QueryRow(`
		SELECT a.company_id, a.numero_sequencial, a.status, s.id_solicitacao_externo
		FROM cgibs_arquivos a
		JOIN cgibs_solicitacoes s ON s.id = a.solicitacao_id
		WHERE a.id = $1
		FOR UPDATE OF a
	`, arquivoID).Scan(&companyID, &numeroSequencial, &status, &idSolicitacaoExterno)
	if err != nil {
		// Item 20: falha na query inicial (ex.: falha transitória de banco, ou arquivoID
		// inexistente) não pode deixar a linha 'pendente' pra sempre sem nunca marcar erro.
		return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("buscar arquivo/solicitação: %v", err))
	}

	if status != "pendente" {
		// Claim já consumido por outra chamada (webhook reentregue) ou arquivo já processado
		// (baixado/erro) — idempotente, não reprocessa. Libera o lock sem mudar nada.
		log.Printf("[CGIBS Parser] Arquivo %s já está em status=%q — ignorando (idempotente, evita duplicar operações/lançamentos)", arquivoID, status)
		return tx.Commit()
	}

	// Item 11: numero_sequencial inválido não deveria nem tentar a chamada de rede.
	if numeroSequencial <= 0 {
		return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("numero_sequencial inválido (%d) — não é possível obter arquivo", numeroSequencial))
	}

	if !idSolicitacaoExterno.Valid || idSolicitacaoExterno.Int64 <= 0 {
		return abortCGIBSArquivo(db, tx, arquivoID, "id_solicitacao_externo ausente/inválido na solicitação vinculada")
	}

	var clientID, clientSecretEnc string
	// Item 18: exige habilitado=true além de ativo=true — mesma correção já aplicada em
	// matchCGIBSCredential (cgibs_webhook.go) na sub-spec anterior, faltava replicar aqui: uma
	// credencial nunca confirmada pela CGIBS não deveria conseguir baixar arquivo nenhum.
	err = tx.QueryRow(`
		SELECT client_id, client_secret FROM cgibs_credentials
		WHERE company_id = $1 AND ativo = true AND habilitado = true
	`, companyID).Scan(&clientID, &clientSecretEnc)
	if err != nil {
		msg := "credencial CGIBS ativa e habilitada não encontrada para a empresa"
		if !errors.Is(err, sql.ErrNoRows) {
			msg = "erro ao consultar credencial CGIBS: " + err.Error()
		}
		return abortCGIBSArquivo(db, tx, arquivoID, msg)
	}
	clientSecret := crypto.DecryptFieldWithFallback(clientSecretEnc)

	rawJSON, err := ObterArquivo(clientID, clientSecret, idSolicitacaoExterno.Int64, numeroSequencial)
	if err != nil {
		return abortCGIBSArquivo(db, tx, arquivoID, "erro ao obter arquivo: "+err.Error())
	}

	// Salva raw_json IMEDIATAMENTE após o download, ANTES do parse — mesmo princípio de
	// rfb_processor.go (ProcessarDownloadRFB): dado recebido não pode se perder por causa de uma
	// falha de parse posterior. Grava DENTRO da mesma tx (já é o dono do lock FOR UPDATE desta
	// linha, então não há risco de deadlock aqui — é a MESMA conexão/transação escrevendo, não
	// uma nova). Não fatal se falhar (só loga) — o processamento continua.
	if _, saveErr := tx.Exec(`UPDATE cgibs_arquivos SET raw_json = $1 WHERE id = $2`, string(rawJSON), arquivoID); saveErr != nil {
		log.Printf("[CGIBS Parser] AVISO: falha ao salvar raw_json do arquivo %s: %v (processamento continua)", arquivoID, saveErr)
	}

	var parsed CGIBSArquivoJSON
	if err := json.Unmarshal(rawJSON, &parsed); err != nil {
		// O raw_json acima já foi gravado NESTA MESMA tx — se abortássemos a tx inteira aqui
		// (abortCGIBSArquivo faz ROLLBACK), o raw_json recém-gravado se perderia junto,
		// violando a garantia da spec ("falha no parse não perde o dado recebido"). Em vez
		// disso, marca erro e COMMITA a mesma tx (preserva o raw_json); só cai no caminho de
		// abort genérico se o próprio UPDATE de erro ou o commit falharem.
		msg := "erro ao parsear JSON do arquivo: " + err.Error()
		if _, uerr := tx.Exec(`UPDATE cgibs_arquivos SET status = 'erro', error_message = $1 WHERE id = $2`,
			TruncateRunes(msg, 1000), arquivoID); uerr != nil {
			return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("%s (também falhou ao marcar erro: %v)", msg, uerr))
		}
		if cerr := tx.Commit(); cerr != nil {
			return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("%s (também falhou ao confirmar transação: %v)", msg, cerr))
		}
		return fmt.Errorf("cgibs processar arquivo %s: %s", arquivoID, msg)
	}

	// Item 16: qtdoperacoes declarado no header vs. operações realmente recebidas — não falha o
	// processamento (pode ser só uma inconsistência do lado da CGIBS), mas fica visível no log
	// como possível sinal de payload truncado.
	if parsed.ParametrosGeracao.QtdOperacoes != len(parsed.Operacoes) {
		log.Printf("[CGIBS Parser] AVISO: arquivo %s declara parametrosgeracao.qtdoperacoes=%d mas o payload trouxe %d operações (possível payload truncado)",
			arquivoID, parsed.ParametrosGeracao.QtdOperacoes, len(parsed.Operacoes))
	}

	operacoesComErro := 0
	lancamentosComErro := 0
	for i, op := range parsed.Operacoes {
		spOp := fmt.Sprintf("cgibs_op_%d", i)
		if _, err := tx.Exec("SAVEPOINT " + spOp); err != nil {
			// Item 8: falha ao CRIAR o savepoint em si é infraestrutura (não item isolável) —
			// aborta o arquivo inteiro em vez de retornar sem marcar erro.
			return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("savepoint operação %d: %v", i, err))
		}

		operacaoID, upErr := upsertCGIBSOperacao(tx, companyID, arquivoID, op)
		if upErr != nil {
			log.Printf("[CGIBS Parser] AVISO: erro ao gravar operação %d (chave=%q) do arquivo %s: %v — item isolado, demais operações seguem",
				i, op.ChaveAcesso, arquivoID, upErr)
			operacoesComErro++
			if _, rbErr := tx.Exec("ROLLBACK TO SAVEPOINT " + spOp); rbErr != nil {
				// Item 9: se o ROLLBACK TO SAVEPOINT falhar, a transação provavelmente já está
				// abortada no Postgres — trata como falha de infraestrutura do arquivo inteiro,
				// não tenta continuar o loop.
				return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("rollback to savepoint operação %d: %v (transação provavelmente abortada)", i, rbErr))
			}
			if _, relErr := tx.Exec("RELEASE SAVEPOINT " + spOp); relErr != nil {
				log.Printf("[CGIBS Parser] AVISO: falha ao liberar savepoint da operação %d do arquivo %s: %v", i, arquivoID, relErr)
			}
			continue
		}
		if _, relErr := tx.Exec("RELEASE SAVEPOINT " + spOp); relErr != nil {
			log.Printf("[CGIBS Parser] AVISO: falha ao liberar savepoint da operação %d do arquivo %s: %v", i, arquivoID, relErr)
		}

		if op.ExtratoCC == nil {
			continue
		}
		for j, lanc := range op.ExtratoCC.Lista {
			spLanc := fmt.Sprintf("cgibs_op_%d_lanc_%d", i, j)
			if _, err := tx.Exec("SAVEPOINT " + spLanc); err != nil {
				return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("savepoint lançamento %d/%d: %v", i, j, err))
			}
			if lErr := insertCGIBSLancamento(tx, companyID, operacaoID, arquivoID, lanc); lErr != nil {
				log.Printf("[CGIBS Parser] AVISO: erro ao gravar lançamento %d da operação %d (chave=%q) do arquivo %s: %v — item isolado, demais lançamentos seguem",
					j, i, op.ChaveAcesso, arquivoID, lErr)
				lancamentosComErro++
				if _, rbErr := tx.Exec("ROLLBACK TO SAVEPOINT " + spLanc); rbErr != nil {
					return abortCGIBSArquivo(db, tx, arquivoID, fmt.Sprintf("rollback to savepoint lançamento %d/%d: %v (transação provavelmente abortada)", i, j, rbErr))
				}
				if _, relErr := tx.Exec("RELEASE SAVEPOINT " + spLanc); relErr != nil {
					log.Printf("[CGIBS Parser] AVISO: falha ao liberar savepoint do lançamento %d/%d do arquivo %s: %v", i, j, arquivoID, relErr)
				}
				continue
			}
			if _, relErr := tx.Exec("RELEASE SAVEPOINT " + spLanc); relErr != nil {
				log.Printf("[CGIBS Parser] AVISO: falha ao liberar savepoint do lançamento %d/%d do arquivo %s: %v", i, j, arquivoID, relErr)
			}
		}
	}

	// Item 15: falha total (todas as operações do payload falharam) não pode virar 'baixado'
	// silenciosamente — só falha parcial mantém 'baixado' (dado real foi gravado), com
	// error_message resumindo o que falhou (migration 131 permite error_message preenchido com
	// status='baixado'; só EXIGE preenchido quando status='erro' — CHECK (status <> 'erro' OR
	// error_message IS NOT NULL) não impede o contrário).
	totalOperacoes := len(parsed.Operacoes)
	finalStatus := "baixado"
	var errorMessageParam interface{}
	switch {
	case totalOperacoes > 0 && operacoesComErro == totalOperacoes:
		finalStatus = "erro"
		errorMessageParam = fmt.Sprintf("todas as %d operações falharam ao processar", totalOperacoes)
	case operacoesComErro > 0 || lancamentosComErro > 0:
		errorMessageParam = fmt.Sprintf("%d de %d operações falharam (%d lançamentos com erro isolado)",
			operacoesComErro, totalOperacoes, lancamentosComErro)
	}

	var updateErr error
	if finalStatus == "erro" {
		_, updateErr = tx.Exec(`
			UPDATE cgibs_arquivos SET status = 'erro', error_message = $1
			WHERE id = $2
		`, errorMessageParam, arquivoID)
	} else {
		_, updateErr = tx.Exec(`
			UPDATE cgibs_arquivos SET status = 'baixado', baixado_em = NOW(), error_message = $1
			WHERE id = $2
		`, errorMessageParam, arquivoID)
	}
	if updateErr != nil {
		return abortCGIBSArquivo(db, tx, arquivoID, "erro ao marcar status final do arquivo: "+updateErr.Error())
	}

	if err := tx.Commit(); err != nil {
		return abortCGIBSArquivo(db, tx, arquivoID, "erro ao confirmar transação: "+err.Error())
	}

	if finalStatus == "erro" {
		log.Printf("[CGIBS Parser] Arquivo %s: TODAS as %d operações falharam — status=erro", arquivoID, totalOperacoes)
		return fmt.Errorf("cgibs processar arquivo %s: %v", arquivoID, errorMessageParam)
	}

	log.Printf("[CGIBS Parser] Arquivo %s processado: %d operações no payload, %d com erro isolado, %d lançamentos com erro isolado",
		arquivoID, totalOperacoes, operacoesComErro, lancamentosComErro)
	return nil
}

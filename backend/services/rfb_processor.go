package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// dbExecutor abstracts *sql.DB and *sql.Tx so insertDebito works in both contexts.
type dbExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// FlexString unmarshals both JSON strings and JSON numbers into a Go string.
// Needed because the RFB API returns some fields (e.g. modeloDfe=55) as numbers.
type FlexString string

func (fs *FlexString) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*fs = FlexString(s)
	return nil
}

// RFBTime handles RFB datetime strings that may lack timezone suffix (e.g. "2026-03-01T08:30:09").
type RFBTime struct {
	T *time.Time
}

func (rt *RFBTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "null" || s == "" {
		rt.T = nil
		return nil
	}
	for _, format := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.999999999",
	} {
		if t, err := time.Parse(format, s); err == nil {
			rt.T = &t
			return nil
		}
	}
	// Log and ignore unparseable dates rather than failing the whole import
	log.Printf("[RFB Processor] WARNING: could not parse datetime '%s', storing as nil", s)
	rt.T = nil
	return nil
}

// RFB JSON structures matching the v1 API response layout
type RFBApuracaoJSON struct {
	ApuracaoCorrente     *RFBGrupoDebitos `json:"apuracaoCorrente"`
	ApuracaoAjuste       *RFBGrupoDebitos `json:"apuracaoAjuste"`
	DebitosExtemporaneos *RFBGrupoDebitos `json:"debitosExtemporaneos"`
}

type RFBGrupoDebitos struct {
	Debitos  []RFBDebito  `json:"debitos"`
	Creditos []RFBCredito `json:"creditos"`
}

// RFBDebito é a struct canônica de um débito — usada tanto como alvo direto do
// Unmarshal do shape v1 (tags json abaixo) quanto como formato de saída do
// adapter v2 (adaptDebitoV2), que preenche os campos manualmente. Os campos
// "Campos v2" só são populados quando o item vem do shape v2 (migration 129);
// ficam nil para linhas v1, gravando NULL em rfb_debitos.
type RFBDebito struct {
	ModeloDfe          FlexString      `json:"modeloDfe"`
	NumeroDfe          FlexString      `json:"numeroDfe"`
	ChaveDfe           FlexString      `json:"chaveDfe"`
	DataDfeEmissao     *RFBTime        `json:"dataDfeEmissao"`
	DataDfeAutorizacao *RFBTime        `json:"dataDfeAutorizacao"`
	DataDfeRegistro    *RFBTime        `json:"dataDfeRegistro"`
	DataApuracao       string          `json:"dataApuracao"`
	NiEmitente         FlexString      `json:"niEmitente"`
	NiAdquirente       FlexString      `json:"niAdquirente"`
	ValorCBSTotal      float64         `json:"valorCBSTotal"`
	ValorCBSExtinto    float64         `json:"valorCBSExtinto"`
	ValorCBSNaoExtinto float64         `json:"valorCBSNaoExtinto"`
	SituacaoDebito     FlexString      `json:"situacao"`
	FormasExtincao     json.RawMessage `json:"formasExtincao"`
	Eventos            json.RawMessage `json:"eventos"`

	// Campos v2 (rfb_debitos, migration 129) — nil quando o item vem do shape v1.
	Origem               *int       `json:"-"`
	Documento            *int       `json:"-"`
	DataRegistro         *time.Time `json:"-"`
	DataAtualizacao      *time.Time `json:"-"`
	ValorCBSExcedente    *float64   `json:"-"`
	ValorCBSInexigivel   *float64   `json:"-"`
	ValorCBSSuspenso     *float64   `json:"-"`
	ValorCBSSaldoDevedor *float64   `json:"-"`
}

// ── Shape detection (v1 vs v2) ──────────────────────────────────────────────
//
// A RFB republicou a API de apuração CBS como v2 (corte anunciado para out/2026,
// ver spec-rfb-cbs-v2-migracao.md): o shape de resposta muda de 3 blocos fixos
// (apuracaoCorrente/apuracaoAjuste/debitosExtemporaneos) para uma lista plana
// agrupada por período (apuracao[].pa), podendo trazer MÚLTIPLOS períodos numa
// única resposta. O parser precisa suportar os dois formatos simultaneamente —
// inclusive para ReprocessarRawJSON(CreditosRFB), que relê raw_json histórico
// que pode ter sido salvo em qualquer um dos dois shapes.

// rfbShapeProbe é um probe leve do JSON bruto — só as chaves de topo relevantes
// para decidir o shape, via json.RawMessage (evita dois Unmarshal completos).
type rfbShapeProbe struct {
	ApuracaoCorrente     json.RawMessage `json:"apuracaoCorrente"`
	ApuracaoAjuste       json.RawMessage `json:"apuracaoAjuste"`
	DebitosExtemporaneos json.RawMessage `json:"debitosExtemporaneos"`
	Apuracao             json.RawMessage `json:"apuracao"`
}

// detectRFBShape decide entre os parsers v1 e v2 a partir das chaves de topo
// presentes no JSON bruto, sem fazer o Unmarshal completo em nenhuma das duas
// structs canônicas. Default "v1" quando nenhuma chave reconhecida aparece
// (ex.: payload vazio "{}") — preserva o comportamento pré-v2 desse caso de
// borda (ver TestProcessarDownloadRFB_PeriodoVazioZeros).
func detectRFBShape(raw []byte) string {
	var probe rfbShapeProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "v1"
	}
	if probe.ApuracaoCorrente != nil || probe.ApuracaoAjuste != nil || probe.DebitosExtemporaneos != nil {
		if probe.Apuracao != nil {
			log.Printf("[RFB Processor] AVISO: payload híbrido (chaves v1 + 'apuracao' v2) — usando v1, bloco 'apuracao' v2 ignorado")
		}
		return "v1"
	}
	if probe.Apuracao != nil {
		if string(probe.Apuracao) == "null" {
			log.Printf("[RFB Processor] resposta v2 sem períodos ('apuracao': null) — tratada como v2 vazio")
		}
		return "v2"
	}
	return "v1"
}

// normalizeDataApuracao converte o formato v1 "AAAAMM" (6 dígitos) para o
// formato canônico de armazenamento "mm/aaaa" (migration 129). Valores que já
// estão em "mm/aaaa" (v2, ou reprocessamento de dado já migrado) e qualquer
// valor que não seja exatamente 6 dígitos passam sem alteração.
func normalizeDataApuracao(raw string) string {
	if len(raw) != 6 {
		return raw
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return raw
		}
	}
	// mês inválido (ex.: "202613"): devolve o original sem converter — o
	// chamador detecta via periodoValido e não grava sob período inválido.
	if m, _ := strconv.Atoi(raw[4:6]); m < 1 || m > 12 {
		return raw
	}
	return raw[4:6] + "/" + raw[0:4]
}

// periodoValido informa se p está no formato canônico "mm/aaaa" com mês 01-12.
func periodoValido(p string) bool {
	if len(p) != 7 || p[2] != '/' {
		return false
	}
	for i, r := range p {
		if i == 2 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	m, _ := strconv.Atoi(p[0:2])
	return m >= 1 && m <= 12
}

// normalizeApuracaoV1DataApuracao normaliza DataApuracao (in place) de todos os
// débitos/créditos embutidos nos 3 blocos do shape v1, logo após o Unmarshal —
// garante que rfb_debitos/rfb_resumo (e rfb_creditos/rfb_creditos_resumo, via
// créditos embutidos) sempre gravem "mm/aaaa", nunca mais "AAAAMM", fechando a
// coexistência de formatos que causou o revert da 1ª tentativa desta spec.
func normalizeApuracaoV1DataApuracao(apuracao *RFBApuracaoJSON) {
	for _, g := range []*RFBGrupoDebitos{apuracao.ApuracaoCorrente, apuracao.ApuracaoAjuste, apuracao.DebitosExtemporaneos} {
		if g == nil {
			continue
		}
		for i := range g.Debitos {
			g.Debitos[i].DataApuracao = normalizeDataApuracao(g.Debitos[i].DataApuracao)
		}
		for i := range g.Creditos {
			g.Creditos[i].DataApuracao = normalizeDataApuracao(g.Creditos[i].DataApuracao)
		}
	}
}

// parsePeriodoMMYYYY interpreta uma string "mm/aaaa" (formato canônico de
// data_apuracao pós-migration 129) e retorna mês e ano. ok=false se o formato
// não bater.
func parsePeriodoMMYYYY(p string) (month, year int, ok bool) {
	parts := strings.Split(p, "/")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 4 {
		return 0, 0, false
	}
	m, errM := strconv.Atoi(parts[0])
	y, errY := strconv.Atoi(parts[1])
	if errM != nil || errY != nil || m < 1 || m > 12 {
		return 0, 0, false
	}
	return m, y, true
}

// bucketTipoApuracaoV2 aplica a heurística de bucketing por data aprovada para
// o shape v2 (que não tem os blocos estruturais apuracaoCorrente/apuracaoAjuste/
// debitosExtemporaneos de onde o v1 deriva tipo_apuracao): compara mês/ano do
// período (pa) com mês/ano de registro do item. Mesmo mês → "corrente". Registro
// em mês posterior ao período → "extemporaneo" — funde o que no v1 era
// ajuste+extemporaneo, já que não dá pra distinguir os dois só pela data
// (decisão registrada em spec-rfb-cbs-v2-migracao.md, decisão 2). Sem data de
// registro ou não parseável, assume "corrente" como default conservador.
func bucketTipoApuracaoV2(pa string, registro *time.Time) string {
	paMonth, paYear, ok := parsePeriodoMMYYYY(pa)
	if !ok || registro == nil {
		return "corrente"
	}
	paSeq := paYear*12 + paMonth
	regSeq := registro.Year()*12 + int(registro.Month())
	if regSeq > paSeq {
		return "extemporaneo"
	}
	return "corrente"
}

// ── RFB v2 (débitos) ────────────────────────────────────────────────────────

// RFBApuracaoV2JSON é o shape v2 do endpoint apuracao-cbs/v2/debitos: lista
// plana agrupada por período (pa), podendo conter MÚLTIPLOS períodos numa única
// resposta — diferente do v1, que assume 1 dataApuracao por chamada.
type RFBApuracaoV2JSON struct {
	Apuracao []RFBGrupoApuracaoV2 `json:"apuracao"`
}

type RFBGrupoApuracaoV2 struct {
	PA      string        `json:"pa"`
	Debitos []RFBDebitoV2 `json:"debitos"`
}

// RFBDebitoV2 é o item de débito no shape v2 — schema mais enxuto que o v1, sem
// modeloDfe/numeroDfe/niEmitente/niAdquirente/situacao explícitos (as queries de
// leitura já derivam modelo/número/série de chave_dfe quando a coluna vem vazia,
// ver rfb_debitos_lista.go).
type RFBDebitoV2 struct {
	Origem      int            `json:"origem"`
	Documento   int            `json:"documento"`
	Chave       string         `json:"chave"`
	Emissao     *RFBTime       `json:"emissao"`
	Registro    *RFBTime       `json:"registro"`
	Atualizacao *RFBTime       `json:"atualizacao"`
	CBS         RFBCBSV2Debito `json:"cbs"`
}

type RFBCBSV2Debito struct {
	Apurado      float64 `json:"apurado"`
	Excedente    float64 `json:"excedente"`
	Inexigivel   float64 `json:"inexigivel"`
	Suspenso     float64 `json:"suspenso"`
	Extinto      float64 `json:"extinto"`
	SaldoDevedor float64 `json:"saldoDevedor"`
}

// adaptDebitoV2 converte um item v2 para a struct canônica RFBDebito, para
// reaproveitar insertDebito sem duplicar a lógica de gravação. Campos sem
// contraparte direta em v2 ficam zerados (ModeloDfe, NumeroDfe, NiEmitente,
// NiAdquirente, SituacaoDebito, FormasExtincao, Eventos).
//
// Mapeamento monetário (aproximação documentada na spec — Design Notes — a
// validar com volume real de produção v2):
//   - valor_cbs_total       = cbs.apurado
//   - valor_cbs_extinto     = cbs.extinto
//   - valor_cbs_nao_extinto = cbs.saldoDevedor
func adaptDebitoV2(pa string, item RFBDebitoV2) RFBDebito {
	origem := item.Origem
	documento := item.Documento
	var dataRegistro, dataAtualizacao *time.Time
	if item.Registro != nil {
		dataRegistro = item.Registro.T
	}
	if item.Atualizacao != nil {
		dataAtualizacao = item.Atualizacao.T
	}
	excedente := item.CBS.Excedente
	inexigivel := item.CBS.Inexigivel
	suspenso := item.CBS.Suspenso
	saldoDevedor := item.CBS.SaldoDevedor

	return RFBDebito{
		ChaveDfe:             FlexString(item.Chave),
		DataDfeEmissao:       item.Emissao,
		DataApuracao:         pa,
		ValorCBSTotal:        item.CBS.Apurado,
		ValorCBSExtinto:      item.CBS.Extinto,
		ValorCBSNaoExtinto:   saldoDevedor,
		Origem:               &origem,
		Documento:            &documento,
		DataRegistro:         dataRegistro,
		DataAtualizacao:      dataAtualizacao,
		ValorCBSExcedente:    &excedente,
		ValorCBSInexigivel:   &inexigivel,
		ValorCBSSuspenso:     &suspenso,
		ValorCBSSaldoDevedor: &saldoDevedor,
	}
}

// processV2Debitos insere os itens de débito do shape v2 (já parseado) e roda a
// agregação SQL + UPSERT de rfb_resumo UMA VEZ POR PERÍODO (pa) distinto
// encontrado em apuracao[] — diferente do caminho v1, que assume um único
// dataApuracao para toda a resposta (ver Boundaries da spec). Não faz rollback
// nem updateRequestError — o chamador decide isso a partir do erro retornado,
// mesmo padrão dos demais pontos de falha em ProcessarDownloadRFB/
// ReprocessarRawJSON.
func processV2Debitos(tx *sql.Tx, requestID, companyID string, apuracao RFBApuracaoV2JSON) (insertErrors int, err error) {
	type periodAgg struct {
		semChaveCorrente, semChaveExtemporaneo rfbAggBucket
	}
	var periodOrder []string
	periods := map[string]*periodAgg{}

	for _, grupo := range apuracao.Apuracao {
		// defensivo: v2 já traz "mm/aaaa", mas normaliza caso venha "AAAAMM".
		pa := normalizeDataApuracao(grupo.PA)
		if !periodoValido(pa) {
			insertErrors += len(grupo.Debitos)
			log.Printf("[RFB Processor v2] período inválido %q (request %s): %d débitos descartados", grupo.PA, requestID, len(grupo.Debitos))
			continue
		}
		agg, seen := periods[pa]
		if !seen {
			agg = &periodAgg{}
			periods[pa] = agg
			periodOrder = append(periodOrder, pa)
		}
		for _, item := range grupo.Debitos {
			var registro *time.Time
			if item.Registro != nil {
				registro = item.Registro.T
			}
			tipo := bucketTipoApuracaoV2(pa, registro)
			canonical := adaptDebitoV2(pa, item)
			if insErr := insertDebito(tx, requestID, companyID, tipo, canonical); insErr != nil {
				log.Printf("[RFB Processor v2] Error inserting %s debit (chave=%s, pa=%s): %v", tipo, item.Chave, pa, insErr)
				insertErrors++
				continue
			}
			if item.Chave == "" {
				bucket := &agg.semChaveCorrente
				if tipo == "extemporaneo" {
					bucket = &agg.semChaveExtemporaneo
				}
				bucket.Count++
				bucket.ValorTotal += canonical.ValorCBSTotal
				bucket.ValorExtinto += canonical.ValorCBSExtinto
				bucket.ValorNaoExtinto += canonical.ValorCBSNaoExtinto
			}
		}
	}

	for _, pa := range periodOrder {
		agg := periods[pa]
		sqlCorrente, sqlAjuste, sqlExtemporaneo, aggErr := aggregateRfbDebitosSQL(tx, companyID, pa)
		if aggErr != nil {
			return insertErrors, fmt.Errorf("aggregate rfb_debitos (pa=%s): %w", pa, aggErr)
		}
		totalCorrente := sqlCorrente.Count + agg.semChaveCorrente.Count
		totalAjuste := sqlAjuste.Count
		totalExtemporaneo := sqlExtemporaneo.Count + agg.semChaveExtemporaneo.Count
		valorTotal := sqlCorrente.ValorTotal + sqlAjuste.ValorTotal + sqlExtemporaneo.ValorTotal +
			agg.semChaveCorrente.ValorTotal + agg.semChaveExtemporaneo.ValorTotal
		valorExtinto := sqlCorrente.ValorExtinto + sqlAjuste.ValorExtinto + sqlExtemporaneo.ValorExtinto +
			agg.semChaveCorrente.ValorExtinto + agg.semChaveExtemporaneo.ValorExtinto
		valorNaoExtinto := sqlCorrente.ValorNaoExtinto + sqlAjuste.ValorNaoExtinto + sqlExtemporaneo.ValorNaoExtinto +
			agg.semChaveCorrente.ValorNaoExtinto + agg.semChaveExtemporaneo.ValorNaoExtinto
		totalDebitos := totalCorrente + totalAjuste + totalExtemporaneo

		if _, upErr := tx.Exec(`
			INSERT INTO rfb_resumo (request_id, company_id, data_apuracao, total_debitos,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
				total_corrente, total_ajuste, total_extemporaneo)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (company_id, data_apuracao)
			DO UPDATE SET request_id = $1, total_debitos = $4,
				valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
				total_corrente = $8, total_ajuste = $9, total_extemporaneo = $10
		`, requestID, companyID, pa, totalDebitos,
			valorTotal, valorExtinto, valorNaoExtinto,
			totalCorrente, totalAjuste, totalExtemporaneo); upErr != nil {
			return insertErrors, fmt.Errorf("upsert rfb_resumo (pa=%s): %w", pa, upErr)
		}
		log.Printf("[RFB Processor v2] Resumo período %s: %d débitos (%d corrente, %d ajuste, %d extemporaneo), CBS R$ %.2f",
			pa, totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, valorTotal)
	}

	return insertErrors, nil
}

// rfbAggBucket holds aggregated totals for one tipo_apuracao bucket (count + monetary sums).
type rfbAggBucket struct {
	Count           int
	ValorTotal      float64
	ValorExtinto    float64
	ValorNaoExtinto float64
}

// aggregateRfbDebitosSQL agrega rfb_debitos por tipo_apuracao (corrente/ajuste/extemporaneo)
// para company_id+data_apuracao, dentro da tx corrente. Considera SOMENTE linhas com
// chave_dfe preenchida — essas são as únicas deduplicadas via UPSERT ON CONFLICT
// (company_id, chave_dfe); refletem o estado real acumulado da tabela para o período,
// independente do payload da chamada atual ser completo ou parcial (API incremental).
// Linhas sem chave_dfe NÃO têm dedup e por isso ficam de fora — o chamador deve somar
// separadamente, em memória, apenas os itens sem chave da chamada atual (ver
// spec-rfb-resumo-agregacao-sql.md, Spec Change Log / Loopback 1).
func aggregateRfbDebitosSQL(tx *sql.Tx, companyID, dataApuracao string) (corrente, ajuste, extemporaneo rfbAggBucket, err error) {
	rows, err := tx.Query(`
		SELECT tipo_apuracao,
		       COUNT(*),
		       COALESCE(SUM(valor_cbs_total), 0),
		       COALESCE(SUM(valor_cbs_extinto), 0),
		       COALESCE(SUM(valor_cbs_nao_extinto), 0)
		FROM rfb_debitos
		WHERE company_id = $1 AND data_apuracao = $2
		  AND chave_dfe IS NOT NULL AND chave_dfe <> ''
		GROUP BY tipo_apuracao
	`, companyID, dataApuracao)
	if err != nil {
		return corrente, ajuste, extemporaneo, fmt.Errorf("aggregate rfb_debitos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tipo string
		var b rfbAggBucket
		if scanErr := rows.Scan(&tipo, &b.Count, &b.ValorTotal, &b.ValorExtinto, &b.ValorNaoExtinto); scanErr != nil {
			return corrente, ajuste, extemporaneo, fmt.Errorf("scan rfb_debitos agg: %w", scanErr)
		}
		switch tipo {
		case "ajuste":
			ajuste = b
		case "extemporaneo":
			extemporaneo = b
		default:
			// "corrente" e qualquer tipo não mapeado caem no bucket corrente (fallback).
			corrente = b
		}
	}
	if err := rows.Err(); err != nil {
		return corrente, ajuste, extemporaneo, fmt.Errorf("iterate rfb_debitos agg: %w", err)
	}
	return corrente, ajuste, extemporaneo, nil
}

// aggregateRfbCreditosSQL agrega rfb_creditos por tipo_apuracao para company_id+data_apuracao,
// dentro da tx corrente, considerando somente linhas com chave_dfe preenchida (mesma lógica
// de dedup de aggregateRfbDebitosSQL). Bucket créditos: tipo_apuracao <> 'ajuste' (incluindo
// "extemporaneo" e a estrutura plana, que é sempre inserida como "corrente") conta em
// total_corrente — decisão do humano confirmada na spec, não é regressão.
func aggregateRfbCreditosSQL(tx *sql.Tx, companyID, dataApuracao string) (corrente, ajuste rfbAggBucket, err error) {
	// rfb_creditos.tipo_apuracao é VARCHAR(50) SEM NOT NULL (migration 096_rfb_creditos.sql,
	// diferente de rfb_debitos.tipo_apuracao que é NOT NULL) — COALESCE evita que um valor
	// NULL quebre o rows.Scan (o que abortaria a transação inteira via rollback).
	rows, err := tx.Query(`
		SELECT COALESCE(tipo_apuracao, ''),
		       COUNT(*),
		       COALESCE(SUM(valor_cbs_total), 0),
		       COALESCE(SUM(valor_cbs_extinto), 0),
		       COALESCE(SUM(valor_cbs_nao_extinto), 0)
		FROM rfb_creditos
		WHERE company_id = $1 AND data_apuracao = $2
		  AND chave_dfe IS NOT NULL AND chave_dfe <> ''
		GROUP BY COALESCE(tipo_apuracao, '')
	`, companyID, dataApuracao)
	if err != nil {
		return corrente, ajuste, fmt.Errorf("aggregate rfb_creditos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tipo string
		var b rfbAggBucket
		if scanErr := rows.Scan(&tipo, &b.Count, &b.ValorTotal, &b.ValorExtinto, &b.ValorNaoExtinto); scanErr != nil {
			return corrente, ajuste, fmt.Errorf("scan rfb_creditos agg: %w", scanErr)
		}
		if tipo == "ajuste" {
			ajuste = b
		} else {
			corrente.Count += b.Count
			corrente.ValorTotal += b.ValorTotal
			corrente.ValorExtinto += b.ValorExtinto
			corrente.ValorNaoExtinto += b.ValorNaoExtinto
		}
	}
	if err := rows.Err(); err != nil {
		return corrente, ajuste, fmt.Errorf("iterate rfb_creditos agg: %w", err)
	}
	return corrente, ajuste, nil
}

// ProcessarDownloadRFB downloads and processes the RFB CBS assessment JSON.
// It saves the raw JSON, normalizes debits into rfb_debitos, and creates a summary in rfb_resumo.
// All DB writes (debits + summary) are wrapped in a single transaction to prevent partial imports.
// An atomic status update prevents two goroutines from processing the same request concurrently.
func ProcessarDownloadRFB(db *sql.DB, rfbClient *RFBClient, requestID string) error {
	log.Printf("[RFB Processor] Starting download processing for request %s", requestID)

	// 1. Fetch request details and company credentials
	var companyID, tiquete, cnpjBase, ambiente string
	var tiqueteDownload, urlAssinada sql.NullString
	var urlExpiraEm sql.NullTime
	var apiVersao string
	err := db.QueryRow(`
		SELECT r.company_id, r.tiquete, r.cnpj_base, r.tiquete_download, COALESCE(r.ambiente, 'producao'),
		       COALESCE(r.api_versao, 'v1'), r.url_assinada, r.url_assinada_expira_em
		FROM rfb_requests r
		WHERE r.id = $1
	`, requestID).Scan(&companyID, &tiquete, &cnpjBase, &tiqueteDownload, &ambiente, &apiVersao, &urlAssinada, &urlExpiraEm)
	if err != nil {
		return fmt.Errorf("failed to fetch request: %w", err)
	}

	var clientID, clientSecret string
	err = db.QueryRow(`
		SELECT client_id, client_secret FROM rfb_credentials
		WHERE company_id = $1 AND ativo = true
	`, companyID).Scan(&clientID, &clientSecret)
	if err != nil {
		updateRequestError(db, requestID, "CRED_NOT_FOUND", "Credenciais RFB não encontradas ou inativas")
		return fmt.Errorf("failed to fetch credentials: %w", err)
	}

	// Ambiente da SOLICITAÇÃO, não do cadastro da credencial: se o cadastro mudar entre
	// solicitar e baixar, o download precisa ir no mesmo prefixo (rtc/prr-rtc) do tíquete.
	log.Printf("[RFB Processor] Request %s — ambiente da solicitação: %s", requestID, ambiente)
	rfbClient.SetAmbiente(ambiente)
	// Versão da API da SOLICITAÇÃO (não a config atual): v2 baixa por urlAssinada/situacao.
	rfbClient.SetAPIVersao(apiVersao)
	dlInput := newRFBDownloadInput(tiquete, apiVersao, tiqueteDownload, urlAssinada, urlExpiraEm)
	dlInput.OnURLRenovada = persistURLRenovada(db, requestID)
	log.Printf("[RFB Processor] Request %s — api_versao: %s | urlAssinada: %t | tiqueteDownload: %t",
		requestID, apiVersao, dlInput.URLAssinada != "", dlInput.TiqueteDownload != "")

	// 2. Atomic status claim — prevents concurrent webhook + manual download races.
	// Only proceeds if status is not already 'downloading', 'completed', or 'reprocessing'.
	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'downloading', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'completed', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim request status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Processor] Request %s already being processed by another goroutine — skipping", requestID)
		return nil
	}

	// 3. Get fresh OAuth2 token
	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		updateRequestError(db, requestID, "TOKEN_ERROR", err.Error())
		return fmt.Errorf("failed to get token: %w", err)
	}

	// 4. Download the JSON file (single-use ticket!) — helper compartilhado com fallback em camadas
	rawJSON, err := BaixarArquivoRFB(rfbClient, token, dlInput)
	if err != nil {
		updateRequestError(db, requestID, downloadErrCode(err), err.Error())
		return fmt.Errorf("failed to download: %w", err)
	}

	// 5. Save raw JSON as TEXT immediately — data is preserved even if parse/insert fails.
	// Column is TEXT (not JSONB, migration 064), so no 268 MB size limit applies.
	log.Printf("[RFB Processor] Saving raw JSON (%d MB) for request %s", len(rawJSON)/1024/1024, requestID)
	_, saveErr := db.Exec(`
		UPDATE rfb_requests SET raw_json = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, string(rawJSON), requestID)
	if saveErr != nil {
		// Non-fatal: log but continue — if parse succeeds, debits are still loaded
		log.Printf("[RFB Processor] WARNING: Failed to save raw JSON for request %s: %v (processing continues)", requestID, saveErr)
	} else {
		log.Printf("[RFB Processor] Raw JSON saved successfully for request %s", requestID)
	}

	// 6. Detect shape (v1 blocos fixos vs v2 lista plana por período) e parse de
	// acordo — ver detectRFBShape.
	shape := detectRFBShape(rawJSON)
	log.Printf("[RFB Processor] Shape detectado para request %s: %s", requestID, shape)

	if shape == "v2" {
		var apuracaoV2 RFBApuracaoV2JSON
		if err := json.Unmarshal(rawJSON, &apuracaoV2); err != nil {
			updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao interpretar JSON v2 da RFB: "+err.Error())
			return fmt.Errorf("failed to parse v2 JSON: %w", err)
		}

		tx, err := db.Begin()
		if err != nil {
			updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		insertErrors, procErr := processV2Debitos(tx, requestID, companyID, apuracaoV2)
		if procErr != nil {
			tx.Rollback()
			updateRequestError(db, requestID, "DB_ERROR", "Falha ao processar apuração v2: "+procErr.Error())
			return fmt.Errorf("failed to process v2 apuracao: %w", procErr)
		}

		if err := tx.Commit(); err != nil {
			updateRequestError(db, requestID, "DB_ERROR", "Falha no commit da transação: "+err.Error())
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		finalStatus := "completed"
		if insertErrors > 0 {
			log.Printf("[RFB Processor] WARN: %d debits (v2) failed to insert (raw_json preserved — use reprocess to retry)", insertErrors)
		}
		updateRequestStatus(db, requestID, finalStatus)
		log.Printf("[RFB Processor] Request %s %s (v2): %d períodos processados, %d erros de inserção",
			requestID, finalStatus, len(apuracaoV2.Apuracao), insertErrors)

		if finalStatus == "completed" {
			go TriggerSAPSync(db, requestID)
		}
		return nil
	}

	var apuracao RFBApuracaoJSON
	if err := json.Unmarshal(rawJSON, &apuracao); err != nil {
		updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao interpretar JSON da RFB: "+err.Error())
		return fmt.Errorf("failed to parse JSON: %w", err)
	}
	// Normaliza dataApuracao "AAAAMM" (v1) → "mm/aaaa" (formato canônico de
	// armazenamento, migration 129) logo após o parse — o resto da lógica v1
	// abaixo (derivação estrutural de tipo_apuracao, agregação, upsert) fica
	// inalterada, só passa a operar sobre o valor já normalizado.
	normalizeApuracaoV1DataApuracao(&apuracao)

	// 7. Insert debits and summary inside a single transaction.
	// If any step fails the whole import is rolled back — no partial data.
	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var totalCorrente, totalAjuste, totalExtemporaneo, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string

	// Créditos embutidos na mesma resposta (a RFB manda a NF-e de entrada do comprador
	// junto com a apuração de débitos)
	var totalCreditosCorrente, totalCreditosAjuste int
	var valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto float64
	// Período dos créditos embutidos, derivado dos próprios itens de crédito — pode
	// divergir de dataApuracao (débitos), ex.: resposta só com créditos extemporâneos
	// e débitos correntes de outro período. Usado para agregar/gravar rfb_creditos_resumo
	// separadamente de rfb_resumo.
	var dataApuracaoCreditos string

	// Acumuladores em memória SOMENTE das linhas sem chave_dfe desta chamada — sem
	// chave não há UPSERT/dedup, então a agregação SQL (que só enxerga linhas com
	// chave_dfe) não pode contá-las; ver aggregateRfbDebitosSQL/aggregateRfbCreditosSQL.
	var semChaveCorrente, semChaveAjuste, semChaveExtemporaneo rfbAggBucket
	var semChaveCreditosCorrente, semChaveCreditosAjuste rfbAggBucket

	if apuracao.ApuracaoCorrente != nil {
		for _, d := range apuracao.ApuracaoCorrente.Debitos {
			if err := insertDebito(tx, requestID, companyID, "corrente", d); err != nil {
				log.Printf("[RFB Processor] Error inserting corrente debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveCorrente.Count++
					semChaveCorrente.ValorTotal += d.ValorCBSTotal
					semChaveCorrente.ValorExtinto += d.ValorCBSExtinto
					semChaveCorrente.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
				log.Printf("[RFB Processor] Error inserting corrente credito (chave=%s): %v", c.ChaveDfe, err)
			} else {
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
	}

	if apuracao.ApuracaoAjuste != nil {
		for _, d := range apuracao.ApuracaoAjuste.Debitos {
			if err := insertDebito(tx, requestID, companyID, "ajuste", d); err != nil {
				log.Printf("[RFB Processor] Error inserting ajuste debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveAjuste.Count++
					semChaveAjuste.ValorTotal += d.ValorCBSTotal
					semChaveAjuste.ValorExtinto += d.ValorCBSExtinto
					semChaveAjuste.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if err := insertCredito(tx, requestID, companyID, "ajuste", c); err != nil {
				log.Printf("[RFB Processor] Error inserting ajuste credito (chave=%s): %v", c.ChaveDfe, err)
			} else {
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosAjuste.Count++
					semChaveCreditosAjuste.ValorTotal += c.ValorCBSTotal
					semChaveCreditosAjuste.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosAjuste.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
	}

	if apuracao.DebitosExtemporaneos != nil {
		for _, d := range apuracao.DebitosExtemporaneos.Debitos {
			if err := insertDebito(tx, requestID, companyID, "extemporaneo", d); err != nil {
				log.Printf("[RFB Processor] Error inserting extemporaneo debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveExtemporaneo.Count++
					semChaveExtemporaneo.ValorTotal += d.ValorCBSTotal
					semChaveExtemporaneo.ValorExtinto += d.ValorCBSExtinto
					semChaveExtemporaneo.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if err := insertCredito(tx, requestID, companyID, "extemporaneo", c); err != nil {
				log.Printf("[RFB Processor] Error inserting extemporaneo credito (chave=%s): %v", c.ChaveDfe, err)
			} else {
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					// Créditos extemporâneos entram no bucket corrente (mesma regra de
					// aggregateRfbCreditosSQL: tipo_apuracao <> 'ajuste' → total_corrente).
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
	}

	// Fallback: se nenhum débito de nenhum bloco (corrente/ajuste/extemporaneo) forneceu
	// dataApuracao, tenta extrair de qualquer crédito embutido (mesmos três blocos) —
	// cobre o caso de resposta só com itens extemporâneos, que antes deixava dataApuracao
	// vazio e fazia a agregação SQL filtrar por período errado.
	if dataApuracao == "" && apuracao.ApuracaoCorrente != nil {
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.ApuracaoAjuste != nil {
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.DebitosExtemporaneos != nil {
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				break
			}
		}
	}

	// Período dos créditos embutidos: se nenhum crédito processado forneceu dataApuracao
	// (ex.: resposta sem créditos nesta chamada), reaproveita o período dos débitos como
	// melhor esforço — mantém rfb_creditos_resumo alinhado a rfb_resumo quando não há
	// sinal próprio dos créditos.
	if dataApuracaoCreditos == "" {
		dataApuracaoCreditos = dataApuracao
	}

	// Agregação SQL sobre rfb_debitos/rfb_creditos (linhas com chave_dfe, já deduplicadas
	// via UPSERT) + acumulador em memória das linhas sem chave desta chamada. Roda dentro
	// da mesma tx, depois de todos os insertDebito/insertCredito, antes do commit — reflete
	// o estado real acumulado da tabela para o período, não só o delta da chamada atual
	// (prep para consulta incremental da RFB a partir de out/2026).
	sqlCorrente, sqlAjuste, sqlExtemporaneo, aggErr := aggregateRfbDebitosSQL(tx, companyID, dataApuracao)
	if aggErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de débitos: "+aggErr.Error())
		return fmt.Errorf("failed to aggregate rfb_debitos summary: %w", aggErr)
	}
	totalCorrente = sqlCorrente.Count + semChaveCorrente.Count
	totalAjuste = sqlAjuste.Count + semChaveAjuste.Count
	totalExtemporaneo = sqlExtemporaneo.Count + semChaveExtemporaneo.Count
	valorTotal = sqlCorrente.ValorTotal + sqlAjuste.ValorTotal + sqlExtemporaneo.ValorTotal +
		semChaveCorrente.ValorTotal + semChaveAjuste.ValorTotal + semChaveExtemporaneo.ValorTotal
	valorExtinto = sqlCorrente.ValorExtinto + sqlAjuste.ValorExtinto + sqlExtemporaneo.ValorExtinto +
		semChaveCorrente.ValorExtinto + semChaveAjuste.ValorExtinto + semChaveExtemporaneo.ValorExtinto
	valorNaoExtinto = sqlCorrente.ValorNaoExtinto + sqlAjuste.ValorNaoExtinto + sqlExtemporaneo.ValorNaoExtinto +
		semChaveCorrente.ValorNaoExtinto + semChaveAjuste.ValorNaoExtinto + semChaveExtemporaneo.ValorNaoExtinto
	totalDebitos := totalCorrente + totalAjuste + totalExtemporaneo

	sqlCredCorrente, sqlCredAjuste, aggCredErr := aggregateRfbCreditosSQL(tx, companyID, dataApuracaoCreditos)
	if aggCredErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de créditos embutidos: "+aggCredErr.Error())
		return fmt.Errorf("failed to aggregate rfb_creditos summary: %w", aggCredErr)
	}
	totalCreditosCorrente = sqlCredCorrente.Count + semChaveCreditosCorrente.Count
	totalCreditosAjuste = sqlCredAjuste.Count + semChaveCreditosAjuste.Count
	valorCreditosTotal = sqlCredCorrente.ValorTotal + sqlCredAjuste.ValorTotal +
		semChaveCreditosCorrente.ValorTotal + semChaveCreditosAjuste.ValorTotal
	valorCreditosExtinto = sqlCredCorrente.ValorExtinto + sqlCredAjuste.ValorExtinto +
		semChaveCreditosCorrente.ValorExtinto + semChaveCreditosAjuste.ValorExtinto
	valorCreditosNaoExtinto = sqlCredCorrente.ValorNaoExtinto + sqlCredAjuste.ValorNaoExtinto +
		semChaveCreditosCorrente.ValorNaoExtinto + semChaveCreditosAjuste.ValorNaoExtinto
	totalCreditos := totalCreditosCorrente + totalCreditosAjuste

	log.Printf("[RFB Processor] Resumo agregado (SQL+memória) | request %s | período débitos %s: %d débitos (%d corrente, %d ajuste, %d extemporaneo), CBS R$ %.2f | período créditos %s: %d créditos embutidos (%d corrente, %d ajuste), CBS R$ %.2f",
		requestID, dataApuracao, totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, valorTotal,
		dataApuracaoCreditos, totalCreditos, totalCreditosCorrente, totalCreditosAjuste, valorCreditosTotal)

	// Upsert summary in the same transaction
	_, err = tx.Exec(`
		INSERT INTO rfb_resumo (request_id, company_id, data_apuracao, total_debitos,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
			total_corrente, total_ajuste, total_extemporaneo)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (company_id, data_apuracao)
		DO UPDATE SET request_id = $1, total_debitos = $4,
			valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
			total_corrente = $8, total_ajuste = $9, total_extemporaneo = $10
	`, requestID, companyID, dataApuracao, totalDebitos,
		valorTotal, valorExtinto, valorNaoExtinto,
		totalCorrente, totalAjuste, totalExtemporaneo)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao salvar resumo: "+err.Error())
		return fmt.Errorf("failed to upsert summary: %w", err)
	}

	// Upsert créditos resumo se houver créditos na resposta
	if totalCreditos > 0 {
		if _, credErr := tx.Exec(`
			INSERT INTO rfb_creditos_resumo (request_id, company_id, data_apuracao, total_creditos,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto, total_corrente, total_ajuste)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (company_id, data_apuracao)
			DO UPDATE SET request_id = $1, total_creditos = $4,
				valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
				total_corrente = $8, total_ajuste = $9
		`, requestID, companyID, dataApuracaoCreditos, totalCreditos,
			valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto,
			totalCreditosCorrente, totalCreditosAjuste); credErr != nil {
			log.Printf("[RFB Processor] WARNING: failed to upsert credits summary: %v", credErr)
		} else {
			log.Printf("[RFB Processor] Credits found: %d (%d corrente, %d ajuste), CBS total: %.2f",
				totalCreditos, totalCreditosCorrente, totalCreditosAjuste, valorCreditosTotal)
		}
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit da transação: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 8. Mark request as completed
	finalStatus := "completed"
	if insertErrors > 0 {
		log.Printf("[RFB Processor] WARN: %d debits failed to insert (raw_json preserved — use reprocess to retry)", insertErrors)
	}
	updateRequestStatus(db, requestID, finalStatus)
	log.Printf("[RFB Processor] Request %s %s: %d debits (%d corrente, %d ajuste, %d extemporaneo, %d errors), CBS total: %.2f",
		requestID, finalStatus, totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, insertErrors, valorTotal)

	if finalStatus == "completed" {
		go TriggerSAPSync(db, requestID)
	}

	return nil
}

func insertDebito(exec dbExecutor, requestID, companyID, tipoApuracao string, d RFBDebito) error {
	formasExtincao := sql.NullString{}
	if len(d.FormasExtincao) > 0 && string(d.FormasExtincao) != "null" {
		formasExtincao = sql.NullString{String: string(d.FormasExtincao), Valid: true}
	}
	eventos := sql.NullString{}
	if len(d.Eventos) > 0 && string(d.Eventos) != "null" {
		eventos = sql.NullString{String: string(d.Eventos), Valid: true}
	}

	var dataEmissao *time.Time
	if d.DataDfeEmissao != nil {
		dataEmissao = d.DataDfeEmissao.T
	}

	origem := sql.NullInt64{}
	if d.Origem != nil {
		origem = sql.NullInt64{Int64: int64(*d.Origem), Valid: true}
	}
	documento := sql.NullInt64{}
	if d.Documento != nil {
		documento = sql.NullInt64{Int64: int64(*d.Documento), Valid: true}
	}
	valorExcedente := sql.NullFloat64{}
	if d.ValorCBSExcedente != nil {
		valorExcedente = sql.NullFloat64{Float64: *d.ValorCBSExcedente, Valid: true}
	}
	valorInexigivel := sql.NullFloat64{}
	if d.ValorCBSInexigivel != nil {
		valorInexigivel = sql.NullFloat64{Float64: *d.ValorCBSInexigivel, Valid: true}
	}
	valorSuspenso := sql.NullFloat64{}
	if d.ValorCBSSuspenso != nil {
		valorSuspenso = sql.NullFloat64{Float64: *d.ValorCBSSuspenso, Valid: true}
	}
	valorSaldoDevedor := sql.NullFloat64{}
	if d.ValorCBSSaldoDevedor != nil {
		valorSaldoDevedor = sql.NullFloat64{Float64: *d.ValorCBSSaldoDevedor, Valid: true}
	}

	_, err := exec.Exec(`
		INSERT INTO rfb_debitos (request_id, company_id, tipo_apuracao,
			modelo_dfe, numero_dfe, chave_dfe, data_dfe_emissao, data_apuracao,
			ni_emitente, ni_adquirente,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
			situacao_debito, formas_extincao, eventos,
			origem, documento, data_registro, data_atualizacao,
			valor_cbs_excedente, valor_cbs_inexigivel, valor_cbs_suspenso, valor_cbs_saldo_devedor)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23, $24)
		ON CONFLICT (company_id, chave_dfe) WHERE chave_dfe IS NOT NULL AND chave_dfe != ''
		DO UPDATE SET
			request_id              = EXCLUDED.request_id,
			tipo_apuracao           = EXCLUDED.tipo_apuracao,
			data_dfe_emissao        = EXCLUDED.data_dfe_emissao,
			data_apuracao           = EXCLUDED.data_apuracao,
			valor_cbs_total         = EXCLUDED.valor_cbs_total,
			valor_cbs_extinto       = EXCLUDED.valor_cbs_extinto,
			valor_cbs_nao_extinto   = EXCLUDED.valor_cbs_nao_extinto,
			situacao_debito         = COALESCE(NULLIF(EXCLUDED.situacao_debito, ''), rfb_debitos.situacao_debito),
			formas_extincao         = COALESCE(EXCLUDED.formas_extincao, rfb_debitos.formas_extincao),
			eventos                 = COALESCE(EXCLUDED.eventos, rfb_debitos.eventos),
			origem                  = EXCLUDED.origem,
			documento               = EXCLUDED.documento,
			data_registro           = EXCLUDED.data_registro,
			data_atualizacao        = EXCLUDED.data_atualizacao,
			valor_cbs_excedente     = EXCLUDED.valor_cbs_excedente,
			valor_cbs_inexigivel    = EXCLUDED.valor_cbs_inexigivel,
			valor_cbs_suspenso      = EXCLUDED.valor_cbs_suspenso,
			valor_cbs_saldo_devedor = EXCLUDED.valor_cbs_saldo_devedor
	`, requestID, companyID, tipoApuracao,
		string(d.ModeloDfe), string(d.NumeroDfe), string(d.ChaveDfe), dataEmissao, d.DataApuracao,
		string(d.NiEmitente), string(d.NiAdquirente),
		d.ValorCBSTotal, d.ValorCBSExtinto, d.ValorCBSNaoExtinto,
		string(d.SituacaoDebito), formasExtincao, eventos,
		origem, documento, d.DataRegistro, d.DataAtualizacao,
		valorExcedente, valorInexigivel, valorSuspenso, valorSaldoDevedor)
	return err
}

// ReprocessarRawJSON re-parses the raw JSON already stored in the DB without calling the RFB API.
// Safe pattern: parse JSON FIRST, then atomically delete old debits and insert new ones in a transaction.
// If parse or any insert fails, old data is never deleted — no data loss. Suporta tanto raw_json
// histórico em shape v1 quanto v2 (detectRFBShape) — necessário porque um mesmo request salvo antes
// do cutover pode ser reprocessado depois dele, e vice-versa.
func ReprocessarRawJSON(db *sql.DB, requestID string) error {
	log.Printf("[RFB Reprocess] ============================================================")
	log.Printf("[RFB Reprocess] Iniciando reprocessamento | request: %s", requestID)
	log.Printf("[RFB Reprocess] ============================================================")

	var companyID string
	var rawJSON *string
	err := db.QueryRow(`
		SELECT company_id, raw_json FROM rfb_requests WHERE id = $1
	`, requestID).Scan(&companyID, &rawJSON)
	if err != nil {
		log.Printf("[RFB Reprocess] ERRO ao buscar request: %v", err)
		return fmt.Errorf("failed to fetch request: %w", err)
	}
	if rawJSON == nil || *rawJSON == "" {
		log.Printf("[RFB Reprocess] ERRO: raw_json não encontrado para request %s — não é possível reprocessar sem o JSON original", requestID)
		return fmt.Errorf("no raw_json stored for request %s — cannot reprocess without raw data", requestID)
	}

	jsonSizeMB := float64(len(*rawJSON)) / 1024 / 1024
	log.Printf("[RFB Reprocess] JSON original encontrado (%.2f MB) | company: %s", jsonSizeMB, companyID)

	// 1. Atomic status claim — prevent concurrent reprocess runs
	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'reprocessing', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim reprocess status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Reprocess] Request %s já está sendo processado em outra goroutine — abortando", requestID)
		return nil
	}
	log.Printf("[RFB Reprocess] Status → reprocessing")

	// 2. Detect shape + parse JSON FIRST — before touching any existing data.
	log.Printf("[RFB Reprocess] Etapa 1/4: Detectando shape e interpretando JSON...")
	shape := detectRFBShape([]byte(*rawJSON))
	log.Printf("[RFB Reprocess] Shape detectado: %s", shape)

	var apuracaoV2 RFBApuracaoV2JSON
	var apuracao RFBApuracaoJSON
	if shape == "v2" {
		if err := json.Unmarshal([]byte(*rawJSON), &apuracaoV2); err != nil {
			log.Printf("[RFB Reprocess] ERRO no parse do JSON v2: %v", err)
			updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao reprocessar JSON v2: "+err.Error())
			return fmt.Errorf("failed to parse v2 JSON: %w", err)
		}
		log.Printf("[RFB Reprocess] JSON v2 interpretado | períodos encontrados: %d", len(apuracaoV2.Apuracao))
	} else {
		if err := json.Unmarshal([]byte(*rawJSON), &apuracao); err != nil {
			log.Printf("[RFB Reprocess] ERRO no parse do JSON: %v", err)
			updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao reprocessar JSON: "+err.Error())
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
		normalizeApuracaoV1DataApuracao(&apuracao)

		grupos := 0
		if apuracao.ApuracaoCorrente != nil {
			grupos++
		}
		if apuracao.ApuracaoAjuste != nil {
			grupos++
		}
		if apuracao.DebitosExtemporaneos != nil {
			grupos++
		}
		log.Printf("[RFB Reprocess] JSON interpretado com sucesso | grupos encontrados: %d (corrente=%v, ajuste=%v, extemporaneo=%v)",
			grupos,
			apuracao.ApuracaoCorrente != nil,
			apuracao.ApuracaoAjuste != nil,
			apuracao.DebitosExtemporaneos != nil,
		)
	}

	// 3. Transaction: delete old debits then insert new ones atomically.
	log.Printf("[RFB Reprocess] Etapa 2/4: Limpando dados anteriores...")
	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var deletedDebitos, deletedCreditos int64
	resD, err := tx.Exec(`DELETE FROM rfb_debitos WHERE request_id = $1`, requestID)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao limpar débitos anteriores: "+err.Error())
		return fmt.Errorf("failed to delete existing debits: %w", err)
	}
	deletedDebitos, _ = resD.RowsAffected()

	resC, err := tx.Exec(`DELETE FROM rfb_creditos WHERE request_id = $1`, requestID)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao limpar créditos anteriores: "+err.Error())
		return fmt.Errorf("failed to delete existing credits: %w", err)
	}
	deletedCreditos, _ = resC.RowsAffected()
	log.Printf("[RFB Reprocess] Removidos: %d débitos e %d créditos anteriores", deletedDebitos, deletedCreditos)

	if shape == "v2" {
		log.Printf("[RFB Reprocess] Etapa 3/4: Inserindo registros (v2)...")
		insertErrors, procErr := processV2Debitos(tx, requestID, companyID, apuracaoV2)
		if procErr != nil {
			tx.Rollback()
			updateRequestError(db, requestID, "DB_ERROR", "Falha ao reprocessar apuração v2: "+procErr.Error())
			return fmt.Errorf("failed to process v2 apuracao: %w", procErr)
		}
		if err := tx.Commit(); err != nil {
			updateRequestError(db, requestID, "DB_ERROR", "Falha no commit: "+err.Error())
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
		updateRequestStatus(db, requestID, "completed")
		go TriggerSAPSync(db, requestID)
		log.Printf("[RFB Reprocess] ============================================================")
		if insertErrors > 0 {
			log.Printf("[RFB Reprocess] AVISO: %d débitos (v2) falharam na inserção", insertErrors)
		}
		log.Printf("[RFB Reprocess] CONCLUÍDO (v2) | request: %s | %d períodos | status → completed", requestID, len(apuracaoV2.Apuracao))
		log.Printf("[RFB Reprocess] ============================================================")
		return nil
	}

	var totalCorrente, totalAjuste, totalExtemporaneo, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string
	var totalCreditosCorrente, totalCreditosAjuste int
	var valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto float64
	// Período dos créditos embutidos, derivado dos próprios itens de crédito — pode
	// divergir de dataApuracao (débitos). Ver comentário equivalente em ProcessarDownloadRFB.
	var dataApuracaoCreditos string

	// Acumuladores em memória SOMENTE das linhas sem chave_dfe desta chamada — ver
	// aggregateRfbDebitosSQL/aggregateRfbCreditosSQL e Spec Change Log / Loopback 1.
	var semChaveCorrente, semChaveAjuste, semChaveExtemporaneo rfbAggBucket
	var semChaveCreditosCorrente, semChaveCreditosAjuste rfbAggBucket

	log.Printf("[RFB Reprocess] Etapa 3/4: Inserindo registros...")

	if apuracao.ApuracaoCorrente != nil {
		nd := len(apuracao.ApuracaoCorrente.Debitos)
		log.Printf("[RFB Reprocess] ApuracaoCorrente: %d débitos (créditos embutidos serão contados no loop)", nd)
		insDeb, insCred := 0, 0
		for _, d := range apuracao.ApuracaoCorrente.Debitos {
			if err := insertDebito(tx, requestID, companyID, "corrente", d); err != nil {
				log.Printf("[RFB Reprocess] ERRO débito corrente (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				insDeb++
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveCorrente.Count++
					semChaveCorrente.ValorTotal += d.ValorCBSTotal
					semChaveCorrente.ValorExtinto += d.ValorCBSExtinto
					semChaveCorrente.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
				log.Printf("[RFB Reprocess] ERRO crédito corrente (chave=%s): %v", c.ChaveDfe, err)
			} else {
				insCred++
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
		log.Printf("[RFB Reprocess] ApuracaoCorrente inserida: %d/%d débitos, %d créditos CBS", insDeb, nd, insCred)
	}

	if apuracao.ApuracaoAjuste != nil {
		nd := len(apuracao.ApuracaoAjuste.Debitos)
		log.Printf("[RFB Reprocess] ApuracaoAjuste: %d débitos", nd)
		insDeb, insCred := 0, 0
		for _, d := range apuracao.ApuracaoAjuste.Debitos {
			if err := insertDebito(tx, requestID, companyID, "ajuste", d); err != nil {
				log.Printf("[RFB Reprocess] ERRO débito ajuste (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				insDeb++
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveAjuste.Count++
					semChaveAjuste.ValorTotal += d.ValorCBSTotal
					semChaveAjuste.ValorExtinto += d.ValorCBSExtinto
					semChaveAjuste.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if err := insertCredito(tx, requestID, companyID, "ajuste", c); err != nil {
				log.Printf("[RFB Reprocess] ERRO crédito ajuste (chave=%s): %v", c.ChaveDfe, err)
			} else {
				insCred++
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosAjuste.Count++
					semChaveCreditosAjuste.ValorTotal += c.ValorCBSTotal
					semChaveCreditosAjuste.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosAjuste.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
		log.Printf("[RFB Reprocess] ApuracaoAjuste inserida: %d/%d débitos, %d créditos CBS", insDeb, nd, insCred)
	}

	if apuracao.DebitosExtemporaneos != nil {
		nd := len(apuracao.DebitosExtemporaneos.Debitos)
		log.Printf("[RFB Reprocess] DebitosExtemporaneos: %d débitos", nd)
		insDeb, insCred := 0, 0
		for _, d := range apuracao.DebitosExtemporaneos.Debitos {
			if err := insertDebito(tx, requestID, companyID, "extemporaneo", d); err != nil {
				log.Printf("[RFB Reprocess] ERRO débito extemporaneo (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				insDeb++
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveExtemporaneo.Count++
					semChaveExtemporaneo.ValorTotal += d.ValorCBSTotal
					semChaveExtemporaneo.ValorExtinto += d.ValorCBSExtinto
					semChaveExtemporaneo.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if err := insertCredito(tx, requestID, companyID, "extemporaneo", c); err != nil {
				log.Printf("[RFB Reprocess] ERRO crédito extemporaneo (chave=%s): %v", c.ChaveDfe, err)
			} else {
				insCred++
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					// Créditos extemporâneos entram no bucket corrente (mesma regra de
					// aggregateRfbCreditosSQL: tipo_apuracao <> 'ajuste' → total_corrente).
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
		log.Printf("[RFB Reprocess] DebitosExtemporaneos inseridos: %d/%d débitos, %d créditos CBS", insDeb, nd, insCred)
	}

	// Fallback: se nenhum débito de nenhum bloco forneceu dataApuracao, tenta extrair de
	// qualquer crédito embutido (corrente/ajuste/extemporaneo) — cobre resposta só com
	// itens extemporâneos.
	if dataApuracao == "" && apuracao.ApuracaoCorrente != nil {
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				log.Printf("[RFB Reprocess] dataApuracao obtido dos créditos correntes: %s", dataApuracao)
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.ApuracaoAjuste != nil {
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				log.Printf("[RFB Reprocess] dataApuracao obtido dos créditos ajuste: %s", dataApuracao)
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.DebitosExtemporaneos != nil {
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				log.Printf("[RFB Reprocess] dataApuracao obtido dos créditos extemporâneos: %s", dataApuracao)
				break
			}
		}
	}

	// Período dos créditos embutidos: se nenhum crédito processado forneceu dataApuracao,
	// reaproveita o período dos débitos como melhor esforço.
	if dataApuracaoCreditos == "" {
		dataApuracaoCreditos = dataApuracao
	}

	// Agregação SQL (linhas com chave_dfe, já deduplicadas via UPSERT) + acumulador em
	// memória das linhas sem chave desta chamada — mesma lógica de ProcessarDownloadRFB.
	// Roda dentro da mesma tx que fez o DELETE+reinsert acima, antes do commit, refletindo
	// o estado real da tabela para o período (não só as linhas deste request_id).
	sqlCorrente, sqlAjuste, sqlExtemporaneo, aggErr := aggregateRfbDebitosSQL(tx, companyID, dataApuracao)
	if aggErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de débitos: "+aggErr.Error())
		return fmt.Errorf("failed to aggregate rfb_debitos summary: %w", aggErr)
	}
	totalCorrente = sqlCorrente.Count + semChaveCorrente.Count
	totalAjuste = sqlAjuste.Count + semChaveAjuste.Count
	totalExtemporaneo = sqlExtemporaneo.Count + semChaveExtemporaneo.Count
	valorTotal = sqlCorrente.ValorTotal + sqlAjuste.ValorTotal + sqlExtemporaneo.ValorTotal +
		semChaveCorrente.ValorTotal + semChaveAjuste.ValorTotal + semChaveExtemporaneo.ValorTotal
	valorExtinto = sqlCorrente.ValorExtinto + sqlAjuste.ValorExtinto + sqlExtemporaneo.ValorExtinto +
		semChaveCorrente.ValorExtinto + semChaveAjuste.ValorExtinto + semChaveExtemporaneo.ValorExtinto
	valorNaoExtinto = sqlCorrente.ValorNaoExtinto + sqlAjuste.ValorNaoExtinto + sqlExtemporaneo.ValorNaoExtinto +
		semChaveCorrente.ValorNaoExtinto + semChaveAjuste.ValorNaoExtinto + semChaveExtemporaneo.ValorNaoExtinto
	totalDebitos := totalCorrente + totalAjuste + totalExtemporaneo

	sqlCredCorrente, sqlCredAjuste, aggCredErr := aggregateRfbCreditosSQL(tx, companyID, dataApuracaoCreditos)
	if aggCredErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de créditos embutidos: "+aggCredErr.Error())
		return fmt.Errorf("failed to aggregate rfb_creditos summary: %w", aggCredErr)
	}
	totalCreditosCorrente = sqlCredCorrente.Count + semChaveCreditosCorrente.Count
	totalCreditosAjuste = sqlCredAjuste.Count + semChaveCreditosAjuste.Count
	valorCreditosTotal = sqlCredCorrente.ValorTotal + sqlCredAjuste.ValorTotal +
		semChaveCreditosCorrente.ValorTotal + semChaveCreditosAjuste.ValorTotal
	valorCreditosExtinto = sqlCredCorrente.ValorExtinto + sqlCredAjuste.ValorExtinto +
		semChaveCreditosCorrente.ValorExtinto + semChaveCreditosAjuste.ValorExtinto
	valorCreditosNaoExtinto = sqlCredCorrente.ValorNaoExtinto + sqlCredAjuste.ValorNaoExtinto +
		semChaveCreditosCorrente.ValorNaoExtinto + semChaveCreditosAjuste.ValorNaoExtinto
	totalCreditos := totalCreditosCorrente + totalCreditosAjuste

	log.Printf("[RFB Reprocess] Etapa 4/4: Atualizando resumos (SQL+memória) | período débitos: %s | período créditos: %s", dataApuracao, dataApuracaoCreditos)
	log.Printf("[RFB Reprocess]   Débitos : %d total (%d corrente, %d ajuste, %d extemporaneo) | CBS R$ %.2f",
		totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, valorTotal)
	log.Printf("[RFB Reprocess]   Créditos: %d total (%d corrente+extemp, %d ajuste) | CBS R$ %.2f",
		totalCreditos, totalCreditosCorrente, totalCreditosAjuste, valorCreditosTotal)

	if _, err = tx.Exec(`
		INSERT INTO rfb_resumo (request_id, company_id, data_apuracao, total_debitos,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
			total_corrente, total_ajuste, total_extemporaneo)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (company_id, data_apuracao)
		DO UPDATE SET request_id = $1, total_debitos = $4,
			valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
			total_corrente = $8, total_ajuste = $9, total_extemporaneo = $10
	`, requestID, companyID, dataApuracao, totalDebitos,
		valorTotal, valorExtinto, valorNaoExtinto,
		totalCorrente, totalAjuste, totalExtemporaneo); err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao salvar resumo: "+err.Error())
		return fmt.Errorf("failed to upsert summary: %w", err)
	}
	log.Printf("[RFB Reprocess] Resumo de débitos atualizado (rfb_resumo)")

	if totalCreditos > 0 {
		if _, credErr := tx.Exec(`
			INSERT INTO rfb_creditos_resumo (request_id, company_id, data_apuracao, total_creditos,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto, total_corrente, total_ajuste)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (company_id, data_apuracao)
			DO UPDATE SET request_id = $1, total_creditos = $4,
				valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
				total_corrente = $8, total_ajuste = $9
		`, requestID, companyID, dataApuracaoCreditos, totalCreditos,
			valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto,
			totalCreditosCorrente, totalCreditosAjuste); credErr != nil {
			log.Printf("[RFB Reprocess] AVISO: falha ao atualizar resumo de créditos: %v", credErr)
		} else {
			log.Printf("[RFB Reprocess] Resumo de créditos atualizado (rfb_creditos_resumo)")
		}
	} else {
		log.Printf("[RFB Reprocess] Nenhum crédito encontrado — rfb_creditos_resumo não atualizado")
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	updateRequestStatus(db, requestID, "completed")
	go TriggerSAPSync(db, requestID)

	log.Printf("[RFB Reprocess] ============================================================")
	if insertErrors > 0 {
		log.Printf("[RFB Reprocess] AVISO: %d débitos falharam na inserção", insertErrors)
	}
	log.Printf("[RFB Reprocess] CONCLUÍDO | request: %s | status → completed", requestID)
	log.Printf("[RFB Reprocess]   %d débitos | %d créditos | período: %s | CBS R$ %.2f",
		totalDebitos, totalCreditos, dataApuracao, valorTotal)
	log.Printf("[RFB Reprocess] ============================================================")
	return nil
}

func updateRequestStatus(db *sql.DB, requestID, status string) {
	query := `UPDATE rfb_requests SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	if status == "completed" {
		// urlAssinada é credencial temporária: não fica guardada depois do download concluir.
		query = `UPDATE rfb_requests SET status = $1, url_assinada = NULL, url_assinada_expira_em = NULL,
			updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	}
	_, err := db.Exec(query, status, requestID)
	if err != nil {
		log.Printf("[RFB Processor] Error updating request %s status to %s: %v", requestID, status, err)
	}
}

// updateRequestError é o ponto único de erro terminal do processamento: grava code/message
// (truncados por runes, UTF-8-safe) e zera url_assinada* — urlAssinada é credencial
// temporária e não deve sobrar numa linha em erro (o retry renova via situacao).
func updateRequestError(db *sql.DB, requestID, code, message string) {
	_, err := db.Exec(`
		UPDATE rfb_requests SET status = 'error', error_code = $1, error_message = $2,
			url_assinada = NULL, url_assinada_expira_em = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`, TruncateRunes(code, 50), TruncateRunes(message, 4000), requestID)
	if err != nil {
		log.Printf("[RFB Processor] Error updating request %s error: %v", requestID, err)
	}
}

package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Structs de parsing XML — CT-e (mod 57)
// Namespace: http://www.portalfiscal.inf.br/cte
// ---------------------------------------------------------------------------

type cteProc struct {
	XMLName xml.Name `xml:"cteProc"`
	CTe     cteDoc   `xml:"CTe"`
	ProtCTe protCTe  `xml:"protCTe"`
}

type cteDoc struct {
	InfCte infCte `xml:"infCte"`
}

type protCTe struct {
	InfProt infProtCTe `xml:"infProt"`
}

type infProtCTe struct {
	ChCTe string `xml:"chCTe"` // chave 44 dígitos
}

type infCte struct {
	ID         string     `xml:"Id,attr"` // "CTe" + 44 dígitos (fallback)
	Ide        ideCTe     `xml:"ide"`
	Emit       emitCTe    `xml:"emit"`
	Rem        parteCTe   `xml:"rem"`
	Dest       parteCTe   `xml:"dest"`
	VPrest     vPrestCTe  `xml:"vPrest"`
	Imp        impCTe     `xml:"imp"`
	InfCTeNorm infCTeNorm `xml:"infCTeNorm"`
}

type ideCTe struct {
	Mod   string `xml:"mod"`    // sempre "57"
	Serie string `xml:"serie"`
	NCT   string `xml:"nCT"`   // número do CT-e
	DhEmi string `xml:"dhEmi"` // ISO8601 → data_emissao + mes_ano
	NatOp string `xml:"natOp"`
	CFOP  string `xml:"CFOP"`
	Modal string `xml:"modal"` // 01=Rodoviário 02=Aéreo 03=Aquaviário 04=Ferroviário
}

type emitCTe struct {
	CNPJ      string   `xml:"CNPJ"`
	XNome     string   `xml:"xNome"`
	EnderEmit enderCTe `xml:"enderEmit"`
}

// parteCTe cobre rem e dest: rem usa EnderReme, dest usa EnderDest
type parteCTe struct {
	CNPJ      string   `xml:"CNPJ"`
	CPF       string   `xml:"CPF"`
	XNome     string   `xml:"xNome"`
	EnderReme enderCTe `xml:"enderReme"` // remetente
	EnderDest enderCTe `xml:"enderDest"` // destinatário
}

type enderCTe struct {
	UF string `xml:"UF"`
}

type vPrestCTe struct {
	VTPrest string `xml:"vTPrest"` // total da prestação
	VRec    string `xml:"vRec"`   // valor a receber
}

// impCTe: <imp> contém <ICMS> (com múltiplas variantes) e <IBSCBSTot>
type impCTe struct {
	ICMS      icmsCTeWrapper `xml:"ICMS"`
	IBSCBSTot ibsCbsTotCTe  `xml:"IBSCBSTot"`
}

// icmsCTeWrapper captura qualquer variante de ICMS do CT-e como campos nomeados.
// Cada variante é um elemento filho diferente de <ICMS>.
type icmsCTeWrapper struct {
	ICMS00      icmsCTeBase `xml:"ICMS00"`
	ICMS20      icmsCTeBase `xml:"ICMS20"`
	ICMS60      icmsCTeBase `xml:"ICMS60"`
	ICMS90      icmsCTeBase `xml:"ICMS90"`
	ICMSOutraUF icmsCTeBase `xml:"ICMSOutraUF"`
}

// icmsCTeBase: variantes que têm vBC e vICMS calculado
type icmsCTeBase struct {
	VBC   string `xml:"vBC"`
	VICMS string `xml:"vICMS"`
}

// ibsCbsTotCTe: estrutura análoga à NF-e (mesmas tags XML)
type ibsCbsTotCTe struct {
	VBCIBSCBS string   `xml:"vBCIBSCBS"`
	GIBS       gIBSCTe `xml:"gIBS"`
	GCBS       gCBSCTe `xml:"gCBS"`
}

type gIBSCTe struct {
	VIBS string `xml:"vIBS"`
}

type gCBSCTe struct {
	VCBS string `xml:"vCBS"`
}

type infCTeNorm struct {
	InfCarga infCTeNormCarga `xml:"infCarga"`
}

type infCTeNormCarga struct {
	VCarga string `xml:"vCarga"`
}

// ---------------------------------------------------------------------------
// Helpers específicos para CT-e
// ---------------------------------------------------------------------------

// parseCTeXML lê bytes de um XML de CT-e e retorna os dados estruturados.
// Reutiliza nfeCharsetReader (definido em nfe_saidas.go, mesmo pacote).
func parseCTeXML(data []byte) (*cteProc, error) {
	// Remove namespace CT-e para simplificar o parsing
	data = bytes.ReplaceAll(data,
		[]byte(` xmlns="http://www.portalfiscal.inf.br/cte"`), []byte(""))
	data = bytes.ReplaceAll(data,
		[]byte(` xmlns='http://www.portalfiscal.inf.br/cte'`), []byte(""))

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = nfeCharsetReader // reutiliza do mesmo pacote

	var proc cteProc
	if err := dec.Decode(&proc); err != nil {
		return nil, fmt.Errorf("erro ao parsear CT-e XML: %w", err)
	}
	return &proc, nil
}

// extractChaveCTe retorna a chave de acesso de 44 dígitos do CT-e.
func extractChaveCTe(proc *cteProc) string {
	// Preferencial: protCTe/infProt/chCTe
	ch := strings.TrimSpace(proc.ProtCTe.InfProt.ChCTe)
	if len(ch) == 44 {
		return ch
	}
	// Fallback: atributo Id de infCte (formato: "CTe" + 44 dígitos)
	id := strings.TrimSpace(proc.CTe.InfCte.ID)
	if strings.HasPrefix(id, "CTe") && len(id) == 47 {
		return id[3:]
	}
	return ""
}

// resolveICMSCTe retorna vBC e vICMS da primeira variante preenchida.
func resolveICMSCTe(w icmsCTeWrapper) (float64, float64) {
	for _, c := range []icmsCTeBase{w.ICMS00, w.ICMS20, w.ICMS60, w.ICMS90, w.ICMSOutraUF} {
		if c.VBC != "" || c.VICMS != "" {
			return toDecimal(c.VBC), toDecimal(c.VICMS)
		}
	}
	return 0, 0
}

// ---------------------------------------------------------------------------
// Tipos de resposta JSON
// ---------------------------------------------------------------------------

type cteErro struct {
	Arquivo string `json:"arquivo"`
	Erro    string `json:"erro"`
}

type cteUploadResult struct {
	Importados int       `json:"importados"`
	Ignorados  int       `json:"ignorados"`
	Erros      []cteErro `json:"erros"`
}

type cteRow struct {
	ID          string   `json:"id"`
	ChaveCTe    string   `json:"chave_cte"`
	Modelo      int      `json:"modelo"`
	Serie       string   `json:"serie"`
	NumeroCTe   string   `json:"numero_cte"`
	DataEmissao string   `json:"data_emissao"`
	MesAno      string   `json:"mes_ano"`
	NatOp       string   `json:"nat_op"`
	CFOP        string   `json:"cfop"`
	Modal       string   `json:"modal"`
	// Emitente (transportadora)
	EmitCNPJ string `json:"emit_cnpj"`
	EmitNome string `json:"emit_nome"`
	EmitUF   string `json:"emit_uf"`
	// Remetente
	RemCNPJCPF string `json:"rem_cnpj_cpf"`
	RemNome    string `json:"rem_nome"`
	RemUF      string `json:"rem_uf"`
	// Destinatário
	DestCNPJCPF string `json:"dest_cnpj_cpf"`
	DestNome    string `json:"dest_nome"`
	DestUF      string `json:"dest_uf"`
	// Valores
	VPrest   float64  `json:"v_prest"`
	VRec     float64  `json:"v_rec"`
	VCarga   float64  `json:"v_carga"`
	VBcICMS  float64  `json:"v_bc_icms"`
	VICMS    float64  `json:"v_icms"`
	// IBS/CBS nullable — transportadoras sem as tags ficam com null
	VBcIbsCbs *float64 `json:"v_bc_ibs_cbs"`
	VIBS      *float64 `json:"v_ibs"`
	VCBS      *float64 `json:"v_cbs"`
}

// ---------------------------------------------------------------------------
// CteEntradasUploadHandler — POST /api/cte-entradas/upload
// ---------------------------------------------------------------------------

func CteEntradasUploadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa: "+err.Error())
			return
		}

		// Streaming multipart — sem limite de número de partes (Go 1.20+ limita a 1000 com ParseMultipartForm)
		mr, err := r.MultipartReader()
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "Erro ao processar upload: "+err.Error())
			return
		}

		result := cteUploadResult{Erros: []cteErro{}}
		totalParts := 0

		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				jsonErr(w, http.StatusBadRequest, "Erro ao ler multipart: "+err.Error())
				return
			}

			if part.FormName() != "xmls" {
				part.Close()
				continue
			}

			filename := part.FileName()
			if filename == "" {
				part.Close()
				continue
			}

			totalParts++
			data, err := io.ReadAll(part)
			part.Close()
			if err != nil {
				result.Erros = append(result.Erros, cteErro{filename, "Erro ao ler: " + err.Error()})
				continue
			}

			proc, err := parseCTeXML(data)
			if err != nil {
				result.Erros = append(result.Erros, cteErro{filename, err.Error()})
				continue
			}

			inf := proc.CTe.InfCte

			// Valida modelo: apenas 57 (CT-e)
			mod := strings.TrimSpace(inf.Ide.Mod)
			if mod != "57" {
				result.Ignorados++
				continue
			}

			// Extrai chave de acesso
			chave := extractChaveCTe(proc)
			if len(chave) != 44 {
				result.Erros = append(result.Erros, cteErro{filename, "Chave CT-e inválida ou ausente"})
				continue
			}

			// Parseia data de emissão (reutiliza helper de nfe_saidas.go)
			dataEmissao, mesAno, err := parseDhEmi(inf.Ide.DhEmi)
			if err != nil {
				result.Erros = append(result.Erros, cteErro{filename, err.Error()})
				continue
			}

			// Remetente: CNPJ ou CPF
			remCNPJCPF := strings.TrimSpace(inf.Rem.CNPJ)
			if remCNPJCPF == "" {
				remCNPJCPF = strings.TrimSpace(inf.Rem.CPF)
			}

			// Destinatário: CNPJ ou CPF
			destCNPJCPF := strings.TrimSpace(inf.Dest.CNPJ)
			if destCNPJCPF == "" {
				destCNPJCPF = strings.TrimSpace(inf.Dest.CPF)
			}

			// UF do remetente: tag <enderReme>
			remUF := strings.TrimSpace(inf.Rem.EnderReme.UF)
			// UF do destinatário: tag <enderDest>
			destUF := strings.TrimSpace(inf.Dest.EnderDest.UF)

			// ICMS: resolve a variante correta
			vBC, vICMS := resolveICMSCTe(inf.Imp.ICMS)

			ib := inf.Imp.IBSCBSTot
			modInt, _ := strconv.Atoi(mod)

			_, err = db.Exec(`
				INSERT INTO cte_entradas (
					company_id, chave_cte, modelo, serie, numero_cte,
					data_emissao, mes_ano, nat_op, cfop, modal,
					emit_cnpj, emit_nome, emit_uf,
					rem_cnpj_cpf, rem_nome, rem_uf,
					dest_cnpj_cpf, dest_nome, dest_uf,
					v_prest, v_rec, v_carga,
					v_bc_icms, v_icms,
					v_bc_ibs_cbs, v_ibs, v_cbs
				) VALUES (
					$1,$2,$3,$4,$5,
					$6,$7,$8,$9,$10,
					$11,$12,$13,
					$14,$15,$16,
					$17,$18,$19,
					$20,$21,$22,
					$23,$24,
					$25,$26,$27
				)
				ON CONFLICT ON CONSTRAINT uq_cte_entradas_company_chave DO NOTHING`,
				companyID, chave, modInt, inf.Ide.Serie, inf.Ide.NCT,
				dataEmissao, mesAno, inf.Ide.NatOp, inf.Ide.CFOP, inf.Ide.Modal,
				inf.Emit.CNPJ, inf.Emit.XNome, inf.Emit.EnderEmit.UF,
				remCNPJCPF, inf.Rem.XNome, remUF,
				destCNPJCPF, inf.Dest.XNome, destUF,
				toDecimal(inf.VPrest.VTPrest), toDecimal(inf.VPrest.VRec),
				toDecimal(inf.InfCTeNorm.InfCarga.VCarga),
				vBC, vICMS,
				toNullDecimal(ib.VBCIBSCBS), toNullDecimal(ib.GIBS.VIBS), toNullDecimal(ib.GCBS.VCBS),
			)
			if err != nil {
				log.Printf("CteEntradas INSERT error [%s]: %v", chave, err)
				result.Erros = append(result.Erros, cteErro{filename, "Erro ao salvar no banco: " + err.Error()})
				continue
			}

			// Save raw XML to dfe_xml for DACTE generation
			_, _ = db.Exec(`
				INSERT INTO dfe_xml (company_id, chave, tipo, modelo, xml_raw)
				VALUES ($1, $2, 'cte', $3, $4)
				ON CONFLICT ON CONSTRAINT uq_dfe_xml_company_chave DO UPDATE SET xml_raw = EXCLUDED.xml_raw
			`, companyID, chave, modInt, string(data))

			result.Importados++
		}

		if totalParts == 0 {
			jsonErr(w, http.StatusBadRequest, "Nenhum arquivo enviado (campo 'xmls')")
			return
		}

		result.Ignorados = totalParts - result.Importados - len(result.Erros) - result.Ignorados
		if result.Ignorados < 0 {
			result.Ignorados = 0
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}
}

// ---------------------------------------------------------------------------
// CteEntradasListHandler — GET /api/cte-entradas
// ---------------------------------------------------------------------------

func CteEntradasListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodGet {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa: "+err.Error())
			return
		}

		q := r.URL.Query()
		mesAno   := q.Get("mes_ano")
		emitCNPJ := q.Get("emit_cnpj")
		modelo   := q.Get("modelo")
		destCNPJ := q.Get("dest_cnpj")
		dataDe   := q.Get("data_de")
		dataAte  := q.Get("data_ate")

		page, pageSize := 1, 100
		if p, e := strconv.Atoi(q.Get("page")); e == nil && p > 0 { page = p }
		if ps, e := strconv.Atoi(q.Get("page_size")); e == nil && ps > 0 && ps <= 500 { pageSize = ps }

		// ── Monta WHERE dinâmico ──────────────────────────────────────────────
		args := []interface{}{companyID}
		idx  := 2
		where := "WHERE company_id = $1"

		if mesAno != "" { where += fmt.Sprintf(" AND mes_ano = $%d", idx); args = append(args, mesAno); idx++ }
		if emitCNPJ != "" { where += fmt.Sprintf(" AND emit_cnpj = $%d", idx); args = append(args, emitCNPJ); idx++ }
		if modelo != "" { where += fmt.Sprintf(" AND modelo = $%d", idx); args = append(args, modelo); idx++ }
		if dataDe != "" { where += fmt.Sprintf(" AND data_emissao >= $%d", idx); args = append(args, dataDe); idx++ }
		if dataAte != "" { where += fmt.Sprintf(" AND data_emissao <= $%d", idx); args = append(args, dataAte); idx++ }
		if destCNPJ != "" { where += fmt.Sprintf(" AND dest_cnpj_cpf LIKE $%d", idx); args = append(args, destCNPJ+"%"); idx++ }
		if destNome := q.Get("dest_nome"); destNome != "" { where += fmt.Sprintf(" AND dest_nome ILIKE $%d", idx); args = append(args, "%"+destNome+"%"); idx++ }
		if emitNome := q.Get("emit_nome"); emitNome != "" { where += fmt.Sprintf(" AND emit_nome ILIKE $%d", idx); args = append(args, "%"+emitNome+"%"); idx++ }
		if emitCNPJSearch := q.Get("emit_cnpj_search"); emitCNPJSearch != "" { where += fmt.Sprintf(" AND emit_cnpj LIKE $%d", idx); args = append(args, emitCNPJSearch+"%"); idx++ }
		if q.Get("sem_ibs_cbs") == "true" { where += " AND (COALESCE(v_ibs,0) = 0 AND COALESCE(v_cbs,0) = 0)" }

		// ── COUNT total ───────────────────────────────────────────────────────
		var total int
		if err := db.QueryRow("SELECT COUNT(*) FROM cte_entradas "+where, args...).Scan(&total); err != nil {
			log.Printf("CteEntradasList count error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		// ── Totalizadores ─────────────────────────────────────────────────────
		var totVPrest, totIBS, totCBS float64
		db.QueryRow(
			"SELECT COALESCE(SUM(v_prest),0), COALESCE(SUM(v_ibs),0), COALESCE(SUM(v_cbs),0) FROM cte_entradas "+where,
			args...,
		).Scan(&totVPrest, &totIBS, &totCBS)

		// ── SELECT paginado ───────────────────────────────────────────────────
		offset := (page - 1) * pageSize
		selectQ := `
			SELECT
				id, chave_cte, modelo, serie, numero_cte,
				TO_CHAR(data_emissao, 'DD/MM/YYYY'), mes_ano,
				COALESCE(nat_op,''), COALESCE(cfop,''), COALESCE(modal,''),
				emit_cnpj, COALESCE(emit_nome,''), COALESCE(emit_uf,''),
				COALESCE(rem_cnpj_cpf,''), COALESCE(rem_nome,''), COALESCE(rem_uf,''),
				COALESCE(dest_cnpj_cpf,''), COALESCE(dest_nome,''), COALESCE(dest_uf,''),
				v_prest, v_rec, v_carga, v_bc_icms, v_icms,
				v_bc_ibs_cbs, v_ibs, v_cbs
			FROM cte_entradas ` + where +
			fmt.Sprintf(" ORDER BY data_emissao DESC, numero_cte DESC LIMIT $%d OFFSET $%d", idx, idx+1)
		pageArgs := append(args, pageSize, offset)

		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			log.Printf("CteEntradasList error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()

		list := []cteRow{}
		for rows.Next() {
			var row cteRow
			err := rows.Scan(
				&row.ID, &row.ChaveCTe, &row.Modelo, &row.Serie, &row.NumeroCTe,
				&row.DataEmissao, &row.MesAno, &row.NatOp, &row.CFOP, &row.Modal,
				&row.EmitCNPJ, &row.EmitNome, &row.EmitUF,
				&row.RemCNPJCPF, &row.RemNome, &row.RemUF,
				&row.DestCNPJCPF, &row.DestNome, &row.DestUF,
				&row.VPrest, &row.VRec, &row.VCarga, &row.VBcICMS, &row.VICMS,
				&row.VBcIbsCbs, &row.VIBS, &row.VCBS,
			)
			if err != nil {
				log.Printf("CteEntradasList scan error: %v", err)
				continue
			}
			list = append(list, row)
		}

		totalPages := (total + pageSize - 1) / pageSize

		json.NewEncoder(w).Encode(map[string]interface{}{
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
			"totals": map[string]float64{
				"v_prest": totVPrest,
				"v_ibs":   totIBS,
				"v_cbs":   totCBS,
			},
			"items": list,
		})
	}
}

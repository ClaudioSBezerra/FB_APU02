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
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// ---------------------------------------------------------------------------
// Structs de parsing XML — nomes dos campos refletem as tags da NF-e
// ---------------------------------------------------------------------------

type nfeProc struct {
	XMLName xml.Name `xml:"nfeProc"`
	NFe     nfe      `xml:"NFe"`
	ProtNFe protNFe  `xml:"protNFe"`
}

type nfe struct {
	InfNFe infNFe `xml:"infNFe"`
}

type protNFe struct {
	InfProt infProt `xml:"infProt"`
}

type infProt struct {
	ChNFe string `xml:"chNFe"` // chave 44 dígitos
}

type infNFe struct {
	ID    string `xml:"Id,attr"` // "NFe" + 44 dígitos (fallback)
	Ide   ide    `xml:"ide"`
	Emit  emit   `xml:"emit"`
	Dest  dest   `xml:"dest"`
	Total total  `xml:"total"`
}

type ide struct {
	Mod   string `xml:"mod"`    // 55 ou 65
	Serie string `xml:"serie"`
	NNF   string `xml:"nNF"`
	DhEmi string `xml:"dhEmi"` // ISO8601 → data_emissao + mes_ano
	TpNF  string `xml:"tpNF"` // 1 = saída (rejeitar se ≠ 1)
	NatOp string `xml:"natOp"`
}

type emit struct {
	CNPJ      string    `xml:"CNPJ"`
	XNome     string    `xml:"xNome"`
	EnderEmit enderEmit `xml:"enderEmit"`
}

type enderEmit struct {
	XMun string `xml:"xMun"`
	UF   string `xml:"UF"`
}

type dest struct {
	CNPJ      string    `xml:"CNPJ"`
	CPF       string    `xml:"CPF"`
	XNome     string    `xml:"xNome"`
	EnderDest enderDest `xml:"enderDest"`
}

type enderDest struct {
	CMun string `xml:"cMun"` // código IBGE 7 dígitos
	UF   string `xml:"UF"`
}

type total struct {
	ICMSTot   icmsTot   `xml:"ICMSTot"`
	IBSCBSTot ibsCbsTot `xml:"IBSCBSTot"`
}

type icmsTot struct {
	VBC        string `xml:"vBC"`
	VICMS      string `xml:"vICMS"`
	VICMSDeson string `xml:"vICMSDeson"`
	VFCP       string `xml:"vFCP"`
	VBCST      string `xml:"vBCST"`
	VST        string `xml:"vST"`
	VFcpST     string `xml:"vFCPST"`
	VFcpSTRet  string `xml:"vFCPSTRet"`
	VProd      string `xml:"vProd"`
	VFrete     string `xml:"vFrete"`
	VSeg       string `xml:"vSeg"`
	VDesc      string `xml:"vDesc"`
	VII        string `xml:"vII"`
	VIPI       string `xml:"vIPI"`
	VIPIDevol  string `xml:"vIPIDevol"`
	VPIS       string `xml:"vPIS"`
	VCOFINS    string `xml:"vCOFINS"`
	VOutro     string `xml:"vOutro"`
	VNF        string `xml:"vNF"`
}

type ibsCbsTot struct {
	VBCIBSCBS string `xml:"vBCIBSCBS"`
	GIBS      gIBS   `xml:"gIBS"`
	GCBS      gCBS   `xml:"gCBS"`
}

type gIBS struct {
	GIBSuf   gIBSuf  `xml:"gIBSUF"`
	GIBSMun  gIBSMun `xml:"gIBSMun"`
	VIBS     string  `xml:"vIBS"`
	VCredPres string `xml:"vCredPres"`
}

type gIBSuf  struct{ VIBSuf  string `xml:"vIBSUF"` }
type gIBSMun struct{ VIBSMun string `xml:"vIBSMun"` }

type gCBS struct {
	VCBS      string `xml:"vCBS"`
	VCredPres string `xml:"vCredPres"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func toDecimal(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func toNullDecimal(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// nfeCharsetReader converte encodings declarados no XML (ex: windows-1252) para UTF-8.
func nfeCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.ReplaceAll(charset, "-", "")) {
	case "windows1252", "cp1252":
		return transform.NewReader(input, charmap.Windows1252.NewDecoder()), nil
	case "iso88591", "latin1":
		return transform.NewReader(input, charmap.ISO8859_1.NewDecoder()), nil
	default:
		return nil, fmt.Errorf("encoding não suportado: %s", charset)
	}
}

// parseNFeXML lê bytes de um XML de NF-e e retorna os dados estruturados.
func parseNFeXML(data []byte) (*nfeProc, error) {
	// Remove namespace para simplificar o parsing
	data = bytes.ReplaceAll(data,
		[]byte(` xmlns="http://www.portalfiscal.inf.br/nfe"`), []byte(""))
	// Namespace com aspas simples (menos comum, mas por segurança)
	data = bytes.ReplaceAll(data,
		[]byte(` xmlns='http://www.portalfiscal.inf.br/nfe'`), []byte(""))

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = nfeCharsetReader

	var proc nfeProc
	if err := dec.Decode(&proc); err != nil {
		return nil, fmt.Errorf("erro ao parsear XML: %w", err)
	}
	return &proc, nil
}

// extractChave retorna a chave de acesso de 44 dígitos.
func extractChave(proc *nfeProc) string {
	// Preferencial: protNFe/infProt/chNFe
	ch := strings.TrimSpace(proc.ProtNFe.InfProt.ChNFe)
	if len(ch) == 44 {
		return ch
	}
	// Fallback: atributo Id de infNFe (formato: "NFe" + 44 dígitos)
	id := strings.TrimSpace(proc.NFe.InfNFe.ID)
	if strings.HasPrefix(id, "NFe") && len(id) == 47 {
		return id[3:]
	}
	return ""
}

// parseDhEmi converte dhEmi ISO8601 em data e mes_ano.
func parseDhEmi(dhEmi string) (time.Time, string, error) {
	dhEmi = strings.TrimSpace(dhEmi)
	// Formatos possíveis: "2026-02-26T12:00:00-03:00" ou "2026-02-26"
	formats := []string{
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	var t time.Time
	var err error
	for _, f := range formats {
		t, err = time.Parse(f, dhEmi)
		if err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, "", fmt.Errorf("data inválida '%s'", dhEmi)
	}
	mesAno := fmt.Sprintf("%02d/%04d", t.Month(), t.Year())
	return t, mesAno, nil
}

// ---------------------------------------------------------------------------
// Resposta JSON de upload
// ---------------------------------------------------------------------------

type nfeSaidaErro struct {
	Arquivo string `json:"arquivo"`
	Erro    string `json:"erro"`
}

type nfeSaidaUploadResult struct {
	Importados int             `json:"importados"`
	Ignorados  int             `json:"ignorados"` // duplicatas
	Erros      []nfeSaidaErro  `json:"erros"`
}

// ---------------------------------------------------------------------------
// NfeSaidasUploadHandler — POST /api/nfe-saidas/upload
// ---------------------------------------------------------------------------

func NfeSaidasUploadHandler(db *sql.DB) http.HandlerFunc {
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

		result := nfeSaidaUploadResult{Erros: []nfeSaidaErro{}}
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

			// Ignora partes que não sejam o campo "xmls"
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
				result.Erros = append(result.Erros, nfeSaidaErro{filename, "Erro ao ler: " + err.Error()})
				continue
			}

			proc, err := parseNFeXML(data)
			if err != nil {
				result.Erros = append(result.Erros, nfeSaidaErro{filename, err.Error()})
				continue
			}

			inf := proc.NFe.InfNFe

			// Valida modelo: apenas 55 (NF-e) e 65 (NFC-e)
			mod := strings.TrimSpace(inf.Ide.Mod)
			if mod != "55" && mod != "65" {
				result.Ignorados++
				continue
			}

			// Valida tipo: apenas saída (tpNF=1)
			if strings.TrimSpace(inf.Ide.TpNF) != "1" {
				result.Ignorados++
				continue
			}

			// Extrai chave
			chave := extractChave(proc)
			if len(chave) != 44 {
				result.Erros = append(result.Erros, nfeSaidaErro{filename, "Chave de acesso inválida ou ausente"})
				continue
			}

			// Parseia data de emissão
			dataEmissao, mesAno, err := parseDhEmi(inf.Ide.DhEmi)
			if err != nil {
				result.Erros = append(result.Erros, nfeSaidaErro{filename, err.Error()})
				continue
			}

			// Determina CNPJ/CPF do destinatário
			destCNPJCPF := strings.TrimSpace(inf.Dest.CNPJ)
			if destCNPJCPF == "" {
				destCNPJCPF = strings.TrimSpace(inf.Dest.CPF)
			}

			modInt, _ := strconv.Atoi(mod)
			ic := inf.Total.ICMSTot
			ib := inf.Total.IBSCBSTot

			_, err = db.Exec(`
				INSERT INTO nfe_saidas (
					company_id, chave_nfe, modelo, serie, numero_nfe,
					data_emissao, mes_ano, nat_op,
					emit_cnpj, emit_nome, emit_uf, emit_municipio,
					dest_cnpj_cpf, dest_nome, dest_uf, dest_c_mun,
					v_bc, v_icms, v_icms_deson, v_fcp,
					v_bc_st, v_st, v_fcp_st, v_fcp_st_ret,
					v_prod, v_frete, v_seg, v_desc,
					v_ii, v_ipi, v_ipi_devol, v_pis, v_cofins, v_outro, v_nf,
					v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cred_pres_ibs,
					v_cbs, v_cred_pres_cbs
				) VALUES (
					$1,$2,$3,$4,$5,
					$6,$7,$8,
					$9,$10,$11,$12,
					$13,$14,$15,$16,
					$17,$18,$19,$20,
					$21,$22,$23,$24,
					$25,$26,$27,$28,
					$29,$30,$31,$32,$33,$34,$35,
					$36,$37,$38,$39,$40,
					$41,$42
				)
				ON CONFLICT ON CONSTRAINT uq_nfe_saidas_company_chave DO NOTHING`,
				companyID, chave, modInt, inf.Ide.Serie, inf.Ide.NNF,
				dataEmissao, mesAno, inf.Ide.NatOp,
				inf.Emit.CNPJ, inf.Emit.XNome, inf.Emit.EnderEmit.UF, inf.Emit.EnderEmit.XMun,
				destCNPJCPF, inf.Dest.XNome, inf.Dest.EnderDest.UF, inf.Dest.EnderDest.CMun,
				toDecimal(ic.VBC), toDecimal(ic.VICMS), toDecimal(ic.VICMSDeson), toDecimal(ic.VFCP),
				toDecimal(ic.VBCST), toDecimal(ic.VST), toDecimal(ic.VFcpST), toDecimal(ic.VFcpSTRet),
				toDecimal(ic.VProd), toDecimal(ic.VFrete), toDecimal(ic.VSeg), toDecimal(ic.VDesc),
				toDecimal(ic.VII), toDecimal(ic.VIPI), toDecimal(ic.VIPIDevol), toDecimal(ic.VPIS), toDecimal(ic.VCOFINS), toDecimal(ic.VOutro), toDecimal(ic.VNF),
				toNullDecimal(ib.VBCIBSCBS), toNullDecimal(ib.GIBS.GIBSuf.VIBSuf), toNullDecimal(ib.GIBS.GIBSMun.VIBSMun),
				toNullDecimal(ib.GIBS.VIBS), toNullDecimal(ib.GIBS.VCredPres),
				toNullDecimal(ib.GCBS.VCBS), toNullDecimal(ib.GCBS.VCredPres),
			)
			if err != nil {
				log.Printf("NfeSaidas INSERT error [%s]: %v", chave, err)
				result.Erros = append(result.Erros, nfeSaidaErro{filename, "Erro ao salvar no banco: " + err.Error()})
				continue
			}

			// Save raw XML to dfe_xml for DANFE generation
			_, _ = db.Exec(`
				INSERT INTO dfe_xml (company_id, chave, tipo, modelo, xml_raw)
				VALUES ($1, $2, 'nfe', $3, $4)
				ON CONFLICT ON CONSTRAINT uq_dfe_xml_company_chave DO UPDATE SET xml_raw = EXCLUDED.xml_raw
			`, companyID, chave, modInt, string(data))

			result.Importados++
		}

		if totalParts == 0 {
			jsonErr(w, http.StatusBadRequest, "Nenhum arquivo enviado (campo 'xmls')")
			return
		}

		// Ajusta ignorados
		result.Ignorados = totalParts - result.Importados - len(result.Erros) - result.Ignorados
		if result.Ignorados < 0 {
			result.Ignorados = 0
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}
}

// ---------------------------------------------------------------------------
// NfeSaidasListHandler — GET /api/nfe-saidas
// ---------------------------------------------------------------------------

type nfeSaidaRow struct {
	// Identificação
	ID          string `json:"id"`
	ChaveNFe    string `json:"chave_nfe"`
	Modelo      int    `json:"modelo"`
	Serie       string `json:"serie"`
	NumeroNFe   string `json:"numero_nfe"`
	DataEmissao string `json:"data_emissao"`
	MesAno      string `json:"mes_ano"`
	NatOp       string `json:"nat_op"`
	// Emitente
	EmitCNPJ      string `json:"emit_cnpj"`
	EmitNome      string `json:"emit_nome"`
	EmitUF        string `json:"emit_uf"`
	EmitMunicipio string `json:"emit_municipio"`
	// Destinatário
	DestCNPJCPF string `json:"dest_cnpj_cpf"`
	DestNome    string `json:"dest_nome"`
	DestUF      string `json:"dest_uf"`
	DestCMun    string `json:"dest_c_mun"`
	// ICMSTot
	VBC       float64 `json:"v_bc"`
	VICMS     float64 `json:"v_icms"`
	VICMSDeson float64 `json:"v_icms_deson"`
	VFCP      float64 `json:"v_fcp"`
	VBcST     float64 `json:"v_bc_st"`
	VST       float64 `json:"v_st"`
	VFcpST    float64 `json:"v_fcp_st"`
	VFcpSTRet float64 `json:"v_fcp_st_ret"`
	VProd     float64 `json:"v_prod"`
	VFrete    float64 `json:"v_frete"`
	VSeg      float64 `json:"v_seg"`
	VDesc     float64 `json:"v_desc"`
	VII       float64 `json:"v_ii"`
	VIPI      float64 `json:"v_ipi"`
	VIPIDevol float64 `json:"v_ipi_devol"`
	VPIS      float64 `json:"v_pis"`
	VCOFINS   float64 `json:"v_cofins"`
	VOutro    float64 `json:"v_outro"`
	VNF       float64 `json:"v_nf"`
	// IBSCBSTot
	VBCIbsCbs   *float64 `json:"v_bc_ibs_cbs"`
	VIBSuf      *float64 `json:"v_ibs_uf"`
	VIBSMun     *float64 `json:"v_ibs_mun"`
	VIBS        *float64 `json:"v_ibs"`
	VCredPresIBS *float64 `json:"v_cred_pres_ibs"`
	VCBS         *float64 `json:"v_cbs"`
	VCredPresCBS *float64 `json:"v_cred_pres_cbs"`
}

func NfeSaidasListHandler(db *sql.DB) http.HandlerFunc {
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

		// ── COUNT total ───────────────────────────────────────────────────────
		var total int
		if err := db.QueryRow("SELECT COUNT(*) FROM nfe_saidas "+where, args...).Scan(&total); err != nil {
			log.Printf("NfeSaidasList count error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		// ── Totalizadores (mesmos filtros) ────────────────────────────────────
		var totVNF, totICMS, totIBS, totCBS float64
		db.QueryRow(
			"SELECT COALESCE(SUM(v_nf),0), COALESCE(SUM(v_icms),0), COALESCE(SUM(v_ibs),0), COALESCE(SUM(v_cbs),0) FROM nfe_saidas "+where,
			args...,
		).Scan(&totVNF, &totICMS, &totIBS, &totCBS)

		// ── SELECT paginado ───────────────────────────────────────────────────
		offset := (page - 1) * pageSize
		selectQ := `
			SELECT
				id, chave_nfe, modelo, serie, numero_nfe,
				TO_CHAR(data_emissao, 'DD/MM/YYYY'), mes_ano, COALESCE(nat_op,''),
				emit_cnpj, COALESCE(emit_nome,''), COALESCE(emit_uf,''), COALESCE(emit_municipio,''),
				COALESCE(dest_cnpj_cpf,''), COALESCE(dest_nome,''), COALESCE(dest_uf,''), COALESCE(dest_c_mun,''),
				v_bc, v_icms, v_icms_deson, v_fcp,
				v_bc_st, v_st, v_fcp_st, v_fcp_st_ret,
				v_prod, v_frete, v_seg, v_desc,
				v_ii, v_ipi, v_ipi_devol, v_pis, v_cofins, v_outro, v_nf,
				v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cred_pres_ibs,
				v_cbs, v_cred_pres_cbs
			FROM nfe_saidas ` + where +
			fmt.Sprintf(" ORDER BY data_emissao DESC, numero_nfe DESC LIMIT $%d OFFSET $%d", idx, idx+1)
		pageArgs := append(args, pageSize, offset)

		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			log.Printf("NfeSaidasList error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()

		list := []nfeSaidaRow{}
		for rows.Next() {
			var row nfeSaidaRow
			err := rows.Scan(
				&row.ID, &row.ChaveNFe, &row.Modelo, &row.Serie, &row.NumeroNFe,
				&row.DataEmissao, &row.MesAno, &row.NatOp,
				&row.EmitCNPJ, &row.EmitNome, &row.EmitUF, &row.EmitMunicipio,
				&row.DestCNPJCPF, &row.DestNome, &row.DestUF, &row.DestCMun,
				&row.VBC, &row.VICMS, &row.VICMSDeson, &row.VFCP,
				&row.VBcST, &row.VST, &row.VFcpST, &row.VFcpSTRet,
				&row.VProd, &row.VFrete, &row.VSeg, &row.VDesc,
				&row.VII, &row.VIPI, &row.VIPIDevol, &row.VPIS, &row.VCOFINS, &row.VOutro, &row.VNF,
				&row.VBCIbsCbs, &row.VIBSuf, &row.VIBSMun, &row.VIBS, &row.VCredPresIBS,
				&row.VCBS, &row.VCredPresCBS,
			)
			if err != nil {
				log.Printf("NfeSaidasList scan error: %v", err)
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
				"v_nf":   totVNF,
				"v_icms": totICMS,
				"v_ibs":  totIBS,
				"v_cbs":  totCBS,
			},
			"items": list,
		})
	}
}

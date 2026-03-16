package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Tipos exclusivos de resposta JSON para NF-e Entradas
// (structs XML reutilizados de nfe_saidas.go — mesmo pacote)
// ---------------------------------------------------------------------------

type nfeEntradaErro struct {
	Arquivo string `json:"arquivo"`
	Erro    string `json:"erro"`
}

type nfeEntradaUploadResult struct {
	Importados int              `json:"importados"`
	Ignorados  int              `json:"ignorados"`
	Erros      []nfeEntradaErro `json:"erros"`
}

type nfeEntradaRow struct {
	// Identificação
	ID          string `json:"id"`
	ChaveNFe    string `json:"chave_nfe"`
	Modelo      int    `json:"modelo"`
	Serie       string `json:"serie"`
	NumeroNFe   string `json:"numero_nfe"`
	DataEmissao string `json:"data_emissao"`
	MesAno      string `json:"mes_ano"`
	NatOp       string `json:"nat_op"`
	// Fornecedor
	FornCNPJ      string `json:"forn_cnpj"`
	FornNome      string `json:"forn_nome"`
	FornUF        string `json:"forn_uf"`
	FornMunicipio string `json:"forn_municipio"`
	// Destinatário
	DestCNPJCPF string `json:"dest_cnpj_cpf"`
	DestNome    string `json:"dest_nome"`
	DestUF      string `json:"dest_uf"`
	DestCMun    string `json:"dest_c_mun"`
	// ICMSTot
	VBC        float64 `json:"v_bc"`
	VICMS      float64 `json:"v_icms"`
	VICMSDeson float64 `json:"v_icms_deson"`
	VFCP       float64 `json:"v_fcp"`
	VBcST      float64 `json:"v_bc_st"`
	VST        float64 `json:"v_st"`
	VFcpST     float64 `json:"v_fcp_st"`
	VFcpSTRet  float64 `json:"v_fcp_st_ret"`
	VProd      float64 `json:"v_prod"`
	VFrete     float64 `json:"v_frete"`
	VSeg       float64 `json:"v_seg"`
	VDesc      float64 `json:"v_desc"`
	VII        float64 `json:"v_ii"`
	VIPI       float64 `json:"v_ipi"`
	VIPIDevol  float64 `json:"v_ipi_devol"`
	VPIS       float64 `json:"v_pis"`
	VCOFINS    float64 `json:"v_cofins"`
	VOutro     float64 `json:"v_outro"`
	VNF        float64 `json:"v_nf"`
	// IBSCBSTot — sempre float64 (nunca null): fornecedores sem tags ficam com 0
	VBCIbsCbs   float64 `json:"v_bc_ibs_cbs"`
	VIBSuf      float64 `json:"v_ibs_uf"`
	VIBSMun     float64 `json:"v_ibs_mun"`
	VIBS        float64 `json:"v_ibs"`
	VCredPresIBS float64 `json:"v_cred_pres_ibs"`
	VCBS         float64 `json:"v_cbs"`
	VCredPresCBS float64 `json:"v_cred_pres_cbs"`
}

// ---------------------------------------------------------------------------
// NfeEntradasUploadHandler — POST /api/nfe-entradas/upload
// ---------------------------------------------------------------------------

func NfeEntradasUploadHandler(db *sql.DB) http.HandlerFunc {
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

		result := nfeEntradaUploadResult{Erros: []nfeEntradaErro{}}
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
				result.Erros = append(result.Erros, nfeEntradaErro{filename, "Erro ao ler: " + err.Error()})
				continue
			}

			proc, err := parseNFeXML(data)
			if err != nil {
				result.Erros = append(result.Erros, nfeEntradaErro{filename, err.Error()})
				continue
			}

			inf := proc.NFe.InfNFe

			// Valida modelo: apenas 55 (NF-e) e 65 (NFC-e)
			// Nota: tpNF NÃO é verificado aqui porque NF-es recebidas de fornecedores
			// sempre têm tpNF=1 no XML (saída do ponto de vista do emitente).
			mod := strings.TrimSpace(inf.Ide.Mod)
			if mod != "55" && mod != "65" {
				result.Ignorados++
				continue
			}

			// Extrai chave
			chave := extractChave(proc)
			if len(chave) != 44 {
				result.Erros = append(result.Erros, nfeEntradaErro{filename, "Chave de acesso inválida ou ausente"})
				continue
			}

			// Parseia data de emissão
			dataEmissao, mesAno, err := parseDhEmi(inf.Ide.DhEmi)
			if err != nil {
				result.Erros = append(result.Erros, nfeEntradaErro{filename, err.Error()})
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

			// IBS/CBS: usa toDecimal (não toNullDecimal) — fornecedores sem tags ficam com 0
			_, err = db.Exec(`
				INSERT INTO nfe_entradas (
					company_id, chave_nfe, modelo, serie, numero_nfe,
					data_emissao, mes_ano, nat_op,
					forn_cnpj, forn_nome, forn_uf, forn_municipio,
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
				ON CONFLICT ON CONSTRAINT uq_nfe_entradas_company_chave DO NOTHING`,
				companyID, chave, modInt, inf.Ide.Serie, inf.Ide.NNF,
				dataEmissao, mesAno, inf.Ide.NatOp,
				inf.Emit.CNPJ, inf.Emit.XNome, inf.Emit.EnderEmit.UF, inf.Emit.EnderEmit.XMun,
				destCNPJCPF, inf.Dest.XNome, inf.Dest.EnderDest.UF, inf.Dest.EnderDest.CMun,
				toDecimal(ic.VBC), toDecimal(ic.VICMS), toDecimal(ic.VICMSDeson), toDecimal(ic.VFCP),
				toDecimal(ic.VBCST), toDecimal(ic.VST), toDecimal(ic.VFcpST), toDecimal(ic.VFcpSTRet),
				toDecimal(ic.VProd), toDecimal(ic.VFrete), toDecimal(ic.VSeg), toDecimal(ic.VDesc),
				toDecimal(ic.VII), toDecimal(ic.VIPI), toDecimal(ic.VIPIDevol), toDecimal(ic.VPIS), toDecimal(ic.VCOFINS), toDecimal(ic.VOutro), toDecimal(ic.VNF),
				toDecimal(ib.VBCIBSCBS), toDecimal(ib.GIBS.GIBSuf.VIBSuf), toDecimal(ib.GIBS.GIBSMun.VIBSMun),
				toDecimal(ib.GIBS.VIBS), toDecimal(ib.GIBS.VCredPres),
				toDecimal(ib.GCBS.VCBS), toDecimal(ib.GCBS.VCredPres),
			)
			if err != nil {
				log.Printf("NfeEntradas INSERT error [%s]: %v", chave, err)
				result.Erros = append(result.Erros, nfeEntradaErro{filename, "Erro ao salvar no banco: " + err.Error()})
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

		result.Ignorados = totalParts - result.Importados - len(result.Erros) - result.Ignorados
		if result.Ignorados < 0 {
			result.Ignorados = 0
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}
}

// ---------------------------------------------------------------------------
// NfeEntradasListHandler — GET /api/nfe-entradas
// ---------------------------------------------------------------------------

func NfeEntradasListHandler(db *sql.DB) http.HandlerFunc {
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
		fornCNPJ := q.Get("forn_cnpj")
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
		if fornCNPJ != "" { where += fmt.Sprintf(" AND forn_cnpj = $%d", idx); args = append(args, fornCNPJ); idx++ }
		if modelo != "" { where += fmt.Sprintf(" AND modelo = $%d", idx); args = append(args, modelo); idx++ }
		if dataDe != "" { where += fmt.Sprintf(" AND data_emissao >= $%d", idx); args = append(args, dataDe); idx++ }
		if dataAte != "" { where += fmt.Sprintf(" AND data_emissao <= $%d", idx); args = append(args, dataAte); idx++ }
		if destCNPJ != "" { where += fmt.Sprintf(" AND dest_cnpj_cpf LIKE $%d", idx); args = append(args, destCNPJ+"%"); idx++ }
		if destNome := q.Get("dest_nome"); destNome != "" { where += fmt.Sprintf(" AND dest_nome ILIKE $%d", idx); args = append(args, "%"+destNome+"%"); idx++ }
		if fornNome := q.Get("forn_nome"); fornNome != "" { where += fmt.Sprintf(" AND forn_nome ILIKE $%d", idx); args = append(args, "%"+fornNome+"%"); idx++ }
		if fornCNPJSearch := q.Get("forn_cnpj_search"); fornCNPJSearch != "" { where += fmt.Sprintf(" AND forn_cnpj LIKE $%d", idx); args = append(args, fornCNPJSearch+"%"); idx++ }
		if q.Get("sem_ibs_cbs") == "true" { where += " AND (v_ibs = 0 AND v_cbs = 0)" }

		// ── COUNT total ───────────────────────────────────────────────────────
		var total int
		if err := db.QueryRow("SELECT COUNT(*) FROM nfe_entradas "+where, args...).Scan(&total); err != nil {
			log.Printf("NfeEntradasList count error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		// ── Totalizadores ─────────────────────────────────────────────────────
		var totVNF, totICMS, totIBS, totCBS float64
		db.QueryRow(
			"SELECT COALESCE(SUM(v_nf),0), COALESCE(SUM(v_icms),0), COALESCE(SUM(v_ibs),0), COALESCE(SUM(v_cbs),0) FROM nfe_entradas "+where,
			args...,
		).Scan(&totVNF, &totICMS, &totIBS, &totCBS)

		// ── SELECT paginado ───────────────────────────────────────────────────
		offset := (page - 1) * pageSize
		selectQ := `
			SELECT
				id, chave_nfe, modelo, serie, numero_nfe,
				TO_CHAR(data_emissao, 'DD/MM/YYYY'), mes_ano, COALESCE(nat_op,''),
				forn_cnpj, COALESCE(forn_nome,''), COALESCE(forn_uf,''), COALESCE(forn_municipio,''),
				COALESCE(dest_cnpj_cpf,''), COALESCE(dest_nome,''), COALESCE(dest_uf,''), COALESCE(dest_c_mun,''),
				v_bc, v_icms, v_icms_deson, v_fcp,
				v_bc_st, v_st, v_fcp_st, v_fcp_st_ret,
				v_prod, v_frete, v_seg, v_desc,
				v_ii, v_ipi, v_ipi_devol, v_pis, v_cofins, v_outro, v_nf,
				v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cred_pres_ibs,
				v_cbs, v_cred_pres_cbs
			FROM nfe_entradas ` + where +
			fmt.Sprintf(" ORDER BY data_emissao DESC, numero_nfe DESC LIMIT $%d OFFSET $%d", idx, idx+1)
		pageArgs := append(args, pageSize, offset)

		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			log.Printf("NfeEntradasList error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()

		list := []nfeEntradaRow{}
		for rows.Next() {
			var row nfeEntradaRow
			err := rows.Scan(
				&row.ID, &row.ChaveNFe, &row.Modelo, &row.Serie, &row.NumeroNFe,
				&row.DataEmissao, &row.MesAno, &row.NatOp,
				&row.FornCNPJ, &row.FornNome, &row.FornUF, &row.FornMunicipio,
				&row.DestCNPJCPF, &row.DestNome, &row.DestUF, &row.DestCMun,
				&row.VBC, &row.VICMS, &row.VICMSDeson, &row.VFCP,
				&row.VBcST, &row.VST, &row.VFcpST, &row.VFcpSTRet,
				&row.VProd, &row.VFrete, &row.VSeg, &row.VDesc,
				&row.VII, &row.VIPI, &row.VIPIDevol, &row.VPIS, &row.VCOFINS, &row.VOutro, &row.VNF,
				&row.VBCIbsCbs, &row.VIBSuf, &row.VIBSMun, &row.VIBS, &row.VCredPresIBS,
				&row.VCBS, &row.VCredPresCBS,
			)
			if err != nil {
				log.Printf("NfeEntradasList scan error: %v", err)
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

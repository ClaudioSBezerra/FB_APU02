package handlers

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"fb_apu02/services"
)

// maxUploadBytes limita o corpo total de uploads (multipart) para conter DoS de
// disco/memória. Configurável via MAX_UPLOAD_BYTES (bytes); default 512 MiB —
// generoso para importações de pastas de XML sem permitir corpos arbitrários.
func maxUploadBytes() int64 {
	if v := os.Getenv("MAX_UPLOAD_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 512 << 20
}

// ---------------------------------------------------------------------------
// XML structs — NF-e
// ---------------------------------------------------------------------------

// nfeDocXML accepts both <nfeProc> and bare <NFe> roots (no XMLName constraint).
type nfeDocXML struct {
	NFe struct {
		InfNFe nfeInfXML `xml:"infNFe"`
	} `xml:"NFe"`
	InfNFe  nfeInfXML `xml:"infNFe"` // bare <NFe> root
	ProtNFe struct {
		InfProt struct {
			ChNFe string `xml:"chNFe"`
		} `xml:"infProt"`
	} `xml:"protNFe"`
}

type nfeInfXML struct {
	Id  string `xml:"Id,attr"`
	Ide struct {
		Mod   string `xml:"mod"`
		Serie string `xml:"serie"`
		NNF   string `xml:"nNF"`
		DhEmi string `xml:"dhEmi"`
		NatOp string `xml:"natOp"`
	} `xml:"ide"`
	Emit struct {
		CNPJ          string `xml:"CNPJ"`
		IdEstrangeiro string `xml:"idEstrangeiro"`
		XNome         string `xml:"xNome"`
		EnderEmit     struct {
			UF string `xml:"UF"`
		} `xml:"enderEmit"`
	} `xml:"emit"`
	Dest struct {
		CNPJ      string `xml:"CNPJ"`
		CPF       string `xml:"CPF"`
		XNome     string `xml:"xNome"`
		EnderDest struct {
			UF   string `xml:"UF"`
			CMun string `xml:"cMun"`
		} `xml:"enderDest"`
	} `xml:"dest"`
	Det []struct {
		Prod struct {
			CFOP string `xml:"CFOP"`
		} `xml:"prod"`
	} `xml:"det"`
	Total struct {
		ICMSTot struct {
			VBC     string `xml:"vBC"`
			VICMS   string `xml:"vICMS"`
			VProd   string `xml:"vProd"`
			VNF     string `xml:"vNF"`
			VPIS    string `xml:"vPIS"`
			VCofins string `xml:"vCOFINS"`
		} `xml:"ICMSTot"`
		IBSCBSTot struct {
			VBcIbsCbs string `xml:"vBCIBSCBS"`
			GIBS      struct {
				GIBSuf struct {
					VIBSuf string `xml:"vIBSUF"`
				} `xml:"gIBSUF"`
				GIBSMun struct {
					VIBSMun string `xml:"vIBSMun"`
				} `xml:"gIBSMun"`
				VIBS string `xml:"vIBS"`
			} `xml:"gIBS"`
			GCBS struct {
				VCBS string `xml:"vCBS"`
			} `xml:"gCBS"`
		} `xml:"IBSCBSTot"`
	} `xml:"total"`
}

// ---------------------------------------------------------------------------
// XML structs — CT-e
// ---------------------------------------------------------------------------

// cteDocXML accepts both <cteProc> and bare <CTe> roots.
type cteDocXML struct {
	CTe struct {
		InfCte cteInfXML `xml:"infCte"`
	} `xml:"CTe"`
	InfCte  cteInfXML `xml:"infCte"` // bare <CTe> root
	ProtCTe struct {
		InfProt struct {
			ChCTe string `xml:"chCTe"`
		} `xml:"infProt"`
	} `xml:"protCTe"`
}

type cteInfXML struct {
	Id  string `xml:"Id,attr"`
	Ide struct {
		Mod   string `xml:"mod"`
		Serie string `xml:"serie"`
		NCT   string `xml:"nCT"`
		DhEmi string `xml:"dhEmi"`
		NatOp string `xml:"natOp"`
		CFOP  string `xml:"CFOP"`
		Modal string `xml:"modal"`
	} `xml:"ide"`
	Emit struct {
		CNPJ      string `xml:"CNPJ"`
		XNome     string `xml:"xNome"`
		EnderEmit struct {
			UF string `xml:"UF"`
		} `xml:"enderEmit"`
	} `xml:"emit"`
	Rem struct {
		CNPJ      string `xml:"CNPJ"`
		CPF       string `xml:"CPF"`
		XNome     string `xml:"xNome"`
		EnderReme struct {
			UF string `xml:"UF"`
		} `xml:"enderReme"`
	} `xml:"rem"`
	Dest struct {
		CNPJ      string `xml:"CNPJ"`
		CPF       string `xml:"CPF"`
		XNome     string `xml:"xNome"`
		EnderDest struct {
			UF string `xml:"UF"`
		} `xml:"enderDest"`
	} `xml:"dest"`
	VPrest struct {
		VTPrest string `xml:"vTPrest"`
	} `xml:"vPrest"`
	Imp struct {
		IBSCBSTot struct {
			VBcIbsCbs string `xml:"vBCIBSCBS"`
			GIBS      struct {
				VIBSuf  string `xml:"vIBSUF"`
				VIBSMun string `xml:"vIBSMun"`
				VIBS    string `xml:"vIBS"`
			} `xml:"gIBS"`
			GCBS struct {
				VCBS string `xml:"vCBS"`
			} `xml:"gCBS"`
		} `xml:"IBSCBSTot"`
	} `xml:"imp"`
}

// ---------------------------------------------------------------------------
// Parsed intermediary types
// ---------------------------------------------------------------------------

type parsedNFe struct {
	Chave       string
	Modelo      int
	Serie       string
	Numero      string
	DataEmissao string // YYYY-MM-DD
	MesAno      string // MM/YYYY
	EmitCNPJ    string
	EmitNome    string
	EmitUF      string
	DestCNPJCPF string
	DestNome    string
	DestUF      string
	DestCMun    string
	CFOP        string
	VNF         float64
	VBCIBS      float64
	VIBSuf      float64
	VIBSMun     float64
	VIBS        float64
	VCBS        float64
}

type parsedCTe struct {
	Chave       string
	Modelo      int
	Serie       string
	Numero      string
	DataEmissao string
	MesAno      string
	NatOp       string
	CFOP        string
	Modal       string
	EmitCNPJ    string
	EmitNome    string
	EmitUF      string
	RemCNPJCPF  string
	RemNome     string
	RemUF       string
	DestCNPJCPF string
	DestNome    string
	DestUF      string
	VPrest      float64
	VBCIBS      float64
	VIBSuf      float64
	VIBSMun     float64
	VIBS        float64
	VCBS        float64
}

// ---------------------------------------------------------------------------
// Upload result type (shared by all 3 handlers)
// ---------------------------------------------------------------------------

type xmlUploadErr struct {
	Arquivo string `json:"arquivo"`
	Erro    string `json:"erro"`
}

type xmlUploadResult struct {
	Importados int            `json:"importados"`
	Ignorados  int            `json:"ignorados"`
	Erros      []xmlUploadErr `json:"erros"`
}

// ---------------------------------------------------------------------------
// NfeEntradasUploadHandler — POST /api/nfe-entradas/upload
// ---------------------------------------------------------------------------

func NfeEntradasUploadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Não autenticado")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao identificar empresa", err, "[NFeEntradasUpload]")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes())
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			jsonErr(w, http.StatusBadRequest, "Erro ao ler multipart: "+err.Error())
			return
		}

		result := xmlUploadResult{Erros: []xmlUploadErr{}}

		for _, fh := range r.MultipartForm.File["xmls"] {
			f, err := fh.Open()
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: "erro ao abrir arquivo"})
				continue
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: "erro ao ler arquivo"})
				continue
			}

			nfe, err := parseNFeXML(data)
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: err.Error()})
				continue
			}

			inserted, err := xmlInsertNFeEntrada(db, companyID, nfe)
			if err != nil {
				log.Printf("[NFeEntradasUpload] INSERT error [%s]: %v", nfe.Chave, err)
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: err.Error()})
				continue
			}
			if inserted {
				result.Importados++
				upsertParceiroDB(db, companyID, nfe.EmitCNPJ, nfe.EmitNome)
			} else {
				result.Ignorados++
			}
		}

		if result.Importados > 0 {
			services.RequestMVRefresh(db, "mv_malha_fina_resumo")
		}

		json.NewEncoder(w).Encode(result)
	}
}

// ---------------------------------------------------------------------------
// NfeSaidasUploadHandler — POST /api/nfe-saidas/upload
// ---------------------------------------------------------------------------

func NfeSaidasUploadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Não autenticado")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao identificar empresa", err, "[NFeSaidasUpload]")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes())
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			jsonErr(w, http.StatusBadRequest, "Erro ao ler multipart: "+err.Error())
			return
		}

		result := xmlUploadResult{Erros: []xmlUploadErr{}}

		for _, fh := range r.MultipartForm.File["xmls"] {
			f, err := fh.Open()
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: "erro ao abrir arquivo"})
				continue
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: "erro ao ler arquivo"})
				continue
			}

			nfe, err := parseNFeXML(data)
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: err.Error()})
				continue
			}

			inserted, err := xmlInsertNFeSaida(db, companyID, nfe)
			if err != nil {
				log.Printf("[NFeSaidasUpload] INSERT error [%s]: %v", nfe.Chave, err)
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: err.Error()})
				continue
			}
			if inserted {
				result.Importados++
				upsertParceiroDB(db, companyID, nfe.DestCNPJCPF, nfe.DestNome)
			} else {
				result.Ignorados++
			}
		}

		if result.Importados > 0 {
			services.RequestMVRefresh(db, "mv_malha_fina_resumo")
		}

		json.NewEncoder(w).Encode(result)
	}
}

// ---------------------------------------------------------------------------
// CteEntradasUploadHandler — POST /api/cte-entradas/upload
// ---------------------------------------------------------------------------

func CteEntradasUploadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Não autenticado")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao identificar empresa", err, "[CTeEntradasUpload]")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes())
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			jsonErr(w, http.StatusBadRequest, "Erro ao ler multipart: "+err.Error())
			return
		}

		result := xmlUploadResult{Erros: []xmlUploadErr{}}

		for _, fh := range r.MultipartForm.File["xmls"] {
			f, err := fh.Open()
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: "erro ao abrir arquivo"})
				continue
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: "erro ao ler arquivo"})
				continue
			}

			cte, err := parseCTeXML(data)
			if err != nil {
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: err.Error()})
				continue
			}

			inserted, err := xmlInsertCTeEntrada(db, companyID, cte)
			if err != nil {
				log.Printf("[CTeEntradasUpload] INSERT error [%s]: %v", cte.Chave, err)
				result.Erros = append(result.Erros, xmlUploadErr{Arquivo: fh.Filename, Erro: err.Error()})
				continue
			}
			if inserted {
				result.Importados++
				upsertParceiroDB(db, companyID, cte.EmitCNPJ, cte.EmitNome)
			} else {
				result.Ignorados++
			}
		}

		if result.Importados > 0 {
			services.RequestMVRefresh(db, "mv_malha_fina_resumo")
		}

		json.NewEncoder(w).Encode(result)
	}
}

// ---------------------------------------------------------------------------
// XML parsing helpers
// ---------------------------------------------------------------------------

func parseNFeXML(data []byte) (*parsedNFe, error) {
	var doc nfeDocXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("XML inválido: %w", err)
	}

	inf := doc.NFe.InfNFe
	if inf.Ide.Mod == "" {
		inf = doc.InfNFe
	}
	if inf.Ide.Mod == "" {
		return nil, fmt.Errorf("NF-e não encontrada no XML")
	}

	chave := doc.ProtNFe.InfProt.ChNFe
	if chave == "" {
		id := strings.TrimSpace(inf.Id)
		if strings.HasPrefix(id, "NFe") && len(id) == 47 {
			chave = id[3:]
		} else if len(id) == 44 {
			chave = id
		}
	}
	if len(chave) != 44 {
		return nil, fmt.Errorf("chave inválida: %q", chave)
	}

	modelo, _ := strconv.Atoi(strings.TrimSpace(inf.Ide.Mod))

	emitCNPJ := strings.TrimSpace(inf.Emit.CNPJ)
	if emitCNPJ == "" {
		emitCNPJ = strings.TrimSpace(inf.Emit.IdEstrangeiro)
	}

	destCNPJCPF := strings.TrimSpace(inf.Dest.CNPJ)
	if destCNPJCPF == "" {
		destCNPJCPF = strings.TrimSpace(inf.Dest.CPF)
	}

	cfop := ""
	if len(inf.Det) > 0 {
		cfop = strings.TrimSpace(inf.Det[0].Prod.CFOP)
	}

	return &parsedNFe{
		Chave:       chave,
		Modelo:      modelo,
		Serie:       strings.TrimSpace(inf.Ide.Serie),
		Numero:      strings.TrimSpace(inf.Ide.NNF),
		DataEmissao: xmlDatePart(inf.Ide.DhEmi),
		MesAno:      xmlMesAno(inf.Ide.DhEmi),
		EmitCNPJ:    emitCNPJ,
		EmitNome:    strings.TrimSpace(inf.Emit.XNome),
		EmitUF:      strings.TrimSpace(inf.Emit.EnderEmit.UF),
		DestCNPJCPF: destCNPJCPF,
		DestNome:    strings.TrimSpace(inf.Dest.XNome),
		DestUF:      strings.TrimSpace(inf.Dest.EnderDest.UF),
		DestCMun:    strings.TrimSpace(inf.Dest.EnderDest.CMun),
		CFOP:        cfop,
		VNF:         xmlFloat(inf.Total.ICMSTot.VNF),
		VBCIBS:      xmlFloat(inf.Total.IBSCBSTot.VBcIbsCbs),
		VIBSuf:      xmlFloat(inf.Total.IBSCBSTot.GIBS.GIBSuf.VIBSuf),
		VIBSMun:     xmlFloat(inf.Total.IBSCBSTot.GIBS.GIBSMun.VIBSMun),
		VIBS:        xmlFloat(inf.Total.IBSCBSTot.GIBS.VIBS),
		VCBS:        xmlFloat(inf.Total.IBSCBSTot.GCBS.VCBS),
	}, nil
}

func parseCTeXML(data []byte) (*parsedCTe, error) {
	var doc cteDocXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("XML inválido: %w", err)
	}

	inf := doc.CTe.InfCte
	if inf.Ide.Mod == "" {
		inf = doc.InfCte
	}
	if inf.Ide.Mod == "" {
		return nil, fmt.Errorf("CT-e não encontrado no XML")
	}

	chave := doc.ProtCTe.InfProt.ChCTe
	if chave == "" {
		id := strings.TrimSpace(inf.Id)
		if strings.HasPrefix(id, "CTe") && len(id) == 47 {
			chave = id[3:]
		} else if len(id) == 44 {
			chave = id
		}
	}
	if len(chave) != 44 {
		return nil, fmt.Errorf("chave inválida: %q", chave)
	}

	modelo, _ := strconv.Atoi(strings.TrimSpace(inf.Ide.Mod))

	remCNPJCPF := strings.TrimSpace(inf.Rem.CNPJ)
	if remCNPJCPF == "" {
		remCNPJCPF = strings.TrimSpace(inf.Rem.CPF)
	}
	destCNPJCPF := strings.TrimSpace(inf.Dest.CNPJ)
	if destCNPJCPF == "" {
		destCNPJCPF = strings.TrimSpace(inf.Dest.CPF)
	}

	return &parsedCTe{
		Chave:       chave,
		Modelo:      modelo,
		Serie:       strings.TrimSpace(inf.Ide.Serie),
		Numero:      strings.TrimSpace(inf.Ide.NCT),
		DataEmissao: xmlDatePart(inf.Ide.DhEmi),
		MesAno:      xmlMesAno(inf.Ide.DhEmi),
		NatOp:       strings.TrimSpace(inf.Ide.NatOp),
		CFOP:        strings.TrimSpace(inf.Ide.CFOP),
		Modal:       strings.TrimSpace(inf.Ide.Modal),
		EmitCNPJ:    strings.TrimSpace(inf.Emit.CNPJ),
		EmitNome:    strings.TrimSpace(inf.Emit.XNome),
		EmitUF:      strings.TrimSpace(inf.Emit.EnderEmit.UF),
		RemCNPJCPF:  remCNPJCPF,
		RemNome:     strings.TrimSpace(inf.Rem.XNome),
		RemUF:       strings.TrimSpace(inf.Rem.EnderReme.UF),
		DestCNPJCPF: destCNPJCPF,
		DestNome:    strings.TrimSpace(inf.Dest.XNome),
		DestUF:      strings.TrimSpace(inf.Dest.EnderDest.UF),
		VPrest:      xmlFloat(inf.VPrest.VTPrest),
		VBCIBS:      xmlFloat(inf.Imp.IBSCBSTot.VBcIbsCbs),
		VIBSuf:      xmlFloat(inf.Imp.IBSCBSTot.GIBS.VIBSuf),
		VIBSMun:     xmlFloat(inf.Imp.IBSCBSTot.GIBS.VIBSMun),
		VIBS:        xmlFloat(inf.Imp.IBSCBSTot.GIBS.VIBS),
		VCBS:        xmlFloat(inf.Imp.IBSCBSTot.GCBS.VCBS),
	}, nil
}

// ---------------------------------------------------------------------------
// DB insert helpers
// ---------------------------------------------------------------------------

func xmlInsertNFeEntrada(db *sql.DB, companyID string, nfe *parsedNFe) (bool, error) {
	res, err := db.Exec(`
		INSERT INTO nfe_entradas (
			company_id, chave_nfe, modelo, serie, numero_nfe,
			data_emissao, mes_ano,
			forn_cnpj, forn_nome, forn_uf,
			dest_cnpj_cpf, dest_nome, dest_uf,
			v_nf,
			v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
			cancelado,
			tipo_cfop, cfop
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,
			$8,$9,$10,
			$11,$12,$13,
			$14,
			$15,$16,$17,$18,$19,
			'N',
			COALESCE((SELECT tipo FROM cfop c WHERE c.cfop = NULLIF($20,'')), 'C'),
			NULLIF($20,'')
		)
		ON CONFLICT ON CONSTRAINT uq_nfe_entradas_company_chave DO NOTHING`,
		companyID, nfe.Chave, nfe.Modelo, nfe.Serie, nfe.Numero,
		nfe.DataEmissao, nfe.MesAno,
		nfe.EmitCNPJ, nullStr(nfe.EmitNome), nullStr(nfe.EmitUF),
		nullStr(nfe.DestCNPJCPF), nullStr(nfe.DestNome), nullStr(nfe.DestUF),
		nfe.VNF,
		nfe.VBCIBS, nfe.VIBSuf, nfe.VIBSMun, nfe.VIBS, nfe.VCBS,
		nfe.CFOP,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func xmlInsertNFeSaida(db *sql.DB, companyID string, nfe *parsedNFe) (bool, error) {
	res, err := db.Exec(`
		INSERT INTO nfe_saidas (
			company_id, chave_nfe, modelo, serie, numero_nfe,
			data_emissao, mes_ano,
			emit_cnpj, emit_nome, emit_uf,
			dest_cnpj_cpf, dest_nome, dest_uf,
			v_nf,
			v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
			cancelado,
			tipo_cfop, cfop
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,
			$8,$9,$10,
			$11,$12,$13,
			$14,
			$15,$16,$17,$18,$19,
			'N',
			COALESCE((SELECT tipo FROM cfop c WHERE c.cfop = NULLIF($20,'')), 'O'),
			NULLIF($20,'')
		)
		ON CONFLICT ON CONSTRAINT uq_nfe_saidas_company_chave DO NOTHING`,
		companyID, nfe.Chave, nfe.Modelo, nfe.Serie, nfe.Numero,
		nfe.DataEmissao, nfe.MesAno,
		nfe.EmitCNPJ, nullStr(nfe.EmitNome), nullStr(nfe.EmitUF),
		nullStr(nfe.DestCNPJCPF), nullStr(nfe.DestNome), nullStr(nfe.DestUF),
		nfe.VNF,
		nfe.VBCIBS, nfe.VIBSuf, nfe.VIBSMun, nfe.VIBS, nfe.VCBS,
		nfe.CFOP,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func xmlInsertCTeEntrada(db *sql.DB, companyID string, cte *parsedCTe) (bool, error) {
	res, err := db.Exec(`
		INSERT INTO cte_entradas (
			company_id, chave_cte, modelo, serie, numero_cte,
			data_emissao, mes_ano,
			nat_op, cfop, modal,
			emit_cnpj, emit_nome, emit_uf,
			rem_cnpj_cpf, rem_nome, rem_uf,
			dest_cnpj_cpf, dest_nome, dest_uf,
			v_prest,
			v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
			cancelado
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,
			$8,$9,$10,
			$11,$12,$13,
			$14,$15,$16,
			$17,$18,$19,
			$20,
			$21,$22,$23,$24,$25,
			'N'
		)
		ON CONFLICT ON CONSTRAINT uq_cte_entradas_company_chave DO NOTHING`,
		companyID, cte.Chave, cte.Modelo, cte.Serie, cte.Numero,
		cte.DataEmissao, cte.MesAno,
		nullStr(cte.NatOp), nullStr(cte.CFOP), nullStr(cte.Modal),
		cte.EmitCNPJ, nullStr(cte.EmitNome), nullStr(cte.EmitUF),
		nullStr(cte.RemCNPJCPF), nullStr(cte.RemNome), nullStr(cte.RemUF),
		nullStr(cte.DestCNPJCPF), nullStr(cte.DestNome), nullStr(cte.DestUF),
		cte.VPrest,
		cte.VBCIBS, cte.VIBSuf, cte.VIBSMun, cte.VIBS, cte.VCBS,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// xmlDatePart extracts the YYYY-MM-DD portion from a dhEmi string.
// Input: "2025-01-15T10:00:00-03:00" → "2025-01-15"
func xmlDatePart(dhEmi string) string {
	if len(dhEmi) < 10 {
		return ""
	}
	if idx := strings.IndexByte(dhEmi, 'T'); idx >= 0 {
		return dhEmi[:idx]
	}
	return dhEmi[:10]
}

// xmlMesAno extracts MM/YYYY from a dhEmi string.
// Input: "2025-01-15T10:00:00-03:00" → "01/2025"
func xmlMesAno(dhEmi string) string {
	date := xmlDatePart(dhEmi) // "2025-01-15"
	if len(date) < 7 {
		return ""
	}
	// date[0:4] = year, date[5:7] = month
	return date[5:7] + "/" + date[0:4]
}

// xmlFloat parses a decimal string to float64; returns 0 on empty or error.
func xmlFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

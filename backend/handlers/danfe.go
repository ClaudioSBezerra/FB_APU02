package handlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── meudanfe.com.br API ───────────────────────────────────────────────────────

func getDanfePDF(xmlRaw string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// Step 1: init — get PID
	resp, err := client.Get("https://ws.meudanfe.com.br/v2/fd/init")
	if err != nil {
		return nil, fmt.Errorf("meudanfe init: %w", err)
	}
	defer resp.Body.Close()
	var initResp struct {
		PID string `json:"pid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil || initResp.PID == "" {
		return nil, fmt.Errorf("meudanfe init: invalid response")
	}

	// Step 2: send XML (base64 encoded)
	xmlB64 := base64.StdEncoding.EncodeToString([]byte(xmlRaw))
	payload, _ := json.Marshal(map[string]string{"pid": initResp.PID, "xml": xmlB64})
	postResp, err := client.Post(
		"https://ws.meudanfe.com.br/v2/fd/xml",
		"application/json",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		return nil, fmt.Errorf("meudanfe xml upload: %w", err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(postResp.Body)
		return nil, fmt.Errorf("meudanfe xml upload status %d: %s", postResp.StatusCode, string(body))
	}

	// Step 3: get PDF
	pdfResp, err := client.Get("https://ws.meudanfe.com.br/v2/fd/pdf?pid=" + initResp.PID)
	if err != nil {
		return nil, fmt.Errorf("meudanfe pdf: %w", err)
	}
	defer pdfResp.Body.Close()
	if pdfResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(pdfResp.Body)
		return nil, fmt.Errorf("meudanfe pdf status %d: %s", pdfResp.StatusCode, string(body))
	}
	return io.ReadAll(pdfResp.Body)
}

// ── NF-e XML structs for HTML DANFE ──────────────────────────────────────────

type NFeXML struct {
	XMLName xml.Name `xml:"nfeProc"`
	NFe     NFeData  `xml:"NFe"`
	ProtNFe ProtNFe  `xml:"protNFe"`
}

type ProtNFe struct {
	InfProt struct {
		ChNFe    string `xml:"chNFe"`
		DhRecbto string `xml:"dhRecbto"`
		NProt    string `xml:"nProt"`
	} `xml:"infProt"`
}

type NFeData struct {
	InfNFe InfNFe `xml:"infNFe"`
}

type InfNFe struct {
	Ide     Ide     `xml:"ide"`
	Emit    Emit    `xml:"emit"`
	Dest    Dest    `xml:"dest"`
	Dets    []Det   `xml:"det"`
	Total   Total   `xml:"total"`
	Transp  Transp  `xml:"transp"`
	InfAdic InfAdic `xml:"infAdic"`
}

type Ide struct {
	CUF      string `xml:"cUF"`
	NNF      string `xml:"nNF"`
	Serie    string `xml:"serie"`
	Mod      string `xml:"mod"`
	DhEmi    string `xml:"dhEmi"`
	DhSaiEnt string `xml:"dhSaiEnt"`
	TpNF     string `xml:"tpNF"`
	NatOp    string `xml:"natOp"`
	TpEmis   string `xml:"tpEmis"`
}

type Emit struct {
	CNPJ      string    `xml:"CNPJ"`
	XNome     string    `xml:"xNome"`
	XFant     string    `xml:"xFant"`
	EnderEmit EnderEmit `xml:"enderEmit"`
	IE        string    `xml:"IE"`
	CRT       string    `xml:"CRT"`
}

type EnderEmit struct {
	XLgr    string `xml:"xLgr"`
	Nro     string `xml:"nro"`
	XCpl    string `xml:"xCpl"`
	XBairro string `xml:"xBairro"`
	XMun    string `xml:"xMun"`
	UF      string `xml:"UF"`
	CEP     string `xml:"CEP"`
	Fone    string `xml:"fone"`
}

type Dest struct {
	CNPJ      string    `xml:"CNPJ"`
	CPF       string    `xml:"CPF"`
	XNome     string    `xml:"xNome"`
	EnderDest EnderDest `xml:"enderDest"`
	IE        string    `xml:"IE"`
	Email     string    `xml:"email"`
}

type EnderDest struct {
	XLgr    string `xml:"xLgr"`
	Nro     string `xml:"nro"`
	XCpl    string `xml:"xCpl"`
	XBairro string `xml:"xBairro"`
	XMun    string `xml:"xMun"`
	UF      string `xml:"UF"`
	CEP     string `xml:"CEP"`
}

type Det struct {
	NItem   string     `xml:"nItem,attr"`
	Prod    Prod       `xml:"prod"`
	Imposto DetImposto `xml:"imposto"`
}

type Prod struct {
	CProd  string `xml:"cProd"`
	XProd  string `xml:"xProd"`
	NCM    string `xml:"NCM"`
	CFOP   string `xml:"CFOP"`
	UCom   string `xml:"uCom"`
	QCom   string `xml:"qCom"`
	VUnCom string `xml:"vUnCom"`
	VProd  string `xml:"vProd"`
}

type DetImposto struct {
	ICMS struct {
		ICMS00 *struct{ VST string `xml:"vST"` } `xml:"ICMS00"`
		ICMS10 *struct{ VST string `xml:"vST"` } `xml:"ICMS10"`
		ICMS20 *struct{ VST string `xml:"vST"` } `xml:"ICMS20"`
		ICMS60 *struct {
			VBCSTRet string `xml:"vBCSTRet"`
		} `xml:"ICMS60"`
	} `xml:"ICMS"`
}

type Total struct {
	ICMSTot ICMSTot `xml:"ICMSTot"`
}

type ICMSTot struct {
	VBC     string `xml:"vBC"`
	VICMS   string `xml:"vICMS"`
	VST     string `xml:"vST"`
	VProd   string `xml:"vProd"`
	VFrete  string `xml:"vFrete"`
	VSeg    string `xml:"vSeg"`
	VDesc   string `xml:"vDesc"`
	VIPI    string `xml:"vIPI"`
	VPIS    string `xml:"vPIS"`
	VCofins string `xml:"vCOFINS"`
	VOutro  string `xml:"vOutro"`
	VNF     string `xml:"vNF"`
}

type Transp struct {
	ModFrete   string `xml:"modFrete"`
	Transporta struct {
		CNPJ   string `xml:"CNPJ"`
		XNome  string `xml:"xNome"`
		XEnder string `xml:"xEnder"`
		XMun   string `xml:"xMun"`
		UF     string `xml:"UF"`
	} `xml:"transporta"`
}

type InfAdic struct {
	InfCpl string `xml:"infCpl"`
}

// ── HTML template for DANFE ───────────────────────────────────────────────────

const danfeHTMLTemplate = `<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="UTF-8">
<title>DANFE - NF-e {{.Ide.NNF}}</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: Arial, sans-serif; font-size: 8pt; color: #000; background: #fff; }
  .page { width: 210mm; margin: 0 auto; padding: 5mm; }
  table { width: 100%; border-collapse: collapse; }
  td, th { border: 1px solid #000; padding: 2px 3px; vertical-align: top; }
  .no-border td { border: none; }
  .label { font-size: 6pt; color: #555; display: block; }
  .value { font-size: 8pt; font-weight: bold; }
  .header-box { border: 1px solid #000; margin-bottom: 3px; padding: 3px; }
  .title { text-align: center; font-size: 10pt; font-weight: bold; border: 2px solid #000; padding: 4px; margin-bottom: 3px; }
  .chave { font-family: monospace; font-size: 7pt; word-break: break-all; }
  .items th { background: #f0f0f0; font-size: 7pt; text-align: center; }
  .items td { font-size: 7pt; }
  .total-row { font-weight: bold; background: #f9f9f9; }
  .section-title { background: #ddd; font-weight: bold; font-size: 7pt; padding: 2px 3px; }
  .right { text-align: right; }
  .center { text-align: center; }
  @media print {
    body { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
    .no-print { display: none; }
    @page { size: A4; margin: 5mm; }
  }
</style>
</head>
<body>
<div class="page">

  <!-- Botão imprimir (só na tela) -->
  <div class="no-print" style="text-align:right;margin-bottom:8px">
    <button onclick="window.print()" style="padding:6px 16px;background:#c00;color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:10pt">
      Baixar / Imprimir PDF
    </button>
  </div>

  <!-- Cabeçalho -->
  <table style="margin-bottom:3px">
    <tr>
      <td style="width:50%;border:1px solid #000;padding:4px">
        <div style="font-size:11pt;font-weight:bold">{{.Emit.XNome}}</div>
        <div>{{.Emit.XFant}}</div>
        <div>{{.Emit.EnderEmit.XLgr}}, {{.Emit.EnderEmit.Nro}} {{.Emit.EnderEmit.XCpl}}</div>
        <div>{{.Emit.EnderEmit.XBairro}} — {{.Emit.EnderEmit.XMun}}/{{.Emit.EnderEmit.UF}}</div>
        <div>CEP: {{.Emit.EnderEmit.CEP}} | Fone: {{.Emit.EnderEmit.Fone}}</div>
        <div>CNPJ: {{.EmitCNPJ}} | IE: {{.Emit.IE}}</div>
      </td>
      <td style="width:20%;border:1px solid #000;padding:4px;text-align:center">
        <div style="font-size:9pt;font-weight:bold">DANFE</div>
        <div style="font-size:7pt">Documento Auxiliar da<br>Nota Fiscal Eletrônica</div>
        <div style="margin-top:4px;font-size:7pt">0 - Entrada<br>1 - Saída</div>
        <div style="font-size:11pt;font-weight:bold;border:1px solid #000;margin-top:4px;padding:2px">{{.Ide.TpNF}}</div>
      </td>
      <td style="width:30%;border:1px solid #000;padding:4px;text-align:center">
        <div style="font-size:9pt;font-weight:bold">Nº {{.Ide.NNF}}</div>
        <div style="font-size:8pt">Série: {{.Ide.Serie}}</div>
        <div style="font-size:8pt">Emissão: {{.DataEmissao}}</div>
        <div style="font-size:8pt">Protocolo: {{.NProt}}</div>
        <div style="font-size:7pt;margin-top:4px">Folha 1/1</div>
      </td>
    </tr>
  </table>

  <!-- Chave de acesso -->
  <table style="margin-bottom:3px">
    <tr>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">CHAVE DE ACESSO</span>
        <span class="chave">{{.Chave}}</span>
      </td>
    </tr>
  </table>

  <!-- Natureza da operação -->
  <table style="margin-bottom:3px">
    <tr>
      <td style="width:70%;border:1px solid #000;padding:3px">
        <span class="label">NATUREZA DA OPERAÇÃO</span>
        <span class="value">{{.Ide.NatOp}}</span>
      </td>
      <td style="width:30%;border:1px solid #000;padding:3px">
        <span class="label">PROTOCOLO DE AUTORIZAÇÃO</span>
        <span class="value">{{.NProt}}</span>
      </td>
    </tr>
    <tr>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">CNPJ</span>
        <span class="value">{{.EmitCNPJ}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">INSCRIÇÃO ESTADUAL</span>
        <span class="value">{{.Emit.IE}}</span>
      </td>
    </tr>
  </table>

  <!-- Destinatário -->
  <div class="section-title">DESTINATÁRIO / REMETENTE</div>
  <table style="margin-bottom:3px">
    <tr>
      <td style="width:60%;border:1px solid #000;padding:3px">
        <span class="label">NOME / RAZÃO SOCIAL</span>
        <span class="value">{{.Dest.XNome}}</span>
      </td>
      <td style="width:25%;border:1px solid #000;padding:3px">
        <span class="label">CNPJ / CPF</span>
        <span class="value">{{.DestDoc}}</span>
      </td>
      <td style="width:15%;border:1px solid #000;padding:3px">
        <span class="label">DATA EMISSÃO</span>
        <span class="value">{{.DataEmissao}}</span>
      </td>
    </tr>
    <tr>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">ENDEREÇO</span>
        <span class="value">{{.Dest.EnderDest.XLgr}}, {{.Dest.EnderDest.Nro}} {{.Dest.EnderDest.XCpl}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">MUNICÍPIO</span>
        <span class="value">{{.Dest.EnderDest.XMun}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">UF</span>
        <span class="value">{{.Dest.EnderDest.UF}}</span>
      </td>
    </tr>
  </table>

  <!-- Produtos -->
  <div class="section-title">DADOS DOS PRODUTOS / SERVIÇOS</div>
  <table class="items" style="margin-bottom:3px">
    <thead>
      <tr>
        <th style="width:4%">#</th>
        <th style="width:8%">CÓD.</th>
        <th style="width:30%">DESCRIÇÃO</th>
        <th style="width:7%">NCM</th>
        <th style="width:5%">CFOP</th>
        <th style="width:5%">UN</th>
        <th style="width:8%">QTD</th>
        <th style="width:8%">VL. UNIT.</th>
        <th style="width:8%">VL. TOTAL</th>
      </tr>
    </thead>
    <tbody>
      {{range .Dets}}
      <tr>
        <td class="center">{{.NItem}}</td>
        <td>{{.Prod.CProd}}</td>
        <td>{{.Prod.XProd}}</td>
        <td class="center">{{.Prod.NCM}}</td>
        <td class="center">{{.Prod.CFOP}}</td>
        <td class="center">{{.Prod.UCom}}</td>
        <td class="right">{{.Prod.QCom}}</td>
        <td class="right">{{.Prod.VUnCom}}</td>
        <td class="right">{{.Prod.VProd}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <!-- Totais -->
  <div class="section-title">CÁLCULO DO IMPOSTO</div>
  <table style="margin-bottom:3px">
    <tr>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">BC ICMS</span>
        <span class="value">{{.Total.ICMSTot.VBC}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">VL. ICMS</span>
        <span class="value">{{.Total.ICMSTot.VICMS}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">VL. ST</span>
        <span class="value">{{.Total.ICMSTot.VST}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">VL. PRODUTOS</span>
        <span class="value">{{.Total.ICMSTot.VProd}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">VL. FRETE</span>
        <span class="value">{{.Total.ICMSTot.VFrete}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">VL. DESCONTO</span>
        <span class="value">{{.Total.ICMSTot.VDesc}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px;background:#f0f0f0">
        <span class="label">VL. TOTAL NF</span>
        <span class="value" style="font-size:10pt">{{.Total.ICMSTot.VNF}}</span>
      </td>
    </tr>
  </table>

  <!-- Transporte -->
  <div class="section-title">TRANSPORTADOR / VOLUMES TRANSPORTADOS</div>
  <table style="margin-bottom:3px">
    <tr>
      <td style="border:1px solid #000;padding:3px;width:40%">
        <span class="label">RAZÃO SOCIAL</span>
        <span class="value">{{.Transp.Transporta.XNome}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px;width:15%">
        <span class="label">FRETE POR CONTA</span>
        <span class="value">{{.ModFrete}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px;width:20%">
        <span class="label">CNPJ</span>
        <span class="value">{{.Transp.Transporta.CNPJ}}</span>
      </td>
      <td style="border:1px solid #000;padding:3px">
        <span class="label">UF</span>
        <span class="value">{{.Transp.Transporta.UF}}</span>
      </td>
    </tr>
  </table>

  <!-- Info adicional -->
  {{if .InfAdic.InfCpl}}
  <div class="section-title">INFORMAÇÕES COMPLEMENTARES</div>
  <table style="margin-bottom:3px">
    <tr>
      <td style="border:1px solid #000;padding:3px;font-size:7pt">{{.InfAdic.InfCpl}}</td>
    </tr>
  </table>
  {{end}}

  <div style="text-align:center;font-size:7pt;margin-top:4px;color:#666">
    Gerado por FB_APU02 — Apuração Assistida Ferreira Costa
  </div>
</div>
</body>
</html>`

// ── View model ────────────────────────────────────────────────────────────────

type DanfeViewModel struct {
	InfNFe
	Chave       string
	NProt       string
	DataEmissao string
	EmitCNPJ    string
	DestDoc     string
	ModFrete    string
}

func parseDanfeViewModel(xmlRaw string) (*DanfeViewModel, error) {
	// Remove namespace prefixes for simpler parsing
	clean := strings.NewReplacer(
		"nfeProc:", "", ":nfeProc", "",
		"NFe:", "", ":NFe", "",
		"ns0:", "", ":ns0", "",
	).Replace(xmlRaw)

	var nfe NFeXML
	if err := xml.Unmarshal([]byte(clean), &nfe); err != nil {
		// Try without nfeProc wrapper
		type NFeDirect struct {
			XMLName xml.Name `xml:"NFe"`
			InfNFe  InfNFe   `xml:"infNFe"`
			Sig     struct{} `xml:"Signature"`
		}
		var direct NFeDirect
		if err2 := xml.Unmarshal([]byte(clean), &direct); err2 != nil {
			return nil, fmt.Errorf("parse XML: %w / %w", err, err2)
		}
		nfe.NFe.InfNFe = direct.InfNFe
	}

	inf := nfe.NFe.InfNFe
	chave := nfe.ProtNFe.InfProt.ChNFe

	// Format date
	dataEmissao := inf.Ide.DhEmi
	if len(dataEmissao) >= 10 {
		p := dataEmissao[:10] // YYYY-MM-DD
		if len(p) == 10 {
			dataEmissao = p[8:10] + "/" + p[5:7] + "/" + p[:4]
		}
	}

	// Format CNPJ emitente
	emitCNPJ := inf.Emit.CNPJ
	if len(emitCNPJ) == 14 {
		emitCNPJ = emitCNPJ[:2] + "." + emitCNPJ[2:5] + "." + emitCNPJ[5:8] + "/" + emitCNPJ[8:12] + "-" + emitCNPJ[12:]
	}

	// Format CNPJ/CPF destinatário
	destDoc := inf.Dest.CNPJ
	if destDoc == "" {
		destDoc = inf.Dest.CPF
	}
	if len(destDoc) == 14 {
		destDoc = destDoc[:2] + "." + destDoc[2:5] + "." + destDoc[5:8] + "/" + destDoc[8:12] + "-" + destDoc[12:]
	} else if len(destDoc) == 11 {
		destDoc = destDoc[:3] + "." + destDoc[3:6] + "." + destDoc[6:9] + "-" + destDoc[9:]
	}

	modFrete := map[string]string{
		"0": "0 - Emitente", "1": "1 - Destinatário", "2": "2 - Terceiros", "9": "9 - Sem frete",
	}[inf.Transp.ModFrete]
	if modFrete == "" {
		modFrete = inf.Transp.ModFrete
	}

	return &DanfeViewModel{
		InfNFe:      inf,
		Chave:       chave,
		NProt:       nfe.ProtNFe.InfProt.NProt,
		DataEmissao: dataEmissao,
		EmitCNPJ:    emitCNPJ,
		DestDoc:     destDoc,
		ModFrete:    modFrete,
	}, nil
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

// DanfeHandler serves DANFE for a given chave de acesso.
// GET /api/danfe/{chave}       — tries meudanfe PDF, falls back to HTML
// GET /api/danfe/{chave}/html  — always returns printable HTML
func DanfeHandler(db *sql.DB) http.HandlerFunc {
	tmpl := template.Must(template.New("danfe").Parse(danfeHTMLTemplate))

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			http.Error(w, "company error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Extract chave from path: /api/danfe/{chave} or /api/danfe/{chave}/html
		path := strings.TrimPrefix(r.URL.Path, "/api/danfe/")
		wantHTML := strings.HasSuffix(path, "/html")
		chave := strings.TrimSuffix(path, "/html")
		chave = strings.TrimSpace(chave)

		if len(chave) != 44 {
			http.Error(w, "Chave inválida — deve ter 44 dígitos", http.StatusBadRequest)
			return
		}

		// Look up XML in dfe_xml table
		var xmlRaw string
		err = db.QueryRow(`SELECT xml_raw FROM dfe_xml WHERE company_id = $1 AND chave = $2`, companyID, chave).Scan(&xmlRaw)
		if err == sql.ErrNoRows {
			http.Error(w, "XML não encontrado. Importe o XML desta NF-e/CT-e primeiro.", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Erro ao buscar XML: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Option 2: meudanfe.com.br PDF (unless /html requested)
		if !wantHTML {
			pdfBytes, pdfErr := getDanfePDF(xmlRaw)
			if pdfErr == nil && len(pdfBytes) > 5 {
				// Check it's actually a PDF
				if strings.HasPrefix(string(pdfBytes[:5]), "%PDF") {
					w.Header().Set("Content-Type", "application/pdf")
					w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="DANFE_%s.pdf"`, chave))
					w.Write(pdfBytes)
					return
				}
			}
			log.Printf("[DANFE] meudanfe API failed for %s: %v — falling back to HTML", chave, pdfErr)
		}

		// Option 3: self-hosted HTML DANFE
		vm, err := parseDanfeViewModel(xmlRaw)
		if err != nil {
			http.Error(w, "Erro ao processar XML: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if vm.Chave == "" {
			vm.Chave = chave
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, vm); err != nil {
			log.Printf("[DANFE] template error: %v", err)
		}
	}
}

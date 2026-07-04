package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"fb_apu02/services"
)

// ---------------------------------------------------------------------------
// ERPBridgeBatchImportHandler — POST /api/erp-bridge/import/batch
//
// Autenticado via X-API-Key (igual /api/erp-bridge/credentials).
// Recebe documentos já agregados do SAP S4/HANA (s4i_nfe + s4i_nfe_impostos)
// e os roteia para nfe_saidas, nfe_entradas ou cte_entradas conforme:
//   - DIRECT=2          → nfe_saidas
//   - DIRECT=1 + modelo IN (55,62,65) → nfe_entradas
//   - DIRECT=1 + modelo IN (57,66,67) → cte_entradas
// ---------------------------------------------------------------------------

// batchDoc representa um documento fiscal já agregado vindo do bridge Python.
type batchDoc struct {
	Direct           string  `json:"direct"`            // "1" = entrada, "2" = saída
	Chave            string  `json:"chave"`             // 44 dígitos
	Modelo           string  `json:"modelo"`            // "55","57","65",...
	Serie            string  `json:"serie"`
	Numero           string  `json:"numero"`
	DataEmissao      string  `json:"data_emissao"`      // "YYYY-MM-DD"
	DataAutorizacao  string  `json:"data_autorizacao"`  // "YYYY-MM-DD"
	MesAno           string  `json:"mes_ano"`           // "MM/YYYY"
	EmitCNPJ         string  `json:"emit_cnpj"`
	DestCNPJ         string  `json:"dest_cnpj"`
	Cancelado        string  `json:"cancelado"`         // "S" = cancelada, demais = normal
	NomeParceiro     string  `json:"nome_parceiro"`     // forn.razsoc (DIRECT=1) ou clie.razsoc (DIRECT=2)
	CFOP             string  `json:"cfop"`              // código CFOP 4 dígitos (ex: "1102")
	TipoCFOP         string  `json:"tipo_cfop"`         // C=Consumo,R=Revenda,A=Ativo Imobilizado,T=Transferência,O=Outros,S=Serviços
	VTotal           float64 `json:"v_total"`
	VBcIbsCbs        float64 `json:"v_bc_ibs_cbs"`
	VIbsUf           float64 `json:"v_ibs_uf"`
	VIbsMun          float64 `json:"v_ibs_mun"`
	VIbs             float64 `json:"v_ibs"`
	VCbs             float64 `json:"v_cbs"`
	BaseIcms         float64 `json:"base_icms"`
	Icms             float64 `json:"icms"`
	IcmsSt           float64 `json:"icms_st"`
	Ipi              float64 `json:"ipi"`
	BasePis          float64 `json:"base_pis"`
	Pis              float64 `json:"pis"`
	BaseCofins       float64 `json:"base_cofins"`
	Cofins           float64 `json:"cofins"`
	BasePartilha     float64 `json:"base_partilha"`
	IcmsPartilha     float64 `json:"icms_partilha"`
}

type batchRequest struct {
	Documents []batchDoc `json:"documents"`
}

type batchResult struct {
	Inserted     int      `json:"inserted"`
	Ignored      int      `json:"ignored"`
	Errors       int      `json:"errors"`
	ErrorDetails []string `json:"error_details"`
}

// modelosSaida: DIRECT=2 → sempre nfe_saidas
// modelosNFeEntrada: DIRECT=1 → nfe_entradas
// modelosCTeEntrada: DIRECT=1 → cte_entradas
var modelosNFeEntrada = map[string]bool{"55": true, "62": true, "65": true}
var modelosCTeEntrada = map[string]bool{"57": true, "66": true, "67": true}

func ERPBridgeBatchImportHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		// ── Auth via X-API-Key ────────────────────────────────────────────────
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			http.Error(w, `{"error":"X-API-Key obrigatório"}`, http.StatusUnauthorized)
			return
		}
		hash := sha256.Sum256([]byte(apiKey))
		hashHex := hex.EncodeToString(hash[:])

		var companyID string
		err := db.QueryRow(
			`SELECT company_id FROM erp_bridge_config WHERE api_key_hash = $1`, hashHex,
		).Scan(&companyID)
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"API key inválida"}`, http.StatusUnauthorized)
			return
		}
		if err != nil {
			log.Printf("[BatchImport] db error auth: %v", err)
			http.Error(w, `{"error":"erro interno"}`, http.StatusInternalServerError)
			return
		}

		// ── Parse body ────────────────────────────────────────────────────────
		var req batchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[BatchImport] JSON inválido: %v", err)
			http.Error(w, `{"error":"JSON inválido"}`, http.StatusBadRequest)
			return
		}

		if len(req.Documents) == 0 {
			json.NewEncoder(w).Encode(batchResult{ErrorDetails: []string{}})
			return
		}

		result := batchResult{ErrorDetails: []string{}}

		// Transação única para o lote inteiro: um fsync no commit em vez de
		// 2 autocommits por documento. SAVEPOINT por documento preserva a
		// semântica de erro individual (doc inválido conta erro, lote segue).
		tx, err := db.Begin()
		if err != nil {
			log.Printf("[BatchImport] begin tx: %v", err)
			http.Error(w, `{"error":"erro interno"}`, http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		stmts, err := prepareBatchStmts(tx)
		if err != nil {
			log.Printf("[BatchImport] prepare stmts: %v", err)
			http.Error(w, `{"error":"erro interno"}`, http.StatusInternalServerError)
			return
		}

		for i, doc := range req.Documents {
			if len(doc.Chave) != 44 {
				result.Errors++
				result.ErrorDetails = append(result.ErrorDetails,
					"doc["+strconv.Itoa(i)+"]: chave inválida ("+doc.Chave+")")
				continue
			}

			modelo := strings.TrimSpace(doc.Modelo)
			direct := strings.TrimSpace(doc.Direct)

			var inserted bool
			var insertErr error

			tx.Exec("SAVEPOINT doc_sp")

			switch {
			case direct == "2":
				// Saída → nfe_saidas (emit_cnpj = filial emitente)
				inserted, insertErr = batchInsertNFeSaida(stmts.saida, companyID, doc, modelo)

			case direct == "1" && modelosNFeEntrada[modelo]:
				// Entrada NF-e → nfe_entradas (forn_cnpj = emitente, dest = filial)
				inserted, insertErr = batchInsertNFeEntrada(stmts.entrada, companyID, doc, modelo)

			case direct == "1" && modelosCTeEntrada[modelo]:
				// Entrada CT-e → cte_entradas (emit_cnpj = transportadora, dest = filial)
				inserted, insertErr = batchInsertCTeEntrada(stmts.cte, companyID, doc, modelo)

			default:
				tx.Exec("RELEASE SAVEPOINT doc_sp")
				result.Errors++
				result.ErrorDetails = append(result.ErrorDetails,
					"doc["+strconv.Itoa(i)+"]: combinação DIRECT="+direct+"/modelo="+modelo+" desconhecida")
				continue
			}

			if insertErr != nil {
				// Desfaz só este documento; a transação segue válida para os próximos
				tx.Exec("ROLLBACK TO SAVEPOINT doc_sp")
				tx.Exec("RELEASE SAVEPOINT doc_sp")
				log.Printf("[BatchImport] INSERT error [%s]: %v", doc.Chave, insertErr)
				result.Errors++
				result.ErrorDetails = append(result.ErrorDetails,
					"doc["+strconv.Itoa(i)+"] "+doc.Chave+": "+insertErr.Error())
			} else {
				tx.Exec("RELEASE SAVEPOINT doc_sp")
				if inserted {
					result.Inserted++
				} else {
					result.Ignored++
				}
				// Grava parceiro na tabela de lookup independente de inserção nova.
				// Savepoint próprio: falha no parceiro não pode abortar a transação
				// nem desfazer o insert do documento (erro era só logado antes).
				if doc.NomeParceiro != "" {
					cnpjParceiro := doc.EmitCNPJ
					if direct != "1" {
						cnpjParceiro = doc.DestCNPJ
					}
					tx.Exec("SAVEPOINT parc_sp")
					if perr := upsertParceiro(stmts.parceiro, companyID, cnpjParceiro, doc.NomeParceiro); perr != nil {
						tx.Exec("ROLLBACK TO SAVEPOINT parc_sp")
					}
					tx.Exec("RELEASE SAVEPOINT parc_sp")
				}
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[BatchImport] commit: %v", err)
			http.Error(w, `{"error":"falha ao gravar lote"}`, http.StatusInternalServerError)
			return
		}

		log.Printf("[BatchImport] company=%s inserted=%d ignored=%d errors=%d",
			companyID, result.Inserted, result.Ignored, result.Errors)
		services.RequestMVRefresh(db, "mv_malha_fina_resumo")
		json.NewEncoder(w).Encode(result)
	}
}

// batchStmts agrupa os prepared statements do lote — preparados uma única vez
// por transação, eliminando parse/plan por documento.
type batchStmts struct {
	saida, entrada, cte, parceiro *sql.Stmt
}

func prepareBatchStmts(tx *sql.Tx) (*batchStmts, error) {
	var s batchStmts
	var err error
	if s.saida, err = tx.Prepare(sqlBatchNFeSaida); err != nil {
		return nil, err
	}
	if s.entrada, err = tx.Prepare(sqlBatchNFeEntrada); err != nil {
		return nil, err
	}
	if s.cte, err = tx.Prepare(sqlBatchCTeEntrada); err != nil {
		return nil, err
	}
	if s.parceiro, err = tx.Prepare(sqlBatchParceiro); err != nil {
		return nil, err
	}
	return &s, nil
}

const sqlBatchNFeSaida = `
		INSERT INTO nfe_saidas (
			company_id, chave_nfe, modelo, serie, numero_nfe,
			data_emissao, data_autorizacao, mes_ano,
			emit_cnpj, dest_cnpj_cpf,
			v_nf,
			v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
			base_icms, icms, icms_st, ipi,
			base_pis, pis, base_cofins, cofins,
			base_partilha, icms_partilha,
			cancelado, tipo_cfop, cfop
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,
			$9,$10,
			$11,
			$12,$13,$14,$15,$16,
			$17,$18,$19,$20,
			$21,$22,$23,$24,
			$25,$26,
			$27,
			COALESCE(NULLIF($28,''), (SELECT c.tipo FROM cfop c WHERE c.cfop = NULLIF($29,'')), 'O'),
			NULLIF($29,'')
		)
		ON CONFLICT ON CONSTRAINT uq_nfe_saidas_company_chave
		DO UPDATE SET
			cancelado      = EXCLUDED.cancelado,
			base_icms      = EXCLUDED.base_icms,
			icms           = EXCLUDED.icms,
			icms_st        = EXCLUDED.icms_st,
			ipi            = EXCLUDED.ipi,
			base_pis       = EXCLUDED.base_pis,
			pis            = EXCLUDED.pis,
			base_cofins    = EXCLUDED.base_cofins,
			cofins         = EXCLUDED.cofins,
			base_partilha  = EXCLUDED.base_partilha,
			icms_partilha  = EXCLUDED.icms_partilha,
			tipo_cfop = COALESCE(
				NULLIF($28,''),
				(SELECT c.tipo FROM cfop c WHERE c.cfop = NULLIF($29,'')),
				nfe_saidas.tipo_cfop,
				'O'
			),
			cfop = COALESCE(NULLIF($29,''), nfe_saidas.cfop)`

func batchInsertNFeSaida(stmt *sql.Stmt, companyID string, doc batchDoc, modelo string) (bool, error) {
	modInt, _ := strconv.Atoi(modelo)
	cancelado := doc.Cancelado
	if cancelado != "S" { cancelado = "N" }
	tipoCFOP := strings.TrimSpace(doc.TipoCFOP)
	cfopCode := strings.TrimSpace(doc.CFOP)
	res, err := stmt.Exec(
		companyID, doc.Chave, modInt, doc.Serie, doc.Numero,
		nullDate(doc.DataEmissao), nullDate(doc.DataAutorizacao), doc.MesAno,
		doc.EmitCNPJ, doc.DestCNPJ,
		doc.VTotal,
		doc.VBcIbsCbs, doc.VIbsUf, doc.VIbsMun, doc.VIbs, doc.VCbs,
		doc.BaseIcms, doc.Icms, doc.IcmsSt, doc.Ipi,
		doc.BasePis, doc.Pis, doc.BaseCofins, doc.Cofins,
		doc.BasePartilha, doc.IcmsPartilha,
		cancelado, tipoCFOP, cfopCode,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

const sqlBatchNFeEntrada = `
		INSERT INTO nfe_entradas (
			company_id, chave_nfe, modelo, serie, numero_nfe,
			data_emissao, data_autorizacao, mes_ano,
			forn_cnpj, dest_cnpj_cpf,
			v_nf,
			v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
			base_icms, icms, icms_st, ipi,
			base_pis, pis, base_cofins, cofins,
			base_partilha, icms_partilha,
			cancelado, tipo_cfop, cfop
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,
			$9,$10,
			$11,
			$12,$13,$14,$15,$16,
			$17,$18,$19,$20,
			$21,$22,$23,$24,
			$25,$26,
			$27,
			COALESCE(NULLIF($28,''), (SELECT c.tipo FROM cfop c WHERE c.cfop = NULLIF($29,'')), 'C'),
			NULLIF($29,'')
		)
		ON CONFLICT ON CONSTRAINT uq_nfe_entradas_company_chave
		DO UPDATE SET
			cancelado      = EXCLUDED.cancelado,
			base_icms      = EXCLUDED.base_icms,
			icms           = EXCLUDED.icms,
			icms_st        = EXCLUDED.icms_st,
			ipi            = EXCLUDED.ipi,
			base_pis       = EXCLUDED.base_pis,
			pis            = EXCLUDED.pis,
			base_cofins    = EXCLUDED.base_cofins,
			cofins         = EXCLUDED.cofins,
			base_partilha  = EXCLUDED.base_partilha,
			icms_partilha  = EXCLUDED.icms_partilha,
			tipo_cfop = COALESCE(
				NULLIF($28,''),
				(SELECT c.tipo FROM cfop c WHERE c.cfop = NULLIF($29,'')),
				nfe_entradas.tipo_cfop,
				'C'
			),
			cfop = COALESCE(NULLIF($29,''), nfe_entradas.cfop)`

func batchInsertNFeEntrada(stmt *sql.Stmt, companyID string, doc batchDoc, modelo string) (bool, error) {
	modInt, _ := strconv.Atoi(modelo)
	cancelado := doc.Cancelado
	if cancelado != "S" { cancelado = "N" }
	// tipo_cfop: usa valor explícito do payload; se vazio, faz lookup na tabela cfop via SQL
	tipoCFOP := strings.TrimSpace(doc.TipoCFOP)
	cfopCode := strings.TrimSpace(doc.CFOP)
	res, err := stmt.Exec(
		companyID, doc.Chave, modInt, doc.Serie, doc.Numero,
		nullDate(doc.DataEmissao), nullDate(doc.DataAutorizacao), doc.MesAno,
		doc.EmitCNPJ, doc.DestCNPJ,
		doc.VTotal,
		doc.VBcIbsCbs, doc.VIbsUf, doc.VIbsMun, doc.VIbs, doc.VCbs,
		doc.BaseIcms, doc.Icms, doc.IcmsSt, doc.Ipi,
		doc.BasePis, doc.Pis, doc.BaseCofins, doc.Cofins,
		doc.BasePartilha, doc.IcmsPartilha,
		cancelado, tipoCFOP, cfopCode,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

const sqlBatchCTeEntrada = `
		INSERT INTO cte_entradas (
			company_id, chave_cte, modelo, serie, numero_cte,
			data_emissao, data_autorizacao, mes_ano,
			emit_cnpj, dest_cnpj_cpf,
			v_prest,
			v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
			cancelado
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,
			$9,$10,
			$11,
			$12,$13,$14,$15,$16,
			$17
		)
		ON CONFLICT ON CONSTRAINT uq_cte_entradas_company_chave
		DO UPDATE SET cancelado = EXCLUDED.cancelado`

func batchInsertCTeEntrada(stmt *sql.Stmt, companyID string, doc batchDoc, modelo string) (bool, error) {
	modInt, _ := strconv.Atoi(modelo)
	cancelado := doc.Cancelado
	if cancelado != "S" { cancelado = "N" }
	res, err := stmt.Exec(
		companyID, doc.Chave, modInt, doc.Serie, doc.Numero,
		nullDate(doc.DataEmissao), nullDate(doc.DataAutorizacao), doc.MesAno,
		doc.EmitCNPJ, doc.DestCNPJ,
		doc.VTotal,
		doc.VBcIbsCbs, doc.VIbsUf, doc.VIbsMun, doc.VIbs, doc.VCbs,
		cancelado,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

const sqlBatchParceiro = `
	INSERT INTO parceiros (company_id, cnpj, nome) VALUES ($1, $2, $3)
	ON CONFLICT (company_id, cnpj)
	DO UPDATE SET nome = EXCLUDED.nome
	WHERE parceiros.nome = '' OR parceiros.nome IS NULL`

// upsertParceiro grava/atualiza CNPJ→nome na tabela parceiros (lookup cross-document).
// Retorna o erro para o chamador decidir o rollback do savepoint.
func upsertParceiro(stmt *sql.Stmt, companyID, cnpj, nome string) error {
	if strings.TrimSpace(cnpj) == "" || strings.TrimSpace(nome) == "" {
		return nil
	}
	if _, err := stmt.Exec(companyID, strings.TrimSpace(cnpj), strings.TrimSpace(nome)); err != nil {
		log.Printf("[ERPBridgeBatch] Erro ao upsert parceiro (cnpj=%s): %v", strings.TrimSpace(cnpj), err)
		return err
	}
	return nil
}

// upsertParceiroDB é a variante sem transação (fluxos de upload XML, um doc por vez).
func upsertParceiroDB(db *sql.DB, companyID, cnpj, nome string) {
	if strings.TrimSpace(cnpj) == "" || strings.TrimSpace(nome) == "" {
		return
	}
	if _, err := db.Exec(sqlBatchParceiro, companyID, strings.TrimSpace(cnpj), strings.TrimSpace(nome)); err != nil {
		log.Printf("[ERPBridgeBatch] Erro ao upsert parceiro (cnpj=%s): %v", strings.TrimSpace(cnpj), err)
	}
}

// nullDate converte "YYYY-MM-DD" para sql.NullString; retorna NULL se vazio.
func nullDate(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

// nullStr retorna nil para string vazia (armazena NULL no banco), caso contrário a própria string.
func nullStr(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

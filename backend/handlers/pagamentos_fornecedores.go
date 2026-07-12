package handlers

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ─── Regex helpers ────────────────────────────────────────────────────────────

var (
	reChaveNFECTE  = regexp.MustCompile(`^\d{44}$`)
	reUUID         = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)
	reNonDigit     = regexp.MustCompile(`[^0-9]`)
)

// ─── CSV import row error ─────────────────────────────────────────────────────

type pagImportError struct {
	Linha int    `json:"linha"`
	Campo string `json:"campo"`
	Erro  string `json:"erro"`
}

// ─── PagamentosFornecedoresImportHandler ─────────────────────────────────────
// POST /api/pagamentos-fornecedores/import
// Aceita multipart com field "csv". Valida, importa e retorna resumo.

func PagamentosFornecedoresImportHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes())
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			jsonErr(w, http.StatusBadRequest, "Erro ao processar formulário: "+err.Error())
			return
		}
		f, header, err := r.FormFile("csv")
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "Campo 'csv' não encontrado no formulário")
			return
		}
		defer f.Close()
		filename := header.Filename

		reader := csv.NewReader(f)
		reader.LazyQuotes = true
		reader.TrimLeadingSpace = true

		// Ler header
		headerRow, err := reader.Read()
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "CSV vazio ou inválido")
			return
		}
		// Mapear índices das colunas (case-insensitive, strip BOM)
		colIdx := map[string]int{}
		for i, h := range headerRow {
			clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\xef\xbb\xbf")))
			colIdx[clean] = i
		}
		required := []string{"chave_doc", "tipo_doc", "forn_cnpj", "data_pagamento", "valor_pagamento"}
		for _, req := range required {
			if _, ok := colIdx[req]; !ok {
				jsonErr(w, http.StatusBadRequest, fmt.Sprintf("Coluna obrigatória ausente no CSV: %s", req))
				return
			}
		}

		type csvRow struct {
			chaveDoc        string
			tipoDoc         string
			fornCNPJ        string
			fornNome        string
			dataPagamento   time.Time
			valorPagamento  float64
			numDocPagamento string
			descricao       string
			mesAno          string
		}

		var rows []csvRow
		var erros []pagImportError
		lineNum := 1 // header já foi linha 1

		records, _ := reader.ReadAll()
		for _, record := range records {
			lineNum++
			getCol := func(name string) string {
				idx, ok := colIdx[name]
				if !ok || idx >= len(record) {
					return ""
				}
				return strings.TrimSpace(record[idx])
			}

			var rowErr bool
			addErr := func(campo, msg string) {
				erros = append(erros, pagImportError{Linha: lineNum, Campo: campo, Erro: msg})
				rowErr = true
			}

			chaveDoc := getCol("chave_doc")
			if chaveDoc == "" {
				addErr("chave_doc", "chave_doc não pode ser vazio")
			}

			tipoDoc := strings.ToUpper(getCol("tipo_doc"))
			validTipos := map[string]bool{"NFE": true, "CTE": true, "NFSE": true}
			if !validTipos[tipoDoc] {
				addErr("tipo_doc", fmt.Sprintf("tipo_doc inválido '%s' (aceito: NFE, CTE, NFSE)", getCol("tipo_doc")))
			}

			// Validar chave: NFE/CTE → 44 dígitos; NFSE → qualquer não-vazia
			if !rowErr && chaveDoc != "" {
				if (tipoDoc == "NFE" || tipoDoc == "CTE") && !reChaveNFECTE.MatchString(chaveDoc) {
					addErr("chave_doc", "chave_doc deve ter exatamente 44 dígitos numéricos para NFE/CTE")
				}
			}

			fornCNPJRaw := getCol("forn_cnpj")
			fornCNPJ := reNonDigit.ReplaceAllString(fornCNPJRaw, "")
			if len(fornCNPJ) != 14 {
				addErr("forn_cnpj", fmt.Sprintf("forn_cnpj deve ter 14 dígitos numéricos (recebido: '%s')", fornCNPJRaw))
			}

			// Parsear data_pagamento: aceita DD/MM/YYYY ou YYYY-MM-DD
			dataStr := getCol("data_pagamento")
			var dataPag time.Time
			var parseErr error
			for _, layout := range []string{"02/01/2006", "2006-01-02"} {
				dataPag, parseErr = time.Parse(layout, dataStr)
				if parseErr == nil {
					break
				}
			}
			if parseErr != nil {
				addErr("data_pagamento", fmt.Sprintf("data_pagamento inválida '%s' (aceito: DD/MM/YYYY ou YYYY-MM-DD)", dataStr))
			}

			valorStr := strings.ReplaceAll(getCol("valor_pagamento"), ",", ".")
			valorPag, parseErrV := strconv.ParseFloat(valorStr, 64)
			if parseErrV != nil || valorPag <= 0 {
				addErr("valor_pagamento", fmt.Sprintf("valor_pagamento inválido '%s' (deve ser número > 0)", getCol("valor_pagamento")))
			}

			if rowErr {
				continue
			}

			mesAno := dataPag.Format("2006-01")
			rows = append(rows, csvRow{
				chaveDoc:        chaveDoc,
				tipoDoc:         tipoDoc,
				fornCNPJ:        fornCNPJ,
				fornNome:        getCol("forn_nome"),
				dataPagamento:   dataPag,
				valorPagamento:  valorPag,
				numDocPagamento: getCol("num_doc_pagamento"),
				descricao:       getCol("descricao"),
				mesAno:          mesAno,
			})
		}

		totalLinhas := lineNum - 1 // sem contar o header
		erroCount := len(erros)

		// Determinar mes_ano do batch (primeiro registro válido, ou vazio)
		batchMesAno := ""
		if len(rows) > 0 {
			batchMesAno = rows[0].mesAno
		}

		// Serializar erros para JSON
		erroDetalhe := ""
		if len(erros) > 0 {
			b, _ := json.Marshal(erros)
			erroDetalhe = string(b)
		}

		// Abrir transação
		tx, err := db.Begin()
		if err != nil {
			log.Printf("[PagamentosImport] begin tx: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao iniciar transação")
			return
		}
		defer tx.Rollback()

		// INSERT registro de batch (antecipado para obter import_id)
		var importID string
		err = tx.QueryRow(`
			INSERT INTO pagamentos_imports
				(company_id, mes_ano, filename, total_linhas, importados, duplicados, erros, importado_por)
			VALUES ($1, $2, $3, $4, 0, 0, $5, $6::uuid)
			RETURNING id::text`,
			companyID, batchMesAno, filename, totalLinhas, erroCount, userID,
		).Scan(&importID)
		if err != nil {
			log.Printf("[PagamentosImport] insert import record: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao registrar batch de importação")
			return
		}

		// INSERT pagamentos com ON CONFLICT DO NOTHING
		importados := 0
		duplicados := 0
		for _, row := range rows {
			var fornNome interface{} = nil
			if row.fornNome != "" {
				fornNome = row.fornNome
			}
			var numDoc interface{} = nil
			if row.numDocPagamento != "" {
				numDoc = row.numDocPagamento
			}
			var desc interface{} = nil
			if row.descricao != "" {
				desc = row.descricao
			}

			// origem não é enviado aqui — a coluna assume o DEFAULT 'csv' (migration
			// 118). O WHERE abaixo casa com esse default e isola a dedup do CSV da
			// dedup de pagamentos de origem SAP (uq_pag_forn_sap_api), que usa uma
			// chave diferente (num_doc_pagamento + bukrs) e não pode colidir com o CSV.
			result, execErr := tx.Exec(`
				INSERT INTO pagamentos_fornecedores
					(company_id, chave_doc, tipo_doc, forn_cnpj, forn_nome,
					 data_pagamento, valor_pagamento, num_doc_pagamento, descricao,
					 mes_ano, import_id, importado_por)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::uuid, $12::uuid)
				ON CONFLICT (company_id, chave_doc, data_pagamento, valor_pagamento) WHERE origem = 'csv' DO NOTHING`,
				companyID, row.chaveDoc, row.tipoDoc, row.fornCNPJ, fornNome,
				row.dataPagamento, row.valorPagamento, numDoc, desc,
				row.mesAno, importID, userID,
			)
			if execErr != nil {
				log.Printf("[PagamentosImport] insert row: %v", execErr)
				erros = append(erros, pagImportError{Campo: "geral", Erro: "Erro ao inserir linha: " + execErr.Error()})
				erroCount++
				continue
			}
			affected, _ := result.RowsAffected()
			if affected > 0 {
				importados++
			} else {
				duplicados++
			}
		}

		// Atualizar contadores no registro de batch
		erroDetalhe = ""
		if len(erros) > 0 {
			b, _ := json.Marshal(erros)
			erroDetalhe = string(b)
		}
		_, err = tx.Exec(`
			UPDATE pagamentos_imports
			SET importados = $1, duplicados = $2, erros = $3, erro_detalhe = $4
			WHERE id = $5::uuid`,
			importados, duplicados, len(erros), erroDetalhe, importID,
		)
		if err != nil {
			log.Printf("[PagamentosImport] update import record: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao atualizar registro de importação")
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[PagamentosImport] commit: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao confirmar transação")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"import_id":    importID,
			"total_linhas": totalLinhas,
			"importados":   importados,
			"duplicados":   duplicados,
			"erros":        erros,
		})
	}
}

// ─── PagamentosFornecedoresListHandler ───────────────────────────────────────
// GET /api/pagamentos-fornecedores
// Query params: mes_ano, forn_cnpj, chave_doc, page, page_size

func PagamentosFornecedoresListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}

		q := r.URL.Query()
		mesAno := strings.TrimSpace(q.Get("mes_ano"))
		fornCNPJ := reNonDigit.ReplaceAllString(q.Get("forn_cnpj"), "")
		chaveDoc := strings.TrimSpace(q.Get("chave_doc"))

		page, _ := strconv.Atoi(q.Get("page"))
		if page < 1 {
			page = 1
		}
		pageSize, _ := strconv.Atoi(q.Get("page_size"))
		if pageSize < 1 {
			pageSize = 50
		}
		if pageSize > 200 {
			pageSize = 200
		}

		// Construir query dinamicamente
		args := []interface{}{companyID}
		where := []string{"company_id = $1"}
		idx := 2

		if mesAno != "" {
			where = append(where, fmt.Sprintf("mes_ano = $%d", idx))
			args = append(args, mesAno)
			idx++
		}
		if len(fornCNPJ) == 14 {
			where = append(where, fmt.Sprintf("forn_cnpj = $%d", idx))
			args = append(args, fornCNPJ)
			idx++
		} else if fornCNPJ != "" {
			where = append(where, fmt.Sprintf("forn_cnpj ILIKE $%d", idx))
			args = append(args, "%"+fornCNPJ+"%")
			idx++
		}
		if chaveDoc != "" {
			where = append(where, fmt.Sprintf("chave_doc ILIKE $%d", idx))
			args = append(args, "%"+chaveDoc+"%")
			idx++
		}

		whereClause := "WHERE " + strings.Join(where, " AND ")

		// COUNT
		var total int
		countQuery := "SELECT COUNT(*) FROM pagamentos_fornecedores " + whereClause
		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			log.Printf("[PagamentosListHandler] count: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao contar registros")
			return
		}

		// LIST
		offset := (page - 1) * pageSize
		listArgs := append(args, pageSize, offset)
		rows, err := db.Query(fmt.Sprintf(`
			SELECT id, chave_doc, tipo_doc, forn_cnpj, COALESCE(forn_nome,''), data_pagamento,
			       valor_pagamento, COALESCE(num_doc_pagamento,''), COALESCE(descricao,''),
			       mes_ano, import_id, importado_em
			FROM pagamentos_fornecedores %s
			ORDER BY data_pagamento DESC, id DESC
			LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1),
			listArgs...,
		)
		if err != nil {
			log.Printf("[PagamentosListHandler] query: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao listar pagamentos")
			return
		}
		defer rows.Close()

		type pagItem struct {
			ID               int64   `json:"id"`
			ChaveDoc         string  `json:"chave_doc"`
			TipoDoc          string  `json:"tipo_doc"`
			FornCNPJ         string  `json:"forn_cnpj"`
			FornNome         string  `json:"forn_nome"`
			DataPagamento    string  `json:"data_pagamento"`
			ValorPagamento   float64 `json:"valor_pagamento"`
			NumDocPagamento  string  `json:"num_doc_pagamento"`
			Descricao        string  `json:"descricao"`
			MesAno           string  `json:"mes_ano"`
			ImportID         string  `json:"import_id"`
			ImportadoEm      string  `json:"importado_em"`
		}

		items := []pagItem{}
		for rows.Next() {
			var item pagItem
			var dataPag time.Time
			var importadoEm time.Time
			if err := rows.Scan(
				&item.ID, &item.ChaveDoc, &item.TipoDoc, &item.FornCNPJ, &item.FornNome,
				&dataPag, &item.ValorPagamento, &item.NumDocPagamento, &item.Descricao,
				&item.MesAno, &item.ImportID, &importadoEm,
			); err != nil {
				log.Printf("[PagamentosListHandler] scan: %v", err)
				continue
			}
			item.DataPagamento = dataPag.Format("2006-01-02")
			item.ImportadoEm = importadoEm.Format(time.RFC3339)
			items = append(items, item)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items":     items,
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		})
	}
}

// ─── PagamentosImportsListHandler ─────────────────────────────────────────────
// GET /api/pagamentos-fornecedores/imports
// Retorna histórico de batches de importação da empresa ativa.

func PagamentosImportsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}

		rows, err := db.Query(`
			SELECT id, COALESCE(mes_ano,''), COALESCE(filename,''),
			       total_linhas, importados, duplicados, erros,
			       COALESCE(importado_por::text,''), importado_em
			FROM pagamentos_imports
			WHERE company_id = $1
			ORDER BY importado_em DESC
			LIMIT 100`, companyID,
		)
		if err != nil {
			log.Printf("[PagamentosImportsList] query: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao listar imports")
			return
		}
		defer rows.Close()

		type batchItem struct {
			ID          string `json:"id"`
			MesAno      string `json:"mes_ano"`
			Filename    string `json:"filename"`
			TotalLinhas int    `json:"total_linhas"`
			Importados  int    `json:"importados"`
			Duplicados  int    `json:"duplicados"`
			Erros       int    `json:"erros"`
			ImportadoPor string `json:"importado_por"`
			ImportadoEm string `json:"importado_em"`
		}

		items := []batchItem{}
		for rows.Next() {
			var item batchItem
			var importadoEm time.Time
			if err := rows.Scan(
				&item.ID, &item.MesAno, &item.Filename,
				&item.TotalLinhas, &item.Importados, &item.Duplicados, &item.Erros,
				&item.ImportadoPor, &importadoEm,
			); err != nil {
				log.Printf("[PagamentosImportsList] scan: %v", err)
				continue
			}
			item.ImportadoEm = importadoEm.Format(time.RFC3339)
			items = append(items, item)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"items": items})
	}
}

// ─── PagamentosImportsDeleteHandler ──────────────────────────────────────────
// DELETE /api/pagamentos-fornecedores/imports/{id}
// Apaga todos os pagamentos do batch e o próprio registro de batch.

func PagamentosImportsDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}

		importID := strings.TrimPrefix(r.URL.Path, "/api/pagamentos-fornecedores/imports/")
		importID = strings.TrimSuffix(importID, "/")
		if !reUUID.MatchString(importID) {
			jsonErr(w, http.StatusBadRequest, "ID de import inválido")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[PagamentosImportsDelete] begin tx: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao iniciar transação")
			return
		}
		defer tx.Rollback()

		// Deletar os pagamentos do batch
		res, err := tx.Exec(`
			DELETE FROM pagamentos_fornecedores
			WHERE import_id = $1::uuid AND company_id = $2`, importID, companyID,
		)
		if err != nil {
			log.Printf("[PagamentosImportsDelete] delete pagamentos: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao apagar pagamentos")
			return
		}
		deleted, _ := res.RowsAffected()

		// Deletar o registro de batch
		_, err = tx.Exec(`
			DELETE FROM pagamentos_imports
			WHERE id = $1::uuid AND company_id = $2`, importID, companyID,
		)
		if err != nil {
			log.Printf("[PagamentosImportsDelete] delete import record: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao apagar registro de importação")
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[PagamentosImportsDelete] commit: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao confirmar transação")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"deleted": deleted})
	}
}

// ─── PagamentosTemplateHandler ────────────────────────────────────────────────
// GET /api/pagamentos-fornecedores/template
// Retorna um CSV de template para download.

func PagamentosTemplateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		const templateCSV = `chave_doc,tipo_doc,forn_cnpj,forn_nome,data_pagamento,valor_pagamento,num_doc_pagamento,descricao
35240112345678000195550010000001231000000001,NFE,12345678000195,Fornecedor Exemplo LTDA,15/01/2024,1500.00,DOC-001,Pagamento parcela 1
35240112345678000195550010000001231000000001,NFE,12345678000195,Fornecedor Exemplo LTDA,15/02/2024,1500.00,DOC-002,Pagamento parcela 2
NFSE-2024-001234,NFSE,98765432000110,Prestadora de Servicos SA,20/01/2024,3200.50,NFS-001,Servico de consultoria
`

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="template_pagamentos_fornecedores.csv"`)
		fmt.Fprint(w, templateCSV)
	}
}

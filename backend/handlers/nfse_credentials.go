package handlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"fb_apu02/crypto"

	"github.com/golang-jwt/jwt/v5"
)

// NFSeCertificado representa os METADADOS do certificado digital A1 de uma empresa.
// certificado_pfx e certificado_senha propositalmente NÃO são campos deste struct —
// nenhuma rota de leitura os expõe (nem em claro, nem criptografados); só
// TemCertificado informa se já existe um certificado cadastrado (Spec Change Log,
// achado "patch": declarar esses campos no struct sem nunca populá-los é uma
// armadilha para uso futuro).
type NFSeCertificado struct {
	ID             string    `json:"id"`
	CompanyID      string    `json:"company_id"`
	TemCertificado bool      `json:"tem_certificado"`
	NomeArquivo    string    `json:"nome_arquivo"`
	Validade       *string   `json:"validade"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// stripWhitespace remove TODO espaço em branco de s, incluindo quebras de linha internas —
// não só as pontas (strings.TrimSpace). Ferramentas de linha de comando como `base64
// arquivo.pfx` no Linux quebram a saída a cada 76 caracteres; sem isso o valor seria
// rejeitado como "não é base64 válido" incorretamente.
func stripWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// GetNFSeCertificadoHandler retorna os metadados do certificado NFS-e da empresa
// efetiva — nunca o certificado nem a senha. Empresa sem certificado cadastrado
// retorna HTTP 200 com tem_certificado:false (não 404/500). status é recalculado na
// leitura: um certificado com status='ativo' persistido mas validade já vencida é
// reportado como 'vencido' mesmo que a coluna ainda diga 'ativo'.
func GetNFSeCertificadoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[NFSeCertificado]")
			return
		}

		var cert NFSeCertificado
		var nomeArquivo sql.NullString
		var validade sql.NullTime
		var status string
		err = db.QueryRow(`
			SELECT id, company_id, COALESCE(nome_arquivo, ''), validade,
			       CASE WHEN status = 'ativo' AND validade IS NOT NULL AND validade < CURRENT_DATE
			            THEN 'vencido' ELSE status END,
			       created_at, updated_at
			FROM nfse_certificados WHERE company_id = $1
		`, companyID).Scan(
			&cert.ID, &cert.CompanyID, &nomeArquivo, &validade, &status, &cert.CreatedAt, &cert.UpdatedAt,
		)
		if err == sql.ErrNoRows {
			// {"certificado": nil} — nunca um NFSeCertificado{} de valor zero, que serializaria
			// created_at/updated_at como "0001-01-01T00:00:00Z". Mesmo padrão de
			// GetCGIBSCredentialHandler ({"credential": nil}) em cgibs_credentials.go.
			json.NewEncoder(w).Encode(map[string]interface{}{"certificado": nil})
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar certificado NFS-e", err, "[NFSeCertificado]")
			return
		}

		cert.TemCertificado = true
		cert.NomeArquivo = nomeArquivo.String
		cert.Status = status
		if validade.Valid {
			v := validade.Time.Format("2006-01-02")
			cert.Validade = &v
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"certificado": cert})
	}
}

// SaveNFSeCertificadoHandler cadastra ou atualiza (UPSERT por company_id) o
// certificado digital A1 da empresa efetiva. certificado_pfx (arquivo .pfx em
// base64) e certificado_senha são sempre criptografados antes de persistir.
//
// Qualquer campo vazio (certificado_pfx, certificado_senha ou validade) PRESERVA o
// valor já persistido em vez de apagá-lo — mesmo padrão de "ler o registro existente
// antes de decidir o que preservar" já usado em SaveCGIBSCredentialHandler
// (cgibs_credentials.go) para client_secret — decisão do usuário registrada no Spec
// Change Log (loopback 1). A única exceção é o PRIMEIRO cadastro (sem registro
// existente para a empresa): nesse caso certificado_pfx/certificado_senha continuam
// obrigatórios.
//
// Retorna 201 apenas quando é um cadastro novo; 200 quando é uma atualização de um
// registro já existente.
func SaveNFSeCertificadoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limite de tamanho do corpo — mesmo padrão já usado para uploads (xml_upload.go,
		// pagamentos_fornecedores.go). Contém DoS de memória/disco com um .pfx "gigante" em
		// base64 no corpo JSON.
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes())

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[NFSeCertificado]")
			return
		}

		var req struct {
			CertificadoPfx   string `json:"certificado_pfx"`
			CertificadoSenha string `json:"certificado_senha"`
			NomeArquivo      string `json:"nome_arquivo"`
			Validade         string `json:"validade"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// http.MaxBytesReader (acima) aborta a leitura do corpo com esse erro específico
			// quando o corpo excede o limite — nesse caso 413 é mais correto e mais claro para o
			// cliente do que o 400 genérico "Dados inválidos" usado para qualquer outro erro de
			// decode.
			if strings.Contains(err.Error(), "http: request body too large") {
				jsonErr(w, http.StatusRequestEntityTooLarge, "Certificado excede o tamanho máximo permitido")
				return
			}
			jsonErr(w, http.StatusBadRequest, "Dados inválidos")
			return
		}

		// stripWhitespace (não só TrimSpace) em certificado_pfx: remove quebras de linha
		// internas inseridas por ferramentas de linha de comando (ex. `base64 arquivo.pfx`),
		// antes da validação de base64 e antes de criptografar.
		req.CertificadoPfx = stripWhitespace(req.CertificadoPfx)
		req.CertificadoSenha = strings.TrimSpace(req.CertificadoSenha)
		req.NomeArquivo = strings.TrimSpace(req.NomeArquivo)
		req.Validade = strings.TrimSpace(req.Validade)

		// Valida que certificado_pfx é base64 decodificável (não valida que é um .pfx/PKCS#12
		// de verdade — isso exigiria parsing de PKCS#12, fora de escopo desta sub-spec).
		if req.CertificadoPfx != "" {
			if _, decErr := base64.StdEncoding.DecodeString(req.CertificadoPfx); decErr != nil {
				jsonErr(w, http.StatusBadRequest, "certificado_pfx deve ser o arquivo .pfx codificado em base64 válido")
				return
			}
		}

		// Round-trip check: time.Parse aceita datas de calendário inválidas (ex. 30/02)
		// normalizando silenciosamente para outra data (ex. 02/03). Reformatar o resultado e
		// comparar com a string original detecta isso sem precisar de validação manual de
		// calendário (Spec Change Log, achado "patch").
		var validadeParsed time.Time
		var validadeInformada bool
		if req.Validade != "" {
			t, perr := time.Parse("2006-01-02", req.Validade)
			if perr != nil || t.Format("2006-01-02") != req.Validade {
				jsonErr(w, http.StatusBadRequest, "validade inválida — use o formato AAAA-MM-DD com uma data de calendário existente")
				return
			}
			validadeParsed = t
			validadeInformada = true
		}

		// Lê o registro já salvo (se houver) ANTES de gravar — mesmo padrão de
		// SaveCGIBSCredentialHandler: decide o que preservar quando o request vem com campos
		// vazios, em vez de rejeitar o save inteiro ou apagar o valor já persistido.
		var existingID, existingPfxEnc, existingSenhaEnc string
		var existingNomeArquivo sql.NullString
		var existingValidade sql.NullTime
		hasExisting := true
		if err := db.QueryRow(`
			SELECT id, certificado_pfx, certificado_senha, nome_arquivo, validade
			FROM nfse_certificados WHERE company_id = $1
		`, companyID).Scan(&existingID, &existingPfxEnc, &existingSenhaEnc, &existingNomeArquivo, &existingValidade); err != nil {
			if err != sql.ErrNoRows {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar certificado NFS-e existente", err, "[NFSeCertificado]")
				return
			}
			hasExisting = false
		}

		// certificado_pfx/certificado_senha/validade só são opcionais quando já existe um
		// registro anterior para preservar — no primeiro cadastro continuam obrigatórios. Sem
		// validade, o cálculo de vencimento em GetNFSeCertificadoHandler nunca funcionaria para
		// esse certificado.
		if !hasExisting && (req.CertificadoPfx == "" || req.CertificadoSenha == "" || req.Validade == "") {
			jsonErr(w, http.StatusBadRequest, "Certificado (.pfx em base64), senha e validade são obrigatórios no primeiro cadastro")
			return
		}

		// Ao ATUALIZAR um registro existente, certificado_pfx e certificado_senha formam um par
		// que nunca deve dessincronizar: se só um dos dois vier preenchido, rejeita e pede os
		// dois juntos. Quando os dois vêm vazios, o comportamento de preservar ambos os valores
		// anteriores (abaixo) continua normalmente.
		if hasExisting && (req.CertificadoPfx == "") != (req.CertificadoSenha == "") {
			jsonErr(w, http.StatusBadRequest, "Certificado (.pfx em base64) e senha devem ser enviados juntos ao trocar o certificado")
			return
		}

		pfxToSave := existingPfxEnc
		if req.CertificadoPfx != "" {
			enc, encErr := crypto.EncryptField(req.CertificadoPfx)
			if encErr != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao proteger certificado", encErr, "[NFSeCertificado]")
				return
			}
			pfxToSave = enc
		}

		senhaToSave := existingSenhaEnc
		if req.CertificadoSenha != "" {
			enc, encErr := crypto.EncryptField(req.CertificadoSenha)
			if encErr != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao proteger senha do certificado", encErr, "[NFSeCertificado]")
				return
			}
			senhaToSave = enc
		}

		nomeArquivoToSave := req.NomeArquivo
		if nomeArquivoToSave == "" && hasExisting {
			nomeArquivoToSave = existingNomeArquivo.String
		}

		// validade vazia preserva o valor já persistido (Spec Change Log — gap #2 do loopback
		// 1): gravar NULL aqui derrotaria para sempre o cálculo de vencimento em
		// GetNFSeCertificadoHandler (status='ativo' AND validade < CURRENT_DATE nunca mais
		// marcaria 'vencido').
		var validadeParam interface{}
		if validadeInformada {
			validadeParam = validadeParsed
		} else if hasExisting && existingValidade.Valid {
			validadeParam = existingValidade.Time
		} else {
			validadeParam = nil
		}

		var id string
		err = db.QueryRow(`
			INSERT INTO nfse_certificados (company_id, certificado_pfx, certificado_senha, nome_arquivo, validade, status)
			VALUES ($1, $2, $3, $4, $5, 'ativo')
			ON CONFLICT (company_id) DO UPDATE SET
				certificado_pfx   = $2,
				certificado_senha = $3,
				nome_arquivo      = $4,
				validade          = $5,
				status            = 'ativo',
				updated_at        = NOW()
			RETURNING id
		`, companyID, pfxToSave, senhaToSave, nomeArquivoToSave, validadeParam).Scan(&id)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao salvar certificado NFS-e", err, "[NFSeCertificado]")
			return
		}

		log.Printf("[NFSeCertificado] Certificado NFS-e salvo por userID=%s companyID=%s (novo=%v)", userID, companyID, !hasExisting)

		var cert NFSeCertificado
		var nomeArquivoResp sql.NullString
		var validadeResp sql.NullTime
		var statusResp string
		if err := db.QueryRow(`
			SELECT id, company_id, COALESCE(nome_arquivo, ''), validade, status, created_at, updated_at
			FROM nfse_certificados WHERE id = $1
		`, id).Scan(&cert.ID, &cert.CompanyID, &nomeArquivoResp, &validadeResp, &statusResp, &cert.CreatedAt, &cert.UpdatedAt); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao reler certificado NFS-e salvo", err, "[NFSeCertificado]")
			return
		}
		cert.TemCertificado = true
		cert.NomeArquivo = nomeArquivoResp.String
		cert.Status = statusResp
		if validadeResp.Valid {
			v := validadeResp.Time.Format("2006-01-02")
			cert.Validade = &v
		}

		if hasExisting {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"certificado": cert,
			"message":     "Certificado NFS-e salvo com sucesso",
		})
	}
}

// DeleteNFSeCertificadoHandler exclui (hard delete) o certificado NFS-e da empresa
// efetiva.
func DeleteNFSeCertificadoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[NFSeCertificado]")
			return
		}

		result, err := db.Exec("DELETE FROM nfse_certificados WHERE company_id=$1", companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao excluir certificado NFS-e", err, "[NFSeCertificado]")
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			jsonErr(w, http.StatusNotFound, "Nenhum certificado encontrado")
			return
		}
		log.Printf("[NFSeCertificado] Certificado NFS-e excluído por userID=%s companyID=%s", userID, companyID)
		w.WriteHeader(http.StatusNoContent)
	}
}

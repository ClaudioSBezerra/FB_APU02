package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
)

// NFSeBuscaRL limita POST /api/nfse/buscar a 1 chamada a cada 30 segundos por empresa — mesmo
// padrão de LoginRL/RegisterRL (handlers/auth.go). Protege contra duplo-clique/retry do
// frontend gerando tráfego repetido contra uma API de governo.
var NFSeBuscaRL = newRateLimiter(1, 30*time.Second)

// BuscarNFSeHandler dispara UM ciclo síncrono de busca manual ("Buscar Agora") de NFS-e no
// Portal Nacional ADN para a empresa efetiva: valida o certificado ativo, autentica via mTLS,
// busca os documentos disponíveis (services.BuscarTodosDocumentosADN) e persiste os novos em
// nfse_entradas/nfse_saidas, sem duplicar. Sem agendamento automático — próxima sub-spec.
//
// Certificado ausente ou vencido/revogado retorna 400 SEM tentar nenhuma chamada de rede (ver
// I/O Matrix da spec-nfse-cliente-adn.md). Falha de mTLS ou formato de resposta inesperado
// retornam 502 sem gravar nada no banco.
func BuscarNFSeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[NFSeBusca]")
			return
		}

		if !NFSeBuscaRL.Allow(companyID) {
			jsonErr(w, http.StatusTooManyRequests, "Muitas solicitações de busca NFS-e para esta empresa — aguarde 30 segundos antes de tentar novamente")
			return
		}

		// Mesma query de status calculado usada em GetNFSeCertificadoHandler
		// (nfse_credentials.go): um certificado com status='ativo' persistido mas validade já
		// vencida é reportado como 'vencido' mesmo que a coluna ainda diga 'ativo'.
		var pfxEnc, senhaEnc, status string
		err = db.QueryRow(`
			SELECT certificado_pfx, certificado_senha,
			       CASE WHEN status = 'ativo' AND validade IS NOT NULL AND validade < CURRENT_DATE
			            THEN 'vencido' ELSE status END
			FROM nfse_certificados WHERE company_id = $1
		`, companyID).Scan(&pfxEnc, &senhaEnc, &status)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusBadRequest, "Certificado NFS-e não cadastrado para esta empresa")
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar certificado NFS-e", err, "[NFSeBusca]")
			return
		}
		if status != "ativo" {
			jsonErr(w, http.StatusBadRequest, "Certificado NFS-e indisponível (vencido ou revogado) — atualize o certificado antes de buscar")
			return
		}

		certDecrypted, err := services.DecryptNFSeCertificado(pfxEnc, senhaEnc)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao processar certificado NFS-e", err, "[NFSeBusca]")
			return
		}

		httpClient, x509Cert, err := services.BuildNFSeMTLSClient(certDecrypted)
		if err != nil {
			log.Printf("[NFSeBusca] Falha ao montar client mTLS companyID=%s: %v", companyID, err)
			jsonErr(w, http.StatusBadGateway, "Falha ao carregar certificado NFS-e para autenticação (arquivo ou senha inválidos)")
			return
		}

		cnpjEmpresa, err := services.ExtractCNPJFromCertificateSubject(x509Cert)
		if err != nil {
			log.Printf("[NFSeBusca] Falha ao identificar CNPJ do certificado companyID=%s: %v", companyID, err)
			jsonErr(w, http.StatusBadGateway, "Não foi possível identificar o CNPJ titular do certificado NFS-e")
			return
		}

		baseURL := services.ResolveNFSeADNBaseURL()
		documentos, err := services.BuscarTodosDocumentosADN(r.Context(), httpClient, baseURL)
		if err != nil {
			switch {
			case errors.Is(err, services.ErrNFSeADNFormatoInesperado):
				log.Printf("[NFSeBusca] Formato de resposta inesperado do ADN companyID=%s: %v", companyID, err)
				jsonErr(w, http.StatusBadGateway, "Portal Nacional ADN retornou um formato de resposta inesperado")
				return
			case errors.Is(err, services.ErrNFSeADNComunicacaoFalhou):
				log.Printf("[NFSeBusca] Falha de comunicação com o Portal Nacional ADN companyID=%s: %v", companyID, err)
				jsonErr(w, http.StatusBadGateway, "Falha de comunicação com o Portal Nacional ADN — tente novamente mais tarde")
				return
			default:
				log.Printf("[NFSeBusca] Falha na comunicação com o Portal Nacional ADN companyID=%s: %v", companyID, err)
				jsonErr(w, http.StatusBadGateway, "Falha de autenticação junto ao Portal Nacional ADN (certificado rejeitado ou falha de comunicação)")
				return
			}
		}

		novos, ignorados, err := persistirDocumentosNFSe(db, companyID, cnpjEmpresa, documentos)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao gravar documentos NFS-e", err, "[NFSeBusca]")
			return
		}

		log.Printf("[NFSeBusca] Busca concluída userID=%s companyID=%s documentosRecebidos=%d documentosNovos=%d documentosIgnorados=%d",
			userID, companyID, len(documentos), novos, ignorados)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"documentos_recebidos": len(documentos),
			"documentos_novos":     novos,
			"documentos_ignorados": ignorados,
		})
	}
}

// nfseTabela identifica a tabela de destino de um INSERT de documento NFS-e — usado em vez de
// montar o nome da tabela por fmt.Sprintf na query (prática arriscada de copiar num refactor
// futuro): as duas queries abaixo são strings literais, escolhidas via switch.
type nfseTabela int

const (
	nfseTabelaEntradas nfseTabela = iota
	nfseTabelaSaidas
)

const nfseInsertEntradasQuery = `
	INSERT INTO nfse_entradas (company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, data_emissao, mes_ano, valor_servicos)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (company_id, identificador_nfse) DO NOTHING
`

const nfseInsertSaidasQuery = `
	INSERT INTO nfse_saidas (company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, data_emissao, mes_ano, valor_servicos)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (company_id, identificador_nfse) DO NOTHING
`

// inserirNFSeDocumento executa o INSERT idempotente (ON CONFLICT DO NOTHING) de um documento em
// nfse_entradas ou nfse_saidas, dentro da transação tx. Devolve quantas linhas foram
// efetivamente inseridas (0 ou 1).
func inserirNFSeDocumento(tx *sql.Tx, tabela nfseTabela, companyID, identificador, cnpjPrestador, cnpjTomador string, dataEmissao, mesAno interface{}, valorServicos float64) (int, error) {
	var query, nomeTabela string
	switch tabela {
	case nfseTabelaEntradas:
		query = nfseInsertEntradasQuery
		nomeTabela = "nfse_entradas"
	case nfseTabelaSaidas:
		query = nfseInsertSaidasQuery
		nomeTabela = "nfse_saidas"
	default:
		return 0, fmt.Errorf("tabela NFS-e desconhecida: %v", tabela)
	}

	result, err := tx.Exec(query, companyID, identificador, nullableNFSeCNPJ(cnpjPrestador), nullableNFSeCNPJ(cnpjTomador), dataEmissao, mesAno, valorServicos)
	if err != nil {
		return 0, fmt.Errorf("insert em %s: %w", nomeTabela, err)
	}
	rows, rerr := result.RowsAffected()
	if rerr != nil {
		log.Printf("[NFSeBusca] Falha ao obter RowsAffected após insert em %s (documento %s): %v", nomeTabela, identificador, rerr)
	}
	return int(rows), nil
}

// persistirDocumentosNFSe grava cada documento em nfse_entradas (a empresa, identificada pelo
// CNPJ titular do certificado mTLS, é a tomadora) ou nfse_saidas (a empresa é a prestadora),
// com ON CONFLICT (company_id, identificador_nfse) DO NOTHING — idempotente entre ciclos
// manuais repetidos (ver TestBuscarNFSeHandler_SegundaChamada_NaoDuplicaDocumentos). Devolve
// quantos documentos foram efetivamente inseridos (novos) e quantos foram ignorados (CNPJ não
// reconhecido). Todo o lote é gravado dentro de uma única transação — se um INSERT falhar no
// meio, nenhuma linha fica parcialmente persistida.
//
// Documentos onde o CNPJ do certificado não é nem prestador nem tomador são ignorados (só
// logados) — não deveriam ocorrer dado que a consulta ao ADN é feita com o certificado da
// própria empresa, mas defensivamente nunca gravamos um documento sem saber de que lado a
// empresa está. O mesmo vale para CNPJs de prestador/tomador que, quando informados, não têm
// exatamente 14 dígitos — tratados como CNPJ não reconhecido.
//
// Quando a empresa é simultaneamente prestadora E tomadora do mesmo documento (autoemitido —
// situação pouco comum), o documento é gravado em AMBAS as tabelas.
func persistirDocumentosNFSe(db *sql.DB, companyID, cnpjEmpresa string, documentos []services.NFSeADNDocumento) (novos int, ignorados int, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, doc := range documentos {
		identificador := doc.IdentificadorNFSe
		if identificador == "" {
			continue
		}

		cnpjPrestador := services.OnlyDigits(doc.CNPJPrestador)
		cnpjTomador := services.OnlyDigits(doc.CNPJTomador)

		if (cnpjPrestador != "" && len(cnpjPrestador) != 14) || (cnpjTomador != "" && len(cnpjTomador) != 14) {
			log.Printf("[NFSeBusca] Documento %s ignorado — CNPJ prestador (%q) ou tomador (%q) não tem 14 dígitos",
				identificador, doc.CNPJPrestador, doc.CNPJTomador)
			ignorados++
			continue
		}

		var dataEmissao interface{}
		var mesAno interface{}
		if t, perr := time.Parse("2006-01-02", doc.DataEmissao); perr == nil {
			dataEmissao = t
			mesAno = t.Format("01/2006")
		} else {
			log.Printf("[NFSeBusca] Documento %s — data de emissão não pôde ser interpretada (valor recebido: %q): %v",
				identificador, doc.DataEmissao, perr)
		}

		ehTomador := cnpjTomador != "" && cnpjEmpresa == cnpjTomador
		ehPrestador := cnpjPrestador != "" && cnpjEmpresa == cnpjPrestador

		if !ehTomador && !ehPrestador {
			log.Printf("[NFSeBusca] Documento %s ignorado — CNPJ do certificado (%s) não é prestador (%s) nem tomador (%s)",
				identificador, cnpjEmpresa, cnpjPrestador, cnpjTomador)
			ignorados++
			continue
		}

		if ehTomador && ehPrestador {
			log.Printf("[NFSeBusca] Documento %s autoemitido — CNPJ do certificado (%s) é prestador e tomador simultaneamente, gravando em nfse_entradas e nfse_saidas",
				identificador, cnpjEmpresa)
			n, ierr := inserirNFSeDocumento(tx, nfseTabelaEntradas, companyID, identificador, cnpjPrestador, cnpjTomador, dataEmissao, mesAno, doc.ValorServicos)
			if ierr != nil {
				return novos, ignorados, ierr
			}
			novos += n
			n, ierr = inserirNFSeDocumento(tx, nfseTabelaSaidas, companyID, identificador, cnpjPrestador, cnpjTomador, dataEmissao, mesAno, doc.ValorServicos)
			if ierr != nil {
				return novos, ignorados, ierr
			}
			novos += n
			continue
		}

		tabela := nfseTabelaSaidas
		if ehTomador {
			tabela = nfseTabelaEntradas
		}
		n, ierr := inserirNFSeDocumento(tx, tabela, companyID, identificador, cnpjPrestador, cnpjTomador, dataEmissao, mesAno, doc.ValorServicos)
		if ierr != nil {
			return novos, ignorados, ierr
		}
		novos += n
	}

	if cerr := tx.Commit(); cerr != nil {
		return novos, ignorados, fmt.Errorf("commit tx: %w", cerr)
	}
	return novos, ignorados, nil
}

func nullableNFSeCNPJ(c string) interface{} {
	if c == "" {
		return nil
	}
	return c
}

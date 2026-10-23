package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
)

// NFSeBuscaRL limita POST /api/nfse/buscar a 1 chamada a cada 30 segundos por empresa — mesmo
// padrão de LoginRL/RegisterRL (handlers/auth.go). Protege contra duplo-clique/retry do
// frontend gerando tráfego repetido contra uma API de governo. É a MESMA instância usada pelo
// scheduler automático (services.StartNFSeScheduler, services/nfse_scheduler.go) — definida em
// services (services.NFSeBuscaRL) porque o scheduler também precisa dela e `services` não pode
// depender de `handlers`; ver services/nfse_processor.go.
var NFSeBuscaRL = services.NFSeBuscaRL

// BuscarNFSeHandler dispara UM ciclo síncrono de busca manual ("Buscar Agora") de NFS-e no
// Portal Nacional ADN para a empresa efetiva, delegando toda a execução (validar certificado,
// autenticar via mTLS, buscar e persistir) para services.ExecutarCicloNFSe — a MESMA função
// chamada pelo scheduler automático (services.StartNFSeScheduler). Este handler só resolve
// companyID/autenticação HTTP, aplica o rate limit e mapeia o erro devolvido pro status/mensagem
// HTTP correspondente; nenhuma lógica de persistência vive aqui.
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

		recebidos, novos, ignorados, err := services.ExecutarCicloNFSe(r.Context(), db, companyID)
		if err != nil {
			mapExecutarCicloNFSeErr(w, companyID, err)
			return
		}

		log.Printf("[NFSeBusca] Busca concluída userID=%s companyID=%s documentosRecebidos=%d documentosNovos=%d documentosIgnorados=%d",
			userID, companyID, recebidos, novos, ignorados)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"documentos_recebidos": recebidos,
			"documentos_novos":     novos,
			"documentos_ignorados": ignorados,
		})
	}
}

// mapExecutarCicloNFSeErr traduz um erro de services.ExecutarCicloNFSe pro status/mensagem HTTP
// correspondente — a única parte da antiga lógica de BuscarNFSeHandler que permanece no handler
// (é específica de HTTP; a execução do ciclo em si mora em services, ver ExecutarCicloNFSe).
func mapExecutarCicloNFSeErr(w http.ResponseWriter, companyID string, err error) {
	switch {
	case errors.Is(err, services.ErrNFSeCertificadoNaoCadastrado):
		jsonErr(w, http.StatusBadRequest, "Certificado NFS-e não cadastrado para esta empresa")
	case errors.Is(err, services.ErrNFSeCertificadoIndisponivel):
		jsonErr(w, http.StatusBadRequest, "Certificado NFS-e indisponível (vencido ou revogado) — atualize o certificado antes de buscar")
	case errors.Is(err, services.ErrNFSeCertificadoProcessamento):
		sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao processar certificado NFS-e", err, "[NFSeBusca]")
	case errors.Is(err, services.ErrNFSeMTLSClientInvalido):
		jsonErr(w, http.StatusBadGateway, "Falha ao carregar certificado NFS-e para autenticação (arquivo ou senha inválidos)")
	case errors.Is(err, services.ErrNFSeCNPJNaoIdentificado):
		jsonErr(w, http.StatusBadGateway, "Não foi possível identificar o CNPJ titular do certificado NFS-e")
	case errors.Is(err, services.ErrNFSeADNFormatoInesperado):
		jsonErr(w, http.StatusBadGateway, "Portal Nacional ADN retornou um formato de resposta inesperado")
	case errors.Is(err, services.ErrNFSeADNComunicacaoFalhou):
		jsonErr(w, http.StatusBadGateway, "Falha de comunicação com o Portal Nacional ADN — tente novamente mais tarde")
	case errors.Is(err, services.ErrNFSeADNAutenticacaoFalhou):
		jsonErr(w, http.StatusBadGateway, "Falha de autenticação junto ao Portal Nacional ADN (certificado rejeitado ou falha de comunicação)")
	case errors.Is(err, services.ErrNFSeADNLimiteLotes):
		jsonErr(w, http.StatusBadGateway, "Captura incompleta — Portal Nacional ADN excedeu o limite de páginas numa única busca")
	case errors.Is(err, services.ErrNFSePersistencia):
		sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao gravar documentos NFS-e", err, "[NFSeBusca]")
	default:
		sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao executar busca de NFS-e", err, "[NFSeBusca]")
	}
}

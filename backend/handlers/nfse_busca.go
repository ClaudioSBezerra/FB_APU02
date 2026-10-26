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
// companyID/autenticação HTTP, aplica o rate limit e mapeia o resultado/erro pro status/mensagem
// HTTP correspondente; nenhuma lógica de persistência vive aqui.
//
// O ciclo manual roda com services.NFSeCicloManualMaxDuracao (4 min), abaixo do WriteTimeout
// de 300 s do servidor (main.go) — a resposta sempre chega ao cliente.
//
// Certificado ausente ou vencido/revogado retorna 400 SEM tentar nenhuma chamada de rede (ver
// I/O Matrix da spec-nfse-cliente-adn.md). Ciclo já em andamento para a empresa → 409. Falha de
// mTLS, rejeição (400 REJEICAO) ou formato de resposta inesperado retornam 502; lotes gravados
// ANTES da falha permanecem (cada lote é uma transação com o cursor ultimo_nsu —
// spec-nfse-adn-parser-real.md). Fim do tempo/limite de lotes/transitório esgotado com algum lote
// gravado é 200 com parcial=true e motivo_encerramento (o próximo ciclo continua); transitório
// esgotado SEM nenhum lote gravado é 502. Toda resposta (inclusive erro) traz as contagens.
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

		res, err := services.ExecutarCicloNFSe(r.Context(), db, companyID, services.NFSeCicloOpcoes{MaxDuracao: services.NFSeCicloManualMaxDuracao})
		if err != nil {
			mapExecutarCicloNFSeErr(w, companyID, err, res)
			return
		}

		if status, msg := statusRespostaCicloNFSe(res); status != http.StatusOK {
			log.Printf("[NFSeBusca] %s userID=%s companyID=%s encerramento=%s", msg, userID, companyID, res.Encerramento)
			escreverRespostaBuscaNFSe(w, status, res, msg)
			return
		}

		log.Printf("[NFSeBusca] Busca concluída userID=%s companyID=%s encerramento=%s parcial=%t documentosRecebidos=%d documentosNovos=%d documentosAtualizados=%d documentosIgnorados=%d documentosFalhas=%d",
			userID, companyID, res.Encerramento, res.Parcial(), res.Recebidos, res.Novos, res.Atualizados, res.Ignorados, res.Falhas)
		escreverRespostaBuscaNFSe(w, http.StatusOK, res, "")
	}
}

// statusRespostaCicloNFSe decide o status de um ciclo que terminou SEM erro: transitório
// esgotado sem nenhum lote gravado é 502 (nada aconteceu — o ADN está fora); qualquer outro
// encerramento (inclusive parcial com progresso) é 200.
func statusRespostaCicloNFSe(res services.NFSeResultadoCiclo) (int, string) {
	if res.Encerramento == services.NFSeADNEncerramentoTransitorio && res.Lotes == 0 {
		return http.StatusBadGateway, "Portal Nacional ADN temporariamente indisponível (tentativas esgotadas, nada foi gravado) — tente novamente mais tarde"
	}
	return http.StatusOK, ""
}

// escreverRespostaBuscaNFSe escreve o corpo JSON de POST /api/nfse/buscar: contagens sempre;
// parcial sempre; motivo_encerramento quando parcial; "error" quando msgErro != "".
func escreverRespostaBuscaNFSe(w http.ResponseWriter, status int, res services.NFSeResultadoCiclo, msgErro string) {
	out := map[string]interface{}{
		"documentos_recebidos":   res.Recebidos,
		"documentos_novos":       res.Novos,
		"documentos_atualizados": res.Atualizados,
		"documentos_ignorados":   res.Ignorados,
		"documentos_falhas":      res.Falhas,
		"parcial":                res.Parcial(),
	}
	if res.Parcial() && res.Encerramento != "" {
		out["motivo_encerramento"] = res.Encerramento
	}
	if msgErro != "" {
		out["error"] = msgErro
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(out)
}

// mapExecutarCicloNFSeErr traduz um erro de services.ExecutarCicloNFSe pro status/mensagem HTTP
// correspondente — a única parte da antiga lógica de BuscarNFSeHandler que permanece no handler
// (é específica de HTTP; a execução do ciclo em si mora em services, ver ExecutarCicloNFSe). A
// resposta carrega as contagens do que já foi gravado antes da falha (res).
func mapExecutarCicloNFSeErr(w http.ResponseWriter, companyID string, err error, res services.NFSeResultadoCiclo) {
	responder := func(status int, msg string) { escreverRespostaBuscaNFSe(w, status, res, msg) }
	logar := func(msg string) { log.Printf("[NFSeBusca] %s companyID=%s: %v", msg, companyID, err) }
	switch {
	case errors.Is(err, services.ErrNFSeCicloEmAndamento):
		responder(http.StatusConflict, "Captura já em andamento para esta empresa — aguarde o término")
	case errors.Is(err, services.ErrNFSeCertificadoNaoCadastrado):
		responder(http.StatusBadRequest, "Certificado NFS-e não cadastrado para esta empresa")
	case errors.Is(err, services.ErrNFSeCertificadoIndisponivel):
		responder(http.StatusBadRequest, "Certificado NFS-e indisponível (vencido ou revogado) — atualize o certificado antes de buscar")
	case errors.Is(err, services.ErrNFSeCertificadoProcessamento):
		logar("Erro ao processar certificado NFS-e")
		responder(http.StatusInternalServerError, "Erro ao processar certificado NFS-e")
	case errors.Is(err, services.ErrNFSeMTLSClientInvalido):
		responder(http.StatusBadGateway, "Falha ao carregar certificado NFS-e para autenticação (arquivo ou senha inválidos)")
	case errors.Is(err, services.ErrNFSeCNPJNaoIdentificado):
		responder(http.StatusBadGateway, "Não foi possível identificar o CNPJ titular do certificado NFS-e")
	case errors.Is(err, services.ErrNFSeADNFormatoInesperado):
		responder(http.StatusBadGateway, "Portal Nacional ADN retornou um formato de resposta inesperado")
	case errors.Is(err, services.ErrNFSeADNComunicacaoFalhou):
		responder(http.StatusBadGateway, "Falha de comunicação com o Portal Nacional ADN — tente novamente mais tarde")
	case errors.Is(err, services.ErrNFSeADNAutenticacaoFalhou):
		responder(http.StatusBadGateway, "Falha de autenticação junto ao Portal Nacional ADN (certificado rejeitado)")
	case errors.Is(err, services.ErrNFSeADNRejeicao):
		responder(http.StatusBadGateway, "Portal Nacional ADN rejeitou a consulta — documentos já recebidos foram gravados; veja o log para os códigos de erro")
	case errors.Is(err, services.ErrNFSeADNTransitorio):
		responder(http.StatusBadGateway, "Portal Nacional ADN temporariamente indisponível — tente novamente mais tarde")
	case errors.Is(err, services.ErrNFSePersistencia):
		logar("Erro ao gravar documentos NFS-e")
		responder(http.StatusInternalServerError, "Erro ao gravar documentos NFS-e")
	default:
		logar("Erro ao executar busca de NFS-e")
		responder(http.StatusInternalServerError, "Erro ao executar busca de NFS-e")
	}
}

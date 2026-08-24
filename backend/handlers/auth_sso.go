package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"fb_apu02/iam"
)

// SSOConfigHandler — GET /api/auth/sso/config (público, sem segredo).
//
// Expõe se o SSO Keycloak está configurado NESTE servidor específico e, se sim, os
// parâmetros que o frontend precisa pra montar a URL de login (nenhum é sensível — é
// o mesmo tipo de dado que já fica visível na URL de authorize de qualquer client
// público OIDC/PKCE). Buscado em runtime pelo frontend em vez de cravado em build-time
// porque a MESMA imagem de frontend é compartilhada entre servidores diferentes (ex:
// Hostinger, multi-cliente, sem SSO; AWS, dedicado à Ferreira Costa, com SSO) — cravar
// a config no build vazaria o botão de SSO (quebrado, redirect não registrado) pra
// clientes que não são a Ferreira Costa.
func SSOConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		iamBaseURL := os.Getenv("IAM_BASE_URL")
		clientID := os.Getenv("IAM_CLIENT_ID")
		redirectURI := os.Getenv("IAM_REDIRECT_URI")
		// Só reporta enabled:true se TODAS as vars que o fluxo precisa estiverem presentes —
		// um .env incompleto (ex: IAM_BASE_URL setado mas IAM_CLIENT_ID esquecido) não pode
		// mostrar um botão de SSO que vai falhar ao clicar (achado de revisão: isso reproduz
		// via env var o mesmo problema de "botão quebrado" que essa mudança pra runtime
		// existe pra evitar). IAM_ALLOWED_CLIENT_IDS também é exigido aqui: sem ele, a
		// allowlist do middleware fica vazia e todo login via Keycloak falha (ver main.go).
		if iamBaseURL == "" || clientID == "" || redirectURI == "" || os.Getenv("IAM_ALLOWED_CLIENT_IDS") == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false})
			return
		}
		scopes := os.Getenv("IAM_SCOPES")
		if scopes == "" {
			scopes = "openid profile email"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":      true,
			"base_url":     iamBaseURL,
			"client_id":    clientID,
			"redirect_uri": redirectURI,
			"scopes":       scopes,
		})
	}
}

// KeycloakSSOHandler troca um token de acesso do Keycloak (já validado por
// iam.IAMAuthMiddleware antes desta função rodar — o handler é sempre registrado
// envolto nesse middleware, nunca exposto direto) por uma sessão própria do FB_APU02.
// Requisição: Authorization: Bearer <access_token do Keycloak> (mesmo header já
// validado pelo middleware, nada de campo extra no corpo).
//
// O e-mail extraído do token pelo middleware precisa já existir em `users` — este
// endpoint NUNCA cria usuário novo (decisão de negócio: usuários via SSO já existem
// no FB_APU02, casados por e-mail).
func KeycloakSSOHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !SSOKeycloakRL.Allow(GetClientIP(r)) {
			jsonErr(w, http.StatusTooManyRequests, "Muitas tentativas. Tente novamente mais tarde.")
			return
		}

		email := iam.GetIAMEmail(r.Context())
		if email == "" {
			log.Printf("[SSO Keycloak] Token válido mas sem claim 'email'")
			jsonErr(w, http.StatusBadRequest, "Token do Keycloak não contém e-mail.")
			return
		}

		// CRÍTICO (achado de revisão): sem checar email_verified, um usuário do Keycloak
		// poderia editar o próprio e-mail de perfil pra outro já cadastrado no FB_APU02
		// (se o realm permitir edição de perfil sem reconfirmação) e logar como essa outra
		// pessoa — o e-mail é a ÚNICA identidade usada aqui pra casar com `users`.
		if !iam.GetIAMEmailVerified(r.Context()) {
			log.Printf("[SSO Keycloak] Rejeitado: e-mail não verificado no Keycloak (%s)", email)
			jsonErr(w, http.StatusUnauthorized, "E-mail não verificado no Keycloak. Confirme seu e-mail e tente novamente.")
			return
		}

		start := time.Now()
		user, _, isBlocked, err := fetchUserByEmail(db, email)
		if err == sql.ErrNoRows {
			log.Printf("[SSO Keycloak] Usuário não encontrado: %s", email)
			jsonErr(w, http.StatusUnauthorized, "Usuário não encontrado no FB_APU02. Contate o administrador.")
			return
		}
		if err != nil {
			log.Printf("[SSO Keycloak] Erro ao buscar usuário: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[SSO Keycloak] Login via Keycloak: %s", email)
		finishLogin(db, w, r, user, isBlocked, start)
	}
}

package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"fb_apu02/iam"
)

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

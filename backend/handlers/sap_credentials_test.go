package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"fb_apu02/crypto"
)

// Testa paths pré-DB de GetSAPCredentialHandler/SaveSAPCredentialHandler. db=nil é seguro
// porque o check de claims ocorre antes de qualquer chamada ao banco.

func TestGetSAPCredentialHandlerUnauthorized(t *testing.T) {
	handler := GetSAPCredentialHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/sap/credentials", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestGetSAPCredentialHandlerWrongClaimsType(t *testing.T) {
	handler := GetSAPCredentialHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/sap/credentials", nil)
	ctx := context.WithValue(req.Context(), ClaimsKey, "not-a-jwt-mapclaims")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestSaveSAPCredentialHandlerUnauthorized(t *testing.T) {
	handler := SaveSAPCredentialHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/sap/credentials", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestSaveSAPCredentialHandlerMethodNotAllowed(t *testing.T) {
	handler := SaveSAPCredentialHandler(nil)
	req := httptest.NewRequest(http.MethodPut, "/api/sap/credentials", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestSAPConnectionHandlerUnauthorized(t *testing.T) {
	handler := TestSAPConnectionHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/sap/credentials/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestSAPConnectionHandlerMethodNotAllowed(t *testing.T) {
	handler := TestSAPConnectionHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/sap/credentials/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

// Confirma que o client_secret sobrevive a um ciclo de criptografia/decriptação
// via o mesmo mecanismo usado por SaveSAPCredentialHandler (EncryptField), e que
// o texto criptografado nunca é igual ao texto original (AC #2).
func TestClientSecretEncryptionRoundTrip(t *testing.T) {
	plaintext := "meu-client-secret-super-secreto"

	encrypted, err := crypto.EncryptField(plaintext)
	if err != nil {
		t.Fatalf("EncryptField falhou: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("valor criptografado é idêntico ao texto plano — client_secret não está sendo protegido")
	}

	decrypted, err := crypto.DecryptField(encrypted)
	if err != nil {
		t.Fatalf("DecryptField falhou: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("esperava %q após decriptar, obteve %q", plaintext, decrypted)
	}
}

// Confirma que duas chamadas a EncryptField com o mesmo texto plano produzem
// ciphertexts diferentes — evidência de que o nonce/IV é aleatório por chamada
// (AES-GCM com nonce fixo/reutilizado seria uma falha criptográfica grave).
func TestClientSecretEncryptionUsesRandomNonce(t *testing.T) {
	plaintext := "mesmo-segredo-duas-vezes"

	first, err := crypto.EncryptField(plaintext)
	if err != nil {
		t.Fatalf("EncryptField (1ª chamada) falhou: %v", err)
	}
	second, err := crypto.EncryptField(plaintext)
	if err != nil {
		t.Fatalf("EncryptField (2ª chamada) falhou: %v", err)
	}
	if first == second {
		t.Fatal("duas criptografias do mesmo texto produziram o mesmo ciphertext — nonce fixo ou reutilizado")
	}
}

package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqComXFF(xff, realIP, remote string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/rfb/webhook", bytes.NewReader([]byte(`{}`)))
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	if realIP != "" {
		r.Header.Set("X-Real-IP", realIP)
	}
	r.RemoteAddr = remote
	return r
}

func TestWebhookClientIP(t *testing.T) {
	casos := []struct {
		nome, xff, realIP, remote, want string
	}{
		{"cadeia Cloudflare->nginx: cliente + proxy interno", "189.9.61.23, 10.0.9.6", "", "10.0.10.5:4000", "189.9.61.23"},
		{"cliente forja entradas à esquerda", "189.9.61.23, 1.2.3.4, 10.0.9.6", "", "10.0.10.5:4000", "1.2.3.4"},
		{"só proxy interno no XFF cai no X-Real-IP", "10.0.9.6", "200.1.2.3", "10.0.10.5:4000", "200.1.2.3"},
		{"sem cabeçalhos usa RemoteAddr", "", "", "203.0.113.9:5555", "203.0.113.9"},
		{"CGNAT é interno", "198.51.100.7, 100.64.1.1", "", "10.0.10.5:4000", "198.51.100.7"},
		{"IPv6 público", "2001:db8::1, 10.0.9.6", "", "10.0.10.5:4000", "2001:db8::1"},
		{"lixo à esquerda não atrapalha", "nao-e-ip, 189.9.61.23", "", "10.0.10.5:4000", "189.9.61.23"},
		{"IPv4 mapeado em IPv6", "::ffff:189.9.61.23, 10.0.9.6", "", "10.0.10.5:4000", "189.9.61.23"},
		{"hop público extra (Traefik atrás do Cloudflare): vira o IP do hop", "189.9.61.23, 172.70.1.1, 10.0.9.6", "", "10.0.10.5:4000", "172.70.1.1"},
	}
	for _, c := range casos {
		got := webhookClientIP(reqComXFF(c.xff, c.realIP, c.remote))
		if got == nil || got.String() != c.want {
			t.Errorf("%s: esperava %s, veio %v", c.nome, c.want, got)
		}
	}
}

// Entrada ilegível à direita foi produzida pela nossa infra: nega, nunca "pula" para uma entrada
// à esquerda que o cliente poderia ter forjado.
func TestWebhookClientIP_EntradaIlegivelADireitaNega(t *testing.T) {
	for _, xff := range []string{"189.9.61.23, 1.2.3.4:5678", "189.9.61.23, unknown", "189.9.61.23, [2001:db8::1]:443"} {
		if got := webhookClientIP(reqComXFF(xff, "", "10.0.10.5:1")); got != nil {
			t.Errorf("XFF %q: esperava nil (negar), veio %v", xff, got)
		}
	}
}

func TestParseIPAllowlist(t *testing.T) {
	redes, inv := parseIPAllowlist(" 189.9.61.23 , 10.0.0.0/8, 2001:db8::/32,, lixo, 1.2.3.4/99 ")
	if len(redes) != 3 || len(inv) != 2 {
		t.Fatalf("esperava 3 redes e 2 inválidas, veio %d/%v", len(redes), inv)
	}
	if !redes[0].Contains(webhookClientIP(reqComXFF("189.9.61.23", "", "10.0.0.1:1"))) {
		t.Errorf("IP simples deve virar /32")
	}
}

func TestRFBWebhookOrigemPermitida(t *testing.T) {
	rfb := reqComXFF("189.9.61.23, 10.0.9.6", "", "10.0.10.5:1")
	outro := reqComXFF("1.2.3.4, 10.0.9.6", "", "10.0.10.5:1")

	t.Setenv("RFB_WEBHOOK_ALLOWED_IPS", "")
	if ok, _ := rfbWebhookOrigemPermitida(outro); !ok {
		t.Errorf("variável vazia = sem restrição")
	}
	t.Setenv("RFB_WEBHOOK_ALLOWED_IPS", "189.9.61.23/32")
	if ok, _ := rfbWebhookOrigemPermitida(rfb); !ok {
		t.Errorf("IP da RFB deve passar")
	}
	if ok, _ := rfbWebhookOrigemPermitida(outro); ok {
		t.Errorf("IP fora da lista deve ser recusado")
	}
	t.Setenv("RFB_WEBHOOK_ALLOWED_IPS", "189.9.61.2x, 200.0.0.1")
	if ok, _ := rfbWebhookOrigemPermitida(rfb); ok {
		t.Errorf("lista parcial: a entrada com typo é ignorada, então o IP da RFB não casa")
	}
	if ok, _ := rfbWebhookOrigemPermitida(reqComXFF("::ffff:200.0.0.1, 10.0.9.6", "", "10.0.10.5:1")); !ok {
		t.Errorf("IPv4 mapeado em IPv6 deve casar com entrada IPv4")
	}
	t.Setenv("RFB_WEBHOOK_ALLOWED_IPS", "lixo, 1.2.3.4/99")
	if ok, _ := rfbWebhookOrigemPermitida(rfb); ok {
		t.Errorf("lista sem nenhuma entrada válida deve negar tudo (fail-closed)")
	}
}

// Handler: 403 antes de ler corpo ou tocar no banco (db nil prova que nada foi acessado) e HEAD aberto.
func TestRFBWebhookHandler_AllowlistSoNoPOST(t *testing.T) {
	t.Setenv("RFB_WEBHOOK_SECRET", "")
	t.Setenv("RFB_WEBHOOK_ALLOWED_IPS", "189.9.61.23/32")
	h := RFBWebhookHandler(nil)

	rec := httptest.NewRecorder()
	h(rec, reqComXFF("1.2.3.4, 10.0.9.6", "", "10.0.10.5:1"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST fora da allowlist: esperava 403, veio %d", rec.Code)
	}

	head := reqComXFF("1.2.3.4, 10.0.9.6", "", "10.0.10.5:1")
	head.Method = http.MethodHead
	rec = httptest.NewRecorder()
	h(rec, head)
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD de qualquer origem deve responder 200, veio %d", rec.Code)
	}
}

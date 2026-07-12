package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// newFakeSAPServer sobe um servidor HTTP real (in-process, porta efêmera) que
// responde /token e o SearchPayments com sucesso — usado para exercitar o
// caminho feliz de ponta a ponta sem depender do sap-mock-server externo.
func newFakeSAPServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "fake-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DFeKeys []string `json:"dfeKeys"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		items := make([]map[string]interface{}, 0, len(req.DFeKeys))
		for _, k := range req.DFeKeys {
			items = append(items, map[string]interface{}{
				"dfeKey":        k,
				"paymentStatus": "EM_ABERTO",
				"matchType":     "CHAVE",
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func validKey(n int) string {
	// Gera uma chave de 44 dígitos determinística e distinta por n, satisfazendo
	// isValidDFeKey (44 ou 50 caracteres).
	s := strconv.Itoa(n)
	for len(s) < 44 {
		s = "0" + s
	}
	return s
}

func TestFilterValidKeys_FiltraChaveMalformadaSemInvalidarLote(t *testing.T) {
	chaves := []string{validKey(1), "curta-demais", validKey(2), ""}
	valid, invalidCount := filterValidKeys(chaves)
	if len(valid) != 2 {
		t.Errorf("esperava 2 chaves válidas, obteve %d: %v", len(valid), valid)
	}
	if invalidCount != 2 {
		t.Errorf("esperava 2 chaves inválidas contadas, obteve %d", invalidCount)
	}
}

func TestSplitIntoBatches_RespeitaLimiteDe500(t *testing.T) {
	chaves := make([]string, 1201)
	for i := range chaves {
		chaves[i] = validKey(i)
	}
	batches := splitIntoBatches(chaves, maxBatchSize)
	if len(batches) != 3 {
		t.Fatalf("esperava 3 lotes (500+500+201), obteve %d", len(batches))
	}
	if len(batches[0]) != 500 || len(batches[1]) != 500 || len(batches[2]) != 201 {
		t.Errorf("tamanhos de lote inesperados: %d, %d, %d", len(batches[0]), len(batches[1]), len(batches[2]))
	}
}

func TestCallPacer_PaceiaChamadasSucessivas(t *testing.T) {
	// Rate alto o suficiente para o teste ser rápido, mas com intervalo
	// mensurável (600/min = 100ms) para provar que o pacing realmente ocorre.
	pacer := newCallPacer(600)
	start := time.Now()
	pacer.Wait()
	pacer.Wait()
	pacer.Wait()
	elapsed := time.Since(start)
	if elapsed < 150*time.Millisecond {
		t.Errorf("esperava ao menos ~200ms entre 3 chamadas pareadas a 600/min (100ms cada), decorreu %v", elapsed)
	}
}

func TestProcessBatch_Retry429ComBackoff_EsgotaEmFalha(t *testing.T) {
	var attempts int32
	mux := http.NewServeMux()
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pacer := newCallPacer(6000)
	outcome := processBatch("fake-token", srv.URL, []string{validKey(1)}, pacer)

	if outcome.success || outcome.credFail {
		t.Errorf("esperava falha transitória (não credFail, não success), obteve %+v", outcome)
	}
	if got := atomic.LoadInt32(&attempts); got != maxRetries {
		t.Errorf("esperava exatamente %d tentativas (AC #7), obteve %d", maxRetries, got)
	}
}

func TestProcessBatch_401NaoTentaNovamente_FalhaCredencial(t *testing.T) {
	var attempts int32
	mux := http.NewServeMux()
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pacer := newCallPacer(6000)
	outcome := processBatch("fake-token", srv.URL, []string{validKey(1)}, pacer)

	if !outcome.credFail {
		t.Errorf("esperava credFail=true para HTTP 401, obteve %+v", outcome)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("AC #6 exige que 401/403 NÃO tente novamente — esperava 1 tentativa, obteve %d", got)
	}
}

func TestProcessBatch_403NaoTentaNovamente_FalhaCredencial(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pacer := newCallPacer(6000)
	outcome := processBatch("fake-token", srv.URL, []string{validKey(1)}, pacer)

	if !outcome.credFail {
		t.Errorf("esperava credFail=true para HTTP 403 (o sap-mock-server nunca gera 403 — coberto apenas aqui, ver Dev Notes), obteve %+v", outcome)
	}
}

func TestProcessBatch_400ReduzLoteAutomaticamente(t *testing.T) {
	var maxSeenBatchSize int32
	mux := http.NewServeMux()
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DFeKeys []string `json:"dfeKeys"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		for {
			cur := atomic.LoadInt32(&maxSeenBatchSize)
			if int32(len(req.DFeKeys)) <= cur || atomic.CompareAndSwapInt32(&maxSeenBatchSize, cur, int32(len(req.DFeKeys))) {
				break
			}
		}
		if len(req.DFeKeys) > 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"value": []interface{}{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pacer := newCallPacer(6000)
	batch := []string{validKey(1), validKey(2), validKey(3), validKey(4)}
	outcome := processBatch("fake-token", srv.URL, batch, pacer)

	if !outcome.success {
		t.Errorf("esperava sucesso após redução automática de lote (AC #4), obteve %+v", outcome)
	}
	if outcome.syncedCount != len(batch) {
		t.Errorf("esperava %d chaves sincronizadas no total após divisão, obteve %d", len(batch), outcome.syncedCount)
	}
}

func TestProcessBatch_400ComCredFailNaMetadeEsquerda_NaoChamaMetadeDireita(t *testing.T) {
	var rightHalfCalled int32
	mux := http.NewServeMux()
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DFeKeys []string `json:"dfeKeys"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case len(req.DFeKeys) == 4:
			w.WriteHeader(http.StatusBadRequest) // força a divisão em 2+2
		case len(req.DFeKeys) == 2 && req.DFeKeys[0] == validKey(1):
			w.WriteHeader(http.StatusUnauthorized) // metade esquerda: credencial inválida
		default:
			atomic.AddInt32(&rightHalfCalled, 1) // metade direita — NÃO deveria ser chamada
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"value": []interface{}{}})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pacer := newCallPacer(6000)
	batch := []string{validKey(1), validKey(2), validKey(3), validKey(4)}
	outcome := processBatch("fake-token", srv.URL, batch, pacer)

	if !outcome.credFail {
		t.Errorf("esperava credFail=true propagado da metade esquerda, obteve %+v", outcome)
	}
	if got := atomic.LoadInt32(&rightHalfCalled); got != 0 {
		t.Errorf("AC #6 exige que 401/403 não gere chamadas adicionais — metade direita foi chamada %d vez(es) após a esquerda já ter falhado por credencial", got)
	}
}

func TestValidateBaseURL_RejeitaSemChamarSAP(t *testing.T) {
	if err := validateBaseURL("http://exemplo.com"); err != nil {
		t.Errorf("esperava base_url válida aceita, obteve erro: %v", err)
	}
	for _, invalid := range []string{"", "sem-scheme-nem-host", "://falta-scheme"} {
		if err := validateBaseURL(invalid); err == nil {
			t.Errorf("esperava erro para base_url invalida %q, obteve nil", invalid)
		}
	}
}

func TestProcessSAPSync_BaseURLInvalida_FalhaSemChamarSAP(t *testing.T) {
	result := ProcessSAPSync(nil, "company-teste", "1000", "cid", "secret", "sem-scheme-nem-host", []string{validKey(1)})
	if result.Status != "falha" {
		t.Errorf("esperava status falha para base_url inválida (sem tentar nenhuma chamada), obteve %q", result.Status)
	}
}

func TestProcessSAPSync_TokenInvalido_FalhaCredencial(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := ProcessSAPSync(nil, "company-teste", "1000", "cid", "secret", srv.URL, []string{validKey(1)})
	if result.Status != "falha_credencial" {
		t.Errorf("esperava status falha_credencial quando GetToken falha, obteve %q (detail=%s)", result.Status, result.Detail)
	}
}

func TestProcessSAPSync_SemChavesValidas_Concluido(t *testing.T) {
	result := ProcessSAPSync(nil, "company-teste", "1000", "cid", "secret", "http://127.0.0.1:1", []string{"invalida", "tambem-invalida"})
	if result.Status != "concluido" {
		t.Errorf("esperava status concluido quando não há chaves válidas para sincronizar (nenhuma chamada ao SAP necessária), obteve %q", result.Status)
	}
}

func TestPacerForClient_IsolaOrcamentoPorClientID(t *testing.T) {
	p1 := pacerForClient("client-A-" + validKey(1))
	p2 := pacerForClient("client-B-" + validKey(2))
	if p1 == p2 {
		t.Error("esperava pacers distintos para client_id distintos (rate limit é por consumidor/credencial)")
	}
	p1Again := pacerForClient("client-A-" + validKey(1))
	if p1 != p1Again {
		t.Error("esperava o mesmo pacer para o mesmo client_id em chamadas subsequentes")
	}
	p3 := pacerForClient("client-A-" + validKey(1))
	if p3 != p1 {
		t.Error("duas empresas com o mesmo client_id devem compartilhar o mesmo orçamento de rate limit")
	}
}

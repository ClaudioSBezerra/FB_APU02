package services

import (
	"os"
	"testing"
	"time"
)

// ─── ResolveModoCalendario ─────────────────────────────────────────────────────

func TestResolveModoCalendario_PadraoPiloto(t *testing.T) {
	os.Unsetenv("APURACAO_CALENDARIO_MODO")
	if got := ResolveModoCalendario(); got != string(ModoCalendarioPiloto) {
		t.Errorf("esperava %q com env ausente, obteve %q", ModoCalendarioPiloto, got)
	}
}

func TestResolveModoCalendario_ValorInvalido_CaiParaPiloto(t *testing.T) {
	os.Setenv("APURACAO_CALENDARIO_MODO", "qualquer-coisa")
	defer os.Unsetenv("APURACAO_CALENDARIO_MODO")
	if got := ResolveModoCalendario(); got != string(ModoCalendarioPiloto) {
		t.Errorf("esperava %q com valor inválido, obteve %q", ModoCalendarioPiloto, got)
	}
}

func TestResolveModoCalendario_Real(t *testing.T) {
	os.Setenv("APURACAO_CALENDARIO_MODO", "real")
	defer os.Unsetenv("APURACAO_CALENDARIO_MODO")
	if got := ResolveModoCalendario(); got != string(ModoCalendarioReal) {
		t.Errorf("esperava %q, obteve %q", ModoCalendarioReal, got)
	}
	// Case-insensitive.
	os.Setenv("APURACAO_CALENDARIO_MODO", "REAL")
	if got := ResolveModoCalendario(); got != string(ModoCalendarioReal) {
		t.Errorf("esperava %q (case-insensitive), obteve %q", ModoCalendarioReal, got)
	}
}

// ─── dataFechamento: modo piloto (dia 26 fixo) ─────────────────────────────────

func TestStatusPeriodoApuracao_Piloto_DataFechamentoDia26(t *testing.T) {
	// pa = qualquer dia de janeiro/2026 — dataFechamento deve ser sempre
	// 26/02/2026 00:00 America/Sao_Paulo, independente do dia exato de pa dentro
	// do mês de referência (normalização para o dia 1).
	pa := time.Date(2026, 1, 15, 23, 59, 0, 0, time.UTC)
	_, dataFechamento := StatusPeriodoApuracao(pa, string(ModoCalendarioPiloto))

	esperado := time.Date(2026, 2, 26, 0, 0, 0, 0, saoPauloLoc)
	if !dataFechamento.Equal(esperado) {
		t.Errorf("esperava dataFechamento=%v, obteve %v", esperado, dataFechamento)
	}
}

// ─── dataFechamento: modo real, último dia útil pulando fim de semana ─────────

func TestStatusPeriodoApuracao_Real_UltimoDiaUtil_PulaFimDeSemana(t *testing.T) {
	// pa = qualquer dia de janeiro/2026 -> mês de referência seguinte é
	// fevereiro/2026, que termina num sábado (28/02/2026). O último dia útil
	// correto é sexta-feira 27/02/2026, não o dia 28 (sábado) nem 26 (fixo do
	// modo piloto).
	if wd := time.Date(2026, 2, 28, 0, 0, 0, 0, saoPauloLoc).Weekday(); wd != time.Saturday {
		t.Fatalf("pré-condição do teste falhou: 28/02/2026 deveria ser sábado, é %v", wd)
	}

	pa := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	_, dataFechamento := StatusPeriodoApuracao(pa, string(ModoCalendarioReal))

	esperado := time.Date(2026, 2, 27, 0, 0, 0, 0, saoPauloLoc)
	if !dataFechamento.Equal(esperado) {
		t.Errorf("esperava dataFechamento=%v (sexta-feira, pulando o sábado 28/02), obteve %v", esperado, dataFechamento)
	}
}

// ─── Predicados de dia útil/feriado (cobertura direta, sem depender de um mês
//     real cujo último dia colida com um feriado fixo — não existe nenhum entre
//     2026-2030, verificado manualmente) ───────────────────────────────────────

func TestEhFeriadoFixo_RelacaoCompleta(t *testing.T) {
	casos := []struct {
		data    time.Time
		feriado bool
	}{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 4, 21, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 5, 1, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 9, 7, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 10, 12, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 11, 2, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 11, 15, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 12, 25, 0, 0, 0, 0, saoPauloLoc), true},
		{time.Date(2026, 3, 10, 0, 0, 0, 0, saoPauloLoc), false},
		{time.Date(2026, 12, 24, 0, 0, 0, 0, saoPauloLoc), false},
	}
	for _, c := range casos {
		if got := ehFeriadoFixo(c.data); got != c.feriado {
			t.Errorf("ehFeriadoFixo(%v) = %v, esperava %v", c.data.Format("2006-01-02"), got, c.feriado)
		}
	}
}

func TestEhDiaUtil_FeriadoFixoEmDiaDeSemana_NaoEhUtil(t *testing.T) {
	// 07/09/2026 (Independência) é uma segunda-feira — não é fim de semana, mas
	// ainda assim não deve ser considerado dia útil.
	natal := time.Date(2026, 9, 7, 0, 0, 0, 0, saoPauloLoc)
	if wd := natal.Weekday(); wd == time.Saturday || wd == time.Sunday {
		t.Fatalf("pré-condição do teste falhou: 07/09/2026 não deveria cair num fim de semana, é %v", wd)
	}
	if ehDiaUtil(natal) {
		t.Errorf("esperava ehDiaUtil=false para feriado fixo em dia de semana, obteve true")
	}
}

// ─── Status: 3 transições (em_andamento / em_ajuste / fechado) ────────────────
//
// Estes testes usam time.Now() como pivô (a assinatura de StatusPeriodoApuracao
// não aceita um "now" injetado) — pa é construído relativo ao mês corrente para
// que o teste continue válido em qualquer dia de execução, EXCETO o caso
// "em_ajuste", que assume que hoje cai nos primeiros ~25 dias do mês corrente
// (true na data de criação deste teste, 2026-10-05). Se este teste passar a
// falhar por ter sido executado entre os dias 26-31 de algum mês, é esse
// pressuposto que quebrou, não a lógica de StatusPeriodoApuracao.

func TestStatusPeriodoApuracao_EmAndamento_MesDeReferenciaCorrente(t *testing.T) {
	agora := time.Now().In(saoPauloLoc)
	pa := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, saoPauloLoc)

	for _, modo := range []string{string(ModoCalendarioPiloto), string(ModoCalendarioReal)} {
		status, _ := StatusPeriodoApuracao(pa, modo)
		if status != "em_andamento" {
			t.Errorf("modo=%s: esperava status=em_andamento para o mês corrente, obteve %q", modo, status)
		}
	}
}

func TestStatusPeriodoApuracao_EmAjuste_MesAnterior(t *testing.T) {
	agora := time.Now().In(saoPauloLoc)
	if agora.Day() > 25 {
		t.Skip("pressuposto do teste (hoje está antes do dia 26) não vale nesta execução")
	}
	paMesAnterior := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, saoPauloLoc).AddDate(0, -1, 0)

	for _, modo := range []string{string(ModoCalendarioPiloto), string(ModoCalendarioReal)} {
		status, dataFechamento := StatusPeriodoApuracao(paMesAnterior, modo)
		if status != "em_ajuste" {
			t.Errorf("modo=%s: esperava status=em_ajuste para o mês anterior (hoje dia %d), obteve %q (dataFechamento=%v)", modo, agora.Day(), status, dataFechamento)
		}
	}
}

func TestStatusPeriodoApuracao_Fechado_DoisMesesAtras(t *testing.T) {
	agora := time.Now().In(saoPauloLoc)
	paDoisMesesAtras := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, saoPauloLoc).AddDate(0, -2, 0)

	for _, modo := range []string{string(ModoCalendarioPiloto), string(ModoCalendarioReal)} {
		status, dataFechamento := StatusPeriodoApuracao(paDoisMesesAtras, modo)
		if status != "fechado" {
			t.Errorf("modo=%s: esperava status=fechado para período de dois meses atrás, obteve %q (dataFechamento=%v, agora=%v)", modo, status, dataFechamento, agora)
		}
	}
}

// TestStatusComLimite_Real_UltimoDiaUtilAindaEmAjuste confirma o patch da revisão adversarial
// (StatusPeriodoApuracao fechava o período às 00:00 do próprio último dia útil no modo real,
// cortando o prazo que a lei garante por inteiro nesse dia) — testa a função pura extraída
// `statusComLimite`, determinística (recebe `now` explícito, sem depender de time.Now()).
func TestStatusComLimite_Real_UltimoDiaUtilAindaEmAjuste(t *testing.T) {
	proximoMes := time.Date(2026, 2, 1, 0, 0, 0, 0, saoPauloLoc)
	dataFechamento := time.Date(2026, 2, 27, 0, 0, 0, 0, saoPauloLoc) // sexta, último dia útil

	casos := []struct {
		nome string
		now  time.Time
		want string
	}{
		{"início do dia do fechamento ainda é ajuste", time.Date(2026, 2, 27, 0, 0, 0, 0, saoPauloLoc), "em_ajuste"},
		{"fim do dia do fechamento ainda é ajuste", time.Date(2026, 2, 27, 23, 59, 59, 0, saoPauloLoc), "em_ajuste"},
		{"dia seguinte já fechado", time.Date(2026, 2, 28, 0, 0, 0, 0, saoPauloLoc), "fechado"},
	}
	for _, c := range casos {
		got := statusComLimite(c.now, proximoMes, dataFechamento, false)
		if got != c.want {
			t.Errorf("%s: esperava %q, obteve %q", c.nome, c.want, got)
		}
	}
}

// TestStatusComLimite_Piloto_FechaNoInicioDoDia26 confirma que o modo piloto continua
// fechando a partir de 00:00 do dia 26 (comportamento pré-existente, "Concluída a partir de
// 26" — não deve regredir com o patch do modo real).
func TestStatusComLimite_Piloto_FechaNoInicioDoDia26(t *testing.T) {
	proximoMes := time.Date(2026, 2, 1, 0, 0, 0, 0, saoPauloLoc)
	dataFechamento := time.Date(2026, 2, 26, 0, 0, 0, 0, saoPauloLoc)

	if got := statusComLimite(dataFechamento, proximoMes, dataFechamento, true); got != "fechado" {
		t.Errorf("piloto: esperava fechado já às 00:00 do dia 26, obteve %q", got)
	}
	antes := dataFechamento.Add(-time.Second)
	if got := statusComLimite(antes, proximoMes, dataFechamento, true); got != "em_ajuste" {
		t.Errorf("piloto: esperava em_ajuste 1s antes do dia 26, obteve %q", got)
	}
}

func TestStatusPeriodoApuracao_ModoDesconhecido_TrataComoPiloto(t *testing.T) {
	pa := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	_, dfPiloto := StatusPeriodoApuracao(pa, string(ModoCalendarioPiloto))
	_, dfDesconhecido := StatusPeriodoApuracao(pa, "modo-que-nao-existe")
	if !dfPiloto.Equal(dfDesconhecido) {
		t.Errorf("esperava modo desconhecido tratado como piloto (dataFechamento=%v), obteve %v", dfPiloto, dfDesconhecido)
	}
}

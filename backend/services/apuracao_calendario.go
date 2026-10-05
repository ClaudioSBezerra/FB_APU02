package services

import (
	"os"
	"strings"
	"time"
)

// ─── Calendário de Apuração Mensal CBS/IBS ────────────────────────────────────
//
// Módulo compartilhado (IBS via ResumoCGIBSHandler, CBS via resumo mensal RFB) que
// resolve o status de um período de apuração mensal e sua data de fechamento, nos
// dois modos de calendário vigentes:
//
//   - "piloto": ano de teste 2026, sem efeito tributário real — datas de fechamento
//     fixas (dia 26 do mês seguinte), conforme os manuais RFB/CGIBS ("Período de
//     Ajuste 01-25, Concluída a partir de 26").
//   - "real": produção a partir de 2027 — fechamento no último dia útil do mês
//     seguinte (pulando fins de semana e feriados nacionais de data fixa).
//
// LIMITAÇÃO CONHECIDA/DEFERIDA: feriados móveis (Carnaval, Sexta-feira Santa,
// Corpus Christi) e feriados estaduais/municipais do domicílio tributário do
// contribuinte NÃO são calculados nesta rodada — só os 8 feriados nacionais de data
// fixa abaixo. Revisitar se o negócio precisar de precisão maior no modo "real".

// ModoCalendario distingue os dois calendários suportados.
type ModoCalendario string

const (
	ModoCalendarioPiloto ModoCalendario = "piloto"
	ModoCalendarioReal   ModoCalendario = "real"
)

// saoPauloLoc é carregado uma vez — todo o cálculo de calendário de apuração roda no
// fuso America/Sao_Paulo (mesmo fuso do container, ver Dockerfile/tzdata).
var saoPauloLoc = loadSaoPauloLocation()

func loadSaoPauloLocation() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		// Fallback defensivo — não deveria acontecer num container com tzdata instalado,
		// mas evita panic em ambiente sem a base de timezone (ex.: alguns `go test` locais).
		return time.FixedZone("America/Sao_Paulo", -3*60*60)
	}
	return loc
}

// ResolveModoCalendario lê a env APURACAO_CALENDARIO_MODO e resolve o modo de calendário
// efetivo. Qualquer valor vazio, ausente ou não reconhecido cai no default "piloto" — 2026
// é o ano piloto do CBS/IBS, sem efeito tributário real.
func ResolveModoCalendario() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("APURACAO_CALENDARIO_MODO")))
	if v == string(ModoCalendarioReal) {
		return string(ModoCalendarioReal)
	}
	return string(ModoCalendarioPiloto)
}

// feriadosFixosNacionais são os feriados nacionais de data fixa (mês, dia) considerados
// no cálculo do último dia útil do modo "real". Ver limitação documentada acima.
var feriadosFixosNacionais = [][2]int{
	{1, 1},   // Confraternização Universal
	{4, 21},  // Tiradentes
	{5, 1},   // Dia do Trabalho
	{9, 7},   // Independência do Brasil
	{10, 12}, // Nossa Senhora Aparecida
	{11, 2},  // Finados
	{11, 15}, // Proclamação da República
	{12, 25}, // Natal
}

// ehFeriadoFixo reporta se a data (mês/dia, ano ignorado) é um dos feriados nacionais
// fixos listados acima.
func ehFeriadoFixo(d time.Time) bool {
	mes, dia := int(d.Month()), d.Day()
	for _, f := range feriadosFixosNacionais {
		if f[0] == mes && f[1] == dia {
			return true
		}
	}
	return false
}

// ehDiaUtil reporta se a data não é fim de semana nem feriado fixo nacional.
func ehDiaUtil(d time.Time) bool {
	if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return !ehFeriadoFixo(d)
}

// ultimoDiaUtilDoMes retorna o último dia útil do mês informado, às 00:00 em
// America/Sao_Paulo — parte do último dia calendário do mês e anda para trás
// enquanto o dia não for útil (fim de semana ou feriado fixo).
func ultimoDiaUtilDoMes(ano int, mes time.Month) time.Time {
	d := time.Date(ano, mes+1, 1, 0, 0, 0, 0, saoPauloLoc).AddDate(0, 0, -1)
	for !ehDiaUtil(d) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// StatusPeriodoApuracao calcula o status do período de apuração mensal de referência
// `pa` (qualquer data dentro desse mês — normalizada aqui para o dia 1, em
// America/Sao_Paulo) e sua data de fechamento, segundo o modo de calendário informado
// (ModoCalendarioPiloto/ModoCalendarioReal — qualquer outro valor é tratado como
// piloto, mesmo default de ResolveModoCalendario). Mesma função é chamada tanto com
// `data_fim` do range pedido no resumo IBS (ResumoCGIBSHandler) quanto com o `periodo`
// parseado no resumo mensal CBS (rfb_resumo_mensal.go).
//
// Transições:
//   - "em_andamento": ainda estamos dentro do mês de referência (ou antes dele) —
//     documentos ainda podem ser emitidos/incluídos.
//   - "em_ajuste": estamos no mês seguinte ao de referência, antes de dataFechamento —
//     Período de Ajuste, o saldo ainda pode mudar.
//   - "fechado": já passamos de dataFechamento — saldo definitivo.
func StatusPeriodoApuracao(pa time.Time, modo string) (status string, dataFechamento time.Time) {
	mesRef := time.Date(pa.Year(), pa.Month(), 1, 0, 0, 0, 0, saoPauloLoc)
	proximoMes := mesRef.AddDate(0, 1, 0)

	// fechaNoInicioDoDia distingue a semântica de cada modo: no piloto, a própria fonte
	// (manuais RFB/CGIBS) diz "Concluída A PARTIR de 26" — o dia 26 já conta como fechado
	// desde o início. No modo real, a lei garante o PRAZO INTEIRO do último dia útil para
	// ajuste/pagamento — esse dia só vira "fechado" a partir do dia seguinte, 00:00
	// (revisão adversarial: tratar os dois modos com a mesma comparação fazia o modo real
	// encerrar o período already às 00:00 do próprio último dia útil, cortando o prazo).
	var fechaNoInicioDoDia bool
	if modo == string(ModoCalendarioReal) {
		dataFechamento = ultimoDiaUtilDoMes(proximoMes.Year(), proximoMes.Month())
		fechaNoInicioDoDia = false
	} else {
		// piloto (default): dia 26 do mês seguinte, 00:00 — "Período de Ajuste 01-25,
		// Concluída a partir de 26" (manuais RFB/CGIBS, ano piloto 2026).
		dataFechamento = time.Date(proximoMes.Year(), proximoMes.Month(), 26, 0, 0, 0, 0, saoPauloLoc)
		fechaNoInicioDoDia = true
	}

	status = statusComLimite(time.Now().In(saoPauloLoc), proximoMes, dataFechamento, fechaNoInicioDoDia)
	return status, dataFechamento
}

// statusComLimite é a parte pura (determinística, sem time.Now()) de StatusPeriodoApuracao —
// extraída só para permitir teste de unidade exato da fronteira de fechamento, sem depender do
// relógio real. `fechaNoInicioDoDia` espelha a semântica de cada modo (ver comentário acima):
// true = dataFechamento já é o primeiro instante fechado (piloto); false = dataFechamento é o
// último dia ainda aberto por inteiro, só fecha no dia seguinte (real).
func statusComLimite(now, proximoMes, dataFechamento time.Time, fechaNoInicioDoDia bool) string {
	limiteFechamento := dataFechamento
	if !fechaNoInicioDoDia {
		limiteFechamento = dataFechamento.AddDate(0, 0, 1)
	}
	switch {
	case now.Before(proximoMes):
		return "em_andamento"
	case now.Before(limiteFechamento):
		return "em_ajuste"
	default:
		return "fechado"
	}
}

package services

import (
	"crypto/tls"
	"fmt"
	"html"
	"log"
	"math"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

// TaxComparisonData holds all structured fiscal data for the email (mirrors the screen).
type TaxComparisonData struct {
	// Impostos (campos originais)
	IcmsAPagar   float64
	IbsProjetado float64
	CbsProjetado float64
	// Dados do período
	FaturamentoBruto float64
	TotalEntradas    float64
	IcmsSaida        float64
	IcmsEntrada      float64
	// Alíquotas efetivas (%)
	AliquotaEfetivaICMS         float64
	AliquotaEfetivaIBS          float64
	AliquotaEfetivaCBS          float64
	AliquotaEfetivaTotalReforma float64
	// Comparativo período anterior
	PeriodoAnterior             string
	FaturamentoAnterior         float64
	IcmsAPagarAnterior          float64
	AliquotaEfetivaICMSAnterior float64
	// Créditos IBS+CBS em risco (NF-e sem crédito + Simples Nacional)
	CreditosEmRiscoTotal    float64
	CreditosNFeSemIBS       float64 // estimado sobre NF-e com v_ibs=0 e v_cbs=0
	CreditosSimplesNacional float64 // estimado sobre fornecedores do Simples
}

// EmailConfig holds SMTP configuration
type EmailConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// GetEmailConfig returns SMTP configuration from environment or defaults
func GetEmailConfig() *EmailConfig {
	portStr := os.Getenv("SMTP_PORT")
	port := 465
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	host := os.Getenv("SMTP_HOST")
	if host == "" {
		host = "smtp.hostinger.com"
	}

	username := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")

	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "FBTax Cloud <noreply@fbtax.cloud>"
	}

	return &EmailConfig{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		From:     from,
	}
}

// sendMailSSL sends email over implicit TLS (port 465)
func sendMailSSL(config *EmailConfig, to []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)

	tlsConfig := &tls.Config{
		ServerName: config.Host,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, config.Host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer client.Quit()

	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP auth failed: %w", err)
	}

	if err = client.Mail(config.Username); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}

	for _, recipient := range to {
		if err = client.Rcpt(recipient); err != nil {
			return fmt.Errorf("SMTP RCPT TO failed for %s: %w", recipient, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}

	_, err = writer.Write(msg)
	if err != nil {
		return fmt.Errorf("SMTP write failed: %w", err)
	}

	return writer.Close()
}

// SendPasswordResetEmail sends a password reset email to the user
func SendPasswordResetEmail(email, resetToken string) error {
	config := GetEmailConfig()

	if config.Password == "" {
		log.Printf("[Email Service] SMTP not configured. Skipping email send to %s", email)
		return fmt.Errorf("serviço de e-mail não configurado - configure SMTP_PASSWORD")
	}

	// Use APP_URL env var for the reset link (defaults to production)
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "https://fbtax.cloud"
	}
	resetLink := fmt.Sprintf("%s/reset-senha?token=%s", appURL, resetToken)

	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: FBTax Cloud - =?UTF-8?B?UmVkZWZpbmnDp8OjbyBkZSBTZW5oYQ==?=\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n"+
		`<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333333; max-width: 600px; margin: 0 auto; }
		.container { background-color: #f4f4f8; padding: 40px; border-radius: 8px; }
		.header { background: #4a5568; color: white; padding: 20px; border-radius: 8px; text-align: center; }
		.logo { font-size: 24px; font-weight: bold; }
		.content { background: white; padding: 30px; border-radius: 8px; }
		h1 { color: #333; margin-bottom: 20px; }
		p { margin: 0 0 15px 0; color: #666; line-height: 1.8; }
		.button { display: inline-block; padding: 12px 24px; background: #2c3e50; color: white; text-decoration: none; border-radius: 4px; font-weight: bold; }
		.footer { background: #f8f9fa; padding: 20px; border-radius: 8px; color: #666; font-size: 12px; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<div class="logo">FBTax Cloud</div>
			<h1 style="color: white;">Redefinição de Senha</h1>
		</div>
		<div class="content">
			<p>Olá,</p>
			<p>Recebemos uma solicitação de redefinição de senha para sua conta no FBTax Cloud.</p>
			<p>Se você não solicitou esta alteração, por favor ignore este e-mail.</p>
			<div style="text-align: center; margin: 30px 0;">
				<a href="%s" class="button">Redefinir Minha Senha</a>
			</div>
			<p style="margin: 30px 0; font-size: 14px; color: #666;">
				Ou copie e cole o link no seu navegador:<br>
				<strong style="color: #2c3e50;">%s</strong>
			</p>
			<p style="font-size: 12px; color: #999;">Este link expira em 1 hora por motivos de segurança.</p>
			<p style="font-size: 12px; color: #999;">Se você não solicitou esta redefinição, entre em contato com o suporte.</p>
		</div>
		<div class="footer">
			<p>&copy; 2026 FBTax Cloud - Todos os direitos reservados</p>
		</div>
	</div>
</body>
</html>
`, config.From, email, resetLink, resetLink)

	log.Printf("[Email Service] Sending password reset email to %s via %s:%d", email, config.Host, config.Port)

	var err error
	if config.Port == 465 {
		err = sendMailSSL(config, []string{email}, []byte(message))
	} else {
		addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
		auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
		err = smtp.SendMail(addr, auth, config.Username, []string{email}, []byte(message))
	}

	if err != nil {
		log.Printf("[Email Service] Failed to send email to %s: %v", email, err)
		return fmt.Errorf("falha ao enviar e-mail: %w", err)
	}

	log.Printf("[Email Service] Password reset email sent successfully to %s", email)
	return nil
}

// SendAIReportEmail sends AI-generated executive summary to company managers.
// The email mirrors exactly what is displayed on screen: structured KPI data first,
// AI narrative (commentary) at the bottom.
func SendAIReportEmail(recipients []string, companyName, periodo, narrativaMarkdown, dadosBrutosJSON string, taxData TaxComparisonData) error {
	// Escape user-controlled strings before use in HTML context
	companyName = html.EscapeString(companyName)
	periodo = html.EscapeString(periodo)
	config := GetEmailConfig()

	if config.Password == "" {
		log.Printf("[Email Service] SMTP not configured. Skipping AI report email to %d recipients", len(recipients))
		return fmt.Errorf("servico de e-mail nao configurado - configure SMTP_PASSWORD")
	}

	if len(recipients) == 0 {
		log.Printf("[Email Service] No recipients for AI report email")
		return nil
	}

	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:3000"
	}

	narrativaHTML := convertMarkdownToHTML(narrativaMarkdown)
	narrativaPlain := stripHTMLTags(narrativaHTML)

	// Build structured data sections (mirrors the screen)
	kpiHTML := generateKPISectionHTML(taxData)
	reformaHTML := generateReformaHTML(taxData)
	cargaHTML := generateCargaTributariaHTML(taxData)
	comparativoHTML := generateComparativoHTML(taxData, periodo)
	creditosHTML := generateCreditosEmRiscoHTML(taxData, appURL)

	// Plain text summary
	plainText := buildPlainTextSummary(companyName, periodo, taxData, narrativaPlain, appURL)

	for _, email := range recipients {
		boundary := fmt.Sprintf("boundary_%d", time.Now().UnixNano())

		message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: FBTax Cloud - Resumo Executivo - %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n",
			config.From, email, periodo, boundary)

		// Plain text part
		message += fmt.Sprintf("--%s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n--%s\r\n",
			boundary, plainText, boundary)

		// HTML part
		message += "Content-Type: text/html; charset=UTF-8\r\n\r\n"
		message += fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
body{font-family:Arial,sans-serif;line-height:1.6;color:#333;max-width:640px;margin:0 auto;background:#f4f4f8}
.wrap{padding:24px}
.hdr{background:#2d3748;color:#fff;padding:20px 24px;border-radius:8px 8px 0 0;text-align:center}
.hdr-logo{font-size:22px;font-weight:700;letter-spacing:.5px}
.hdr-sub{font-size:14px;color:#cbd5e0;margin-top:4px}
.body{background:#fff;padding:24px;border-radius:0 0 8px 8px}
.info-box{background:#ebf8ff;border-left:4px solid #3182ce;padding:12px 16px;margin:0 0 20px;border-radius:0 6px 6px 0;font-size:13px;color:#2c5282}
.sec{margin:20px 0}
.sec-title{font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:.06em;color:#718096;border-bottom:2px solid #e2e8f0;padding-bottom:6px;margin-bottom:14px}
.kpi-table{width:100%%;border-collapse:separate;border-spacing:8px 8px}
.kpi-cell{border-radius:8px;padding:14px 12px;text-align:center;vertical-align:top}
.kpi-label{font-size:10px;text-transform:uppercase;letter-spacing:.08em;margin-bottom:4px}
.kpi-val{font-size:19px;font-weight:700;margin:2px 0}
.kpi-sub{font-size:10px;margin-top:2px}
.data-table{width:100%%;border-collapse:collapse;font-size:13px;margin:8px 0}
.data-table th{background:#4a5568;color:#fff;padding:8px 12px;text-align:left;font-size:12px}
.data-table td{padding:8px 12px;border-bottom:1px solid #e2e8f0}
.data-table tr:last-child td{border-bottom:none;font-weight:700;background:#edf2f7}
.ai-box{background:#f7fafc;border:1px solid #e2e8f0;border-radius:8px;padding:20px;margin:20px 0}
.ai-label{font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.06em;color:#a0aec0;margin-bottom:12px}
.btn{display:inline-block;padding:12px 28px;background:#2d3748;color:#fff;text-decoration:none;border-radius:6px;font-weight:700;font-size:14px;margin:8px 0}
.footer{text-align:center;padding:16px;color:#a0aec0;font-size:11px;margin-top:8px}
</style>
</head>
<body>
<div class="wrap">
<div class="hdr">
  <div class="hdr-logo">FBTax Cloud</div>
  <div class="hdr-sub">Resumo Executivo &mdash; %s</div>
</div>
<div class="body">
  <div class="info-box">
    <strong>Empresa:</strong> %s &nbsp;|&nbsp; <strong>Per&iacute;odo:</strong> %s &nbsp;|&nbsp; <strong>Gerado em:</strong> %s
  </div>
  %s
  %s
  %s
  %s
  %s
  <div class="ai-box">
    <div class="ai-label">&#129302; An&aacute;lise da Intelig&ecirc;ncia Artificial</div>
    %s
  </div>
  <div style="text-align:center;margin:24px 0">
    <a href="%s/relatorios/resumo-executivo" class="btn">Acessar Painel Completo</a>
  </div>
</div>
<div class="footer">&copy; 2026 FBTax Cloud &mdash; Todos os direitos reservados</div>
</div>
</body>
</html>`,
			periodo,
			companyName, periodo, getTimeBrasil(),
			kpiHTML, reformaHTML, cargaHTML, comparativoHTML, creditosHTML,
			narrativaHTML,
			appURL)

		message += fmt.Sprintf("\r\n--%s--\r\n", boundary)

		log.Printf("[Email Service] Sending AI report email to %s via %s:%d", email, config.Host, config.Port)

		var err error
		if config.Port == 465 {
			err = sendMailSSL(config, []string{email}, []byte(message))
		} else {
			addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
			auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
			err = smtp.SendMail(addr, auth, config.Username, []string{email}, []byte(message))
		}

		if err != nil {
			log.Printf("[Email Service] Failed to send AI report email to %s: %v", email, err)
			return fmt.Errorf("falha ao enviar e-mail de relatorio IA: %w", err)
		}
		log.Printf("[Email Service] AI report email sent successfully to %s", email)
	}

	return nil
}

// generateKPISectionHTML renders the top KPI cards (faturamento, ICMS, entradas).
func generateKPISectionHTML(d TaxComparisonData) string {
	var sb strings.Builder
	sb.WriteString(`<div class="sec"><div class="sec-title">Dados do Per&iacute;odo</div>`)
	sb.WriteString(`<table class="kpi-table"><tr>`)

	// Faturamento Bruto
	sb.WriteString(fmt.Sprintf(`<td class="kpi-cell" style="background:#ebf8ff;border:1px solid #bee3f8">
  <div class="kpi-label" style="color:#2b6cb0">Faturamento Bruto</div>
  <div class="kpi-val" style="color:#1a365d">R$ %s</div>
  <div class="kpi-sub" style="color:#718096">Total de Sa&iacute;das</div>
</td>`, formatEmailBRL(d.FaturamentoBruto)))

	// Total Entradas
	sb.WriteString(fmt.Sprintf(`<td class="kpi-cell" style="background:#f0fff4;border:1px solid #9ae6b4">
  <div class="kpi-label" style="color:#276749">Total de Entradas</div>
  <div class="kpi-val" style="color:#1c4532">R$ %s</div>
  <div class="kpi-sub" style="color:#718096">Compras e insumos</div>
</td>`, formatEmailBRL(d.TotalEntradas)))

	sb.WriteString(`</tr></table>`)

	// ICMS breakdown
	sb.WriteString(`<table class="data-table" style="margin-top:12px">`)
	sb.WriteString(`<thead><tr><th>ICMS do Per&iacute;odo</th><th style="text-align:right">Valor</th></tr></thead><tbody>`)
	sb.WriteString(fmt.Sprintf(`<tr><td>D&eacute;bito (sobre sa&iacute;das)</td><td style="text-align:right">R$ %s</td></tr>`, formatEmailBRL(d.IcmsSaida)))
	sb.WriteString(fmt.Sprintf(`<tr><td>Cr&eacute;dito (sobre entradas)</td><td style="text-align:right">- R$ %s</td></tr>`, formatEmailBRL(d.IcmsEntrada)))
	sb.WriteString(fmt.Sprintf(`<tr><td>ICMS a Recolher</td><td style="text-align:right">R$ %s</td></tr>`, formatEmailBRL(d.IcmsAPagar)))
	sb.WriteString(`</tbody></table></div>`)
	return sb.String()
}

// generateReformaHTML renders the Reforma Tributária section as a pure HTML table
// (SVG is not supported by most email clients and produces garbled text).
func generateReformaHTML(d TaxComparisonData) string {
	ibsCbsTotal := d.IbsProjetado + d.CbsProjetado

	// Compute bar widths (relative to the largest value)
	maxVal := d.IcmsAPagar
	if d.IbsProjetado > maxVal { maxVal = d.IbsProjetado }
	if d.CbsProjetado > maxVal { maxVal = d.CbsProjetado }
	if ibsCbsTotal > maxVal { maxVal = ibsCbsTotal }
	if maxVal == 0 { maxVal = 1 }
	barPct := func(v float64) int {
		p := int(v / maxVal * 100)
		if v > 0 && p < 3 { return 3 }
		return p
	}

	var sb strings.Builder
	sb.WriteString(`<div class="sec"><div class="sec-title">Reforma Tribut&aacute;ria &mdash; Proje&ccedil;&atilde;o 2033</div>`)

	// HTML bar chart (table-based, works in all email clients)
	type barRow struct { label, color, tipo string; val float64 }
	rows := []barRow{
		{"ICMS (atual)",   "#3B82F6", "Regime atual",              d.IcmsAPagar},
		{"IBS Projetado",  "#10B981", "Novo imposto (2033)",       d.IbsProjetado},
		{"CBS Projetado",  "#F59E0B", "Novo imposto (2033)",       d.CbsProjetado},
		{"IBS + CBS",      "#8B5CF6", "Substituir&aacute; ICMS+PIS/COFINS", ibsCbsTotal},
	}
	sb.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" style="margin:12px 0 16px">`)
	for _, r := range rows {
		pct := barPct(r.val)
		sb.WriteString(fmt.Sprintf(`<tr>
  <td width="90" style="font-size:12px;font-weight:700;color:#4a5568;padding:4px 8px 10px 0;vertical-align:middle">%s</td>
  <td style="padding:4px 0 10px;vertical-align:middle">
    <table width="100%%" cellpadding="0" cellspacing="0"><tr>
      <td width="%d%%" bgcolor="%s" height="22" style="border-radius:4px">&nbsp;</td>
      <td style="padding-left:8px;font-size:12px;white-space:nowrap;color:#2d3748;font-weight:600">R$ %s</td>
    </tr></table>
    <div style="font-size:10px;color:#a0aec0;margin-top:2px">%s</div>
  </td>
</tr>`, r.label, pct, r.color, formatEmailBRL(r.val), r.tipo))
	}
	sb.WriteString(`</table></div>`)
	return sb.String()
}

// generateCargaTributariaHTML renders the effective tax rate section.
func generateCargaTributariaHTML(d TaxComparisonData) string {
	if d.FaturamentoBruto == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<div class="sec"><div class="sec-title">Carga Tribut&aacute;ria Efetiva (sobre faturamento)</div>`)
	sb.WriteString(`<table class="data-table"><thead><tr><th>Imposto</th><th style="text-align:right">Al&iacute;quota Efetiva</th><th>Regime</th></tr></thead><tbody>`)
	sb.WriteString(fmt.Sprintf(`<tr><td>ICMS a Recolher</td><td style="text-align:right">%.2f%%</td><td>Atual</td></tr>`, d.AliquotaEfetivaICMS))
	sb.WriteString(fmt.Sprintf(`<tr><td>IBS Projetado</td><td style="text-align:right">%.2f%%</td><td>Proje&ccedil;&atilde;o 2033</td></tr>`, d.AliquotaEfetivaIBS))
	sb.WriteString(fmt.Sprintf(`<tr><td>CBS Projetado</td><td style="text-align:right">%.2f%%</td><td>Proje&ccedil;&atilde;o 2033</td></tr>`, d.AliquotaEfetivaCBS))
	sb.WriteString(fmt.Sprintf(`<tr><td>Total IBS + CBS</td><td style="text-align:right">%.2f%%</td><td>Proje&ccedil;&atilde;o 2033</td></tr>`, d.AliquotaEfetivaTotalReforma))
	sb.WriteString(`</tbody></table></div>`)
	return sb.String()
}

// generateComparativoHTML renders the previous period comparison section (if data available).
func generateComparativoHTML(d TaxComparisonData, periodoAtual string) string {
	if d.PeriodoAnterior == "" || d.FaturamentoAnterior == 0 {
		return ""
	}
	varFat := ((d.FaturamentoBruto - d.FaturamentoAnterior) / d.FaturamentoAnterior) * 100
	varIcms := 0.0
	if d.IcmsAPagarAnterior > 0 {
		varIcms = ((d.IcmsAPagar - d.IcmsAPagarAnterior) / d.IcmsAPagarAnterior) * 100
	}
	arrow := func(v float64) string {
		if v > 0 {
			return fmt.Sprintf(`<span style="color:#10b981">&#8593; +%.1f%%</span>`, v)
		}
		if v < 0 {
			return fmt.Sprintf(`<span style="color:#e53e3e">&#8595; %.1f%%</span>`, v)
		}
		return "0,0%"
	}
	var sb strings.Builder
	sb.WriteString(`<div class="sec"><div class="sec-title">Comparativo com Per&iacute;odo Anterior</div>`)
	sb.WriteString(fmt.Sprintf(`<table class="data-table"><thead><tr><th>Indicador</th><th style="text-align:right">%s</th><th style="text-align:right">%s</th><th style="text-align:right">Varia&ccedil;&atilde;o</th></tr></thead><tbody>`,
		d.PeriodoAnterior, periodoAtual))
	sb.WriteString(fmt.Sprintf(`<tr><td>Faturamento Bruto</td><td style="text-align:right">R$ %s</td><td style="text-align:right">R$ %s</td><td style="text-align:right">%s</td></tr>`,
		formatEmailBRL(d.FaturamentoAnterior), formatEmailBRL(d.FaturamentoBruto), arrow(varFat)))
	sb.WriteString(fmt.Sprintf(`<tr><td>ICMS a Recolher</td><td style="text-align:right">R$ %s</td><td style="text-align:right">R$ %s</td><td style="text-align:right">%s</td></tr>`,
		formatEmailBRL(d.IcmsAPagarAnterior), formatEmailBRL(d.IcmsAPagar), arrow(varIcms)))
	if d.AliquotaEfetivaICMSAnterior > 0 {
		varAliq := d.AliquotaEfetivaICMS - d.AliquotaEfetivaICMSAnterior
		sb.WriteString(fmt.Sprintf(`<tr><td>Al&iacute;quota Efetiva ICMS</td><td style="text-align:right">%.2f%%</td><td style="text-align:right">%.2f%%</td><td style="text-align:right">%s p.p.</td></tr>`,
			d.AliquotaEfetivaICMSAnterior, d.AliquotaEfetivaICMS, arrow(varAliq)))
	}
	sb.WriteString(`</tbody></table></div>`)
	return sb.String()
}

// buildPlainTextSummary builds the plain text version of the email (for spam filters / text-only clients).
func buildPlainTextSummary(companyName, periodo string, d TaxComparisonData, narrativaPlain, appURL string) string {
	ibsCbsTotal := d.IbsProjetado + d.CbsProjetado
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("FBTax Cloud - Resumo Executivo\n\nEmpresa: %s\nPeriodo: %s\nGerado em: %s\n\n", companyName, periodo, getTimeBrasil()))
	sb.WriteString("=== DADOS DO PERIODO ===\n")
	sb.WriteString(fmt.Sprintf("Faturamento Bruto:   R$ %s\n", formatEmailBRL(d.FaturamentoBruto)))
	sb.WriteString(fmt.Sprintf("Total de Entradas:   R$ %s\n", formatEmailBRL(d.TotalEntradas)))
	sb.WriteString(fmt.Sprintf("ICMS Debito:         R$ %s\n", formatEmailBRL(d.IcmsSaida)))
	sb.WriteString(fmt.Sprintf("ICMS Credito:        R$ %s\n", formatEmailBRL(d.IcmsEntrada)))
	sb.WriteString(fmt.Sprintf("ICMS a Recolher:     R$ %s\n\n", formatEmailBRL(d.IcmsAPagar)))
	sb.WriteString("=== REFORMA TRIBUTARIA (Projecao 2033) ===\n")
	sb.WriteString(fmt.Sprintf("IBS Projetado:       R$ %s\n", formatEmailBRL(d.IbsProjetado)))
	sb.WriteString(fmt.Sprintf("CBS Projetado:       R$ %s\n", formatEmailBRL(d.CbsProjetado)))
	sb.WriteString(fmt.Sprintf("Total IBS + CBS:     R$ %s\n\n", formatEmailBRL(ibsCbsTotal)))
	if d.FaturamentoBruto > 0 {
		sb.WriteString("=== CARGA TRIBUTARIA EFETIVA ===\n")
		sb.WriteString(fmt.Sprintf("ICMS efetivo:        %.2f%%\n", d.AliquotaEfetivaICMS))
		sb.WriteString(fmt.Sprintf("IBS+CBS efetivo:     %.2f%%\n\n", d.AliquotaEfetivaTotalReforma))
	}
	if d.PeriodoAnterior != "" && d.FaturamentoAnterior > 0 {
		varFat := ((d.FaturamentoBruto - d.FaturamentoAnterior) / d.FaturamentoAnterior) * 100
		sb.WriteString(fmt.Sprintf("=== COMPARATIVO COM %s ===\n", d.PeriodoAnterior))
		sb.WriteString(fmt.Sprintf("Faturamento anterior: R$ %s (variacao: %+.1f%%)\n\n", formatEmailBRL(d.FaturamentoAnterior), varFat))
	}
	if d.CreditosEmRiscoTotal > 0 {
		sb.WriteString("=== ATENCAO: CREDITOS IBS+CBS EM RISCO ===\n")
		sb.WriteString(fmt.Sprintf("Total em risco:            R$ %s\n", formatEmailBRL(d.CreditosEmRiscoTotal)))
		sb.WriteString(fmt.Sprintf("NF-e sem credito IBS/CBS:  R$ %s\n", formatEmailBRL(d.CreditosNFeSemIBS)))
		sb.WriteString(fmt.Sprintf("Fornec. Simples Nacional:  R$ %s\n\n", formatEmailBRL(d.CreditosSimplesNacional)))
	}
	sb.WriteString("=== ANALISE DA IA ===\n")
	sb.WriteString(narrativaPlain)
	sb.WriteString(fmt.Sprintf("\n\nAcesse o painel completo: %s/relatorios/resumo-executivo\n\n---\n(c) 2026 FBTax Cloud - Todos os direitos reservados\n", appURL))
	return sb.String()
}

// generateCreditosEmRiscoHTML renders the "Créditos IBS+CBS em Risco" alert section.
func generateCreditosEmRiscoHTML(d TaxComparisonData, appURL string) string {
	if d.CreditosEmRiscoTotal == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<div class="sec" style="background:#fff5f5;border:2px solid #fc8181;border-radius:8px;padding:20px;margin:20px 0">`)
	sb.WriteString(`<table cellpadding="0" cellspacing="0" style="margin-bottom:14px"><tr>`)
	sb.WriteString(`<td style="font-size:20px;vertical-align:middle;padding-right:8px">&#9888;</td>`)
	sb.WriteString(`<td style="font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:.06em;color:#c53030;vertical-align:middle">Aten&ccedil;&atilde;o: Cr&eacute;ditos IBS+CBS em Risco</td>`)
	sb.WriteString(`</tr></table>`)

	// Total highlight
	sb.WriteString(fmt.Sprintf(`<div style="background:#c53030;border-radius:8px;padding:16px;text-align:center;margin-bottom:14px">
  <div style="font-size:11px;color:#fed7d7;text-transform:uppercase;letter-spacing:.08em;margin-bottom:4px">Total de Cr&eacute;ditos que podem N&Atilde;O ser aproveitados</div>
  <div style="font-size:26px;font-weight:700;color:#fff">R$ %s</div>
  <div style="font-size:10px;color:#fed7d7;margin-top:4px">Estimativa com al&iacute;quotas 2033</div>
</div>`, formatEmailBRL(d.CreditosEmRiscoTotal)))

	// Breakdown table
	sb.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px">`)
	sb.WriteString(`<thead><tr>`)
	sb.WriteString(`<th style="background:#822727;color:#fff;padding:8px 12px;text-align:left;font-size:12px">Origem</th>`)
	sb.WriteString(`<th style="background:#822727;color:#fff;padding:8px 12px;text-align:right;font-size:12px">Cr&eacute;dito Estimado Perdido</th>`)
	sb.WriteString(`</tr></thead><tbody>`)
	sb.WriteString(fmt.Sprintf(`<tr style="background:#fff5f5">
  <td style="padding:8px 12px;border-bottom:1px solid #fed7d7">NF-e de entradas sem IBS/CBS (fornecedores sem as tags)</td>
  <td style="padding:8px 12px;border-bottom:1px solid #fed7d7;text-align:right;color:#c53030;font-weight:600">R$ %s</td>
</tr>`, formatEmailBRL(d.CreditosNFeSemIBS)))
	sb.WriteString(fmt.Sprintf(`<tr style="background:#fff5f5">
  <td style="padding:8px 12px">Fornecedores do Simples Nacional (sem cr&eacute;dito de IBS/CBS)</td>
  <td style="padding:8px 12px;text-align:right;color:#c53030;font-weight:600">R$ %s</td>
</tr>`, formatEmailBRL(d.CreditosSimplesNacional)))
	sb.WriteString(`</tbody></table>`)
	sb.WriteString(fmt.Sprintf(`<div style="text-align:center;margin-top:12px">
  <a href="%s/apuracao/creditos-perdidos" style="font-size:12px;color:#c53030;font-weight:600">Ver relat&oacute;rio completo de cr&eacute;ditos em risco &rarr;</a>
</div>`, appURL))
	sb.WriteString(`</div>`)
	return sb.String()
}

// convertMarkdownToHTML converts basic markdown to HTML for email rendering
// stripHTMLTags removes HTML tags for plain text email version
func stripHTMLTags(html string) string {
	// Simple HTML tag removal for plain text version
	result := html
	result = strings.ReplaceAll(result, "<p>", "")
	result = strings.ReplaceAll(result, "</p>", "\n")
	result = strings.ReplaceAll(result, "<br>", "\n")
	result = strings.ReplaceAll(result, "<br/>", "\n")
	result = strings.ReplaceAll(result, "<br />", "\n")
	result = strings.ReplaceAll(result, "<h2>", "\n\n")
	result = strings.ReplaceAll(result, "</h2>", "\n")
	result = strings.ReplaceAll(result, "<h3>", "\n")
	result = strings.ReplaceAll(result, "</h3>", "\n")
	result = strings.ReplaceAll(result, "<strong>", "")
	result = strings.ReplaceAll(result, "</strong>", "")
	result = strings.ReplaceAll(result, "<em>", "")
	result = strings.ReplaceAll(result, "</em>", "")
	result = strings.ReplaceAll(result, "<ul>", "\n")
	result = strings.ReplaceAll(result, "</ul>", "\n")
	result = strings.ReplaceAll(result, "<ol>", "\n")
	result = strings.ReplaceAll(result, "</ol>", "\n")
	result = strings.ReplaceAll(result, "<li>", "  - ")
	result = strings.ReplaceAll(result, "</li>", "\n")
	// Table tags
	result = strings.ReplaceAll(result, "<thead>", "")
	result = strings.ReplaceAll(result, "</thead>", "")
	result = strings.ReplaceAll(result, "<tbody>", "")
	result = strings.ReplaceAll(result, "</tbody>", "")
	result = strings.ReplaceAll(result, "<tr>", "")
	result = strings.ReplaceAll(result, "</tr>", "\n")
	result = strings.ReplaceAll(result, "</td>", " | ")
	result = strings.ReplaceAll(result, "</th>", " | ")
	// Remove remaining HTML tags with style attributes
	for strings.Contains(result, "<") {
		start := strings.Index(result, "<")
		end := strings.Index(result[start:], ">")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}
	return result
}

func convertMarkdownToHTML(markdown string) string {
	html := markdown
	lines := strings.Split(html, "\n")

	var result strings.Builder
	inList := false
	inCodeBlock := false
	inTable := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Code blocks
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				result.WriteString("<div class=\"code-block\">")
			} else {
				result.WriteString("</div>")
			}
			continue
		}

		if inCodeBlock {
			result.WriteString(line + "<br>")
			continue
		}

		// Markdown tables: detect lines starting and ending with |
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			// Skip separator lines (|---|---|)
			isSeparator := true
			cells := strings.Split(trimmed, "|")
			for _, cell := range cells {
				cell = strings.TrimSpace(cell)
				if cell != "" && !isTableSeparator(cell) {
					isSeparator = false
					break
				}
			}

			if !inTable {
				if inList {
					result.WriteString("</ul>")
					inList = false
				}
				result.WriteString(`<table style="width: 100%; border-collapse: collapse; margin: 15px 0;">`)
				inTable = true

				// First row is header
				cells := parseTableRow(trimmed)
				result.WriteString("<thead><tr>")
				for _, cell := range cells {
					cell = applyInlineBold(cell)
					result.WriteString(fmt.Sprintf(`<th style="background: #4a5568; color: white; padding: 8px 12px; text-align: left; font-size: 13px;">%s</th>`, cell))
				}
				result.WriteString("</tr></thead><tbody>")
				continue
			}

			if isSeparator {
				continue
			}

			// Data row
			cells = parseTableRow(trimmed)
			result.WriteString("<tr>")
			for _, cell := range cells {
				cell = applyInlineBold(cell)
				result.WriteString(fmt.Sprintf(`<td style="padding: 8px 12px; border-bottom: 1px solid #e2e8f0; font-size: 13px;">%s</td>`, cell))
			}
			result.WriteString("</tr>")
			continue
		}

		// Close table if we were in one
		if inTable {
			result.WriteString("</tbody></table>")
			inTable = false
		}

		// Headers
		if strings.HasPrefix(trimmed, "### ") {
			if inList {
				result.WriteString("</ul>")
				inList = false
			}
			text := strings.TrimPrefix(trimmed, "### ")
			result.WriteString(fmt.Sprintf("<h3>%s</h3>", text))
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			if inList {
				result.WriteString("</ul>")
				inList = false
			}
			text := strings.TrimPrefix(trimmed, "## ")
			result.WriteString(fmt.Sprintf("<h2>%s</h2>", text))
			continue
		}

		// Lists
		if strings.HasPrefix(trimmed, "- ") {
			if !inList {
				result.WriteString("<ul>")
				inList = true
			}
			text := applyInlineBold(strings.TrimPrefix(trimmed, "- "))
			result.WriteString(fmt.Sprintf("<li>%s</li>", text))
			continue
		}

		// Numbered lists
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed[:3], ".") {
			dotIdx := strings.Index(trimmed, ".")
			if dotIdx > 0 && dotIdx < 3 {
				if !inList {
					result.WriteString("<ol>")
					inList = true
				}
				text := applyInlineBold(strings.TrimSpace(trimmed[dotIdx+1:]))
				result.WriteString(fmt.Sprintf("<li>%s</li>", text))
				continue
			}
		}

		// Close list if needed
		if inList && trimmed == "" {
			result.WriteString("</ul>")
			inList = false
			continue
		}

		// Bold text: alternate **open** and **close** tags
		line = applyInlineBold(line)

		// Regular paragraph
		if trimmed != "" {
			if inList {
				result.WriteString("</ul>")
				inList = false
			}
			result.WriteString(fmt.Sprintf("<p>%s</p>", line))
		}

		// Add line break unless it's the last line
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	if inTable {
		result.WriteString("</tbody></table>")
	}
	if inList {
		result.WriteString("</ul>")
	}

	return result.String()
}

// isTableSeparator checks if a cell is a markdown table separator (----, :---:, etc.)
func isTableSeparator(cell string) bool {
	cell = strings.TrimSpace(cell)
	for _, c := range cell {
		if c != '-' && c != ':' {
			return false
		}
	}
	return len(cell) > 0
}

// parseTableRow extracts cell contents from a markdown table row
func parseTableRow(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	parts := strings.Split(row, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// applyInlineBold converts **text** to <strong>text</strong>
func applyInlineBold(text string) string {
	for strings.Contains(text, "**") {
		text = strings.Replace(text, "**", "<strong>", 1)
		if strings.Contains(text, "**") {
			text = strings.Replace(text, "**", "</strong>", 1)
		}
	}
	return text
}

// generateTaxComparisonSVG creates an inline SVG bar chart comparing ICMS vs IBS vs CBS
func generateTaxComparisonSVG(data TaxComparisonData) string {
	maxVal := data.IcmsAPagar
	if data.IbsProjetado > maxVal {
		maxVal = data.IbsProjetado
	}
	if data.CbsProjetado > maxVal {
		maxVal = data.CbsProjetado
	}
	ibsCbsTotal := data.IbsProjetado + data.CbsProjetado
	if ibsCbsTotal > maxVal {
		maxVal = ibsCbsTotal
	}
	if maxVal == 0 {
		maxVal = 1 // avoid division by zero
	}

	maxBarWidth := 380.0
	barHeight := 30.0
	barSpacing := 50.0

	icmsWidth := (data.IcmsAPagar / maxVal) * maxBarWidth
	ibsWidth := (data.IbsProjetado / maxVal) * maxBarWidth
	cbsWidth := (data.CbsProjetado / maxVal) * maxBarWidth
	totalWidth := (ibsCbsTotal / maxVal) * maxBarWidth

	// Ensure minimum visible bar width when value > 0
	if data.IcmsAPagar > 0 && icmsWidth < 5 {
		icmsWidth = 5
	}
	if data.IbsProjetado > 0 && ibsWidth < 5 {
		ibsWidth = 5
	}
	if data.CbsProjetado > 0 && cbsWidth < 5 {
		cbsWidth = 5
	}
	if ibsCbsTotal > 0 && totalWidth < 5 {
		totalWidth = 5
	}

	svgHeight := 4*barSpacing + 30

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 520 %.0f" style="max-width: 520px; width: 100%%;">`, svgHeight))

	// ICMS bar
	y := 15.0
	sb.WriteString(fmt.Sprintf(`<text x="0" y="%.0f" font-family="Arial" font-size="12" fill="#4a5568" font-weight="bold">ICMS</text>`, y))
	y += 5
	sb.WriteString(fmt.Sprintf(`<rect x="120" y="%.0f" width="%.1f" height="%.0f" rx="4" fill="#3B82F6"/>`, y, icmsWidth, barHeight))
	sb.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.0f" font-family="Arial" font-size="11" fill="#333">R$ %s</text>`, 125+icmsWidth, y+20, formatEmailBRL(data.IcmsAPagar)))

	// IBS bar
	y += barSpacing
	sb.WriteString(fmt.Sprintf(`<text x="0" y="%.0f" font-family="Arial" font-size="12" fill="#4a5568" font-weight="bold">IBS</text>`, y))
	y += 5
	sb.WriteString(fmt.Sprintf(`<rect x="120" y="%.0f" width="%.1f" height="%.0f" rx="4" fill="#10B981"/>`, y, ibsWidth, barHeight))
	sb.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.0f" font-family="Arial" font-size="11" fill="#333">R$ %s</text>`, 125+ibsWidth, y+20, formatEmailBRL(data.IbsProjetado)))

	// CBS bar
	y += barSpacing
	sb.WriteString(fmt.Sprintf(`<text x="0" y="%.0f" font-family="Arial" font-size="12" fill="#4a5568" font-weight="bold">CBS</text>`, y))
	y += 5
	sb.WriteString(fmt.Sprintf(`<rect x="120" y="%.0f" width="%.1f" height="%.0f" rx="4" fill="#F59E0B"/>`, y, cbsWidth, barHeight))
	sb.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.0f" font-family="Arial" font-size="11" fill="#333">R$ %s</text>`, 125+cbsWidth, y+20, formatEmailBRL(data.CbsProjetado)))

	// Total IBS+CBS bar
	y += barSpacing
	sb.WriteString(fmt.Sprintf(`<text x="0" y="%.0f" font-family="Arial" font-size="12" fill="#4a5568" font-weight="bold">IBS+CBS</text>`, y))
	y += 5
	sb.WriteString(fmt.Sprintf(`<rect x="120" y="%.0f" width="%.1f" height="%.0f" rx="4" fill="#8B5CF6"/>`, y, totalWidth, barHeight))
	sb.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.0f" font-family="Arial" font-size="11" fill="#333" font-weight="bold">R$ %s</text>`, 125+totalWidth, y+20, formatEmailBRL(ibsCbsTotal)))

	sb.WriteString(`</svg>`)
	return sb.String()
}

// generateTaxTableHTML creates an HTML table comparing current vs projected taxes
func generateTaxTableHTML(data TaxComparisonData) string {
	ibsCbsTotal := data.IbsProjetado + data.CbsProjetado
	return fmt.Sprintf(`<table class="tax-table">
	<thead>
		<tr>
			<th>Imposto</th>
			<th>Valor</th>
			<th>Tipo</th>
		</tr>
	</thead>
	<tbody>
		<tr>
			<td>ICMS a Recolher</td>
			<td>R$ %s</td>
			<td>Imposto atual</td>
		</tr>
		<tr>
			<td>IBS Projetado</td>
			<td>R$ %s</td>
			<td style="color: #10B981;">Novo (Reforma Tributaria)</td>
		</tr>
		<tr>
			<td>CBS Projetado</td>
			<td>R$ %s</td>
			<td style="color: #F59E0B;">Novo (Reforma Tributaria)</td>
		</tr>
		<tr>
			<td>Total IBS + CBS</td>
			<td>R$ %s</td>
			<td>Substituira ICMS + PIS/COFINS</td>
		</tr>
	</tbody>
</table>`, formatEmailBRL(data.IcmsAPagar), formatEmailBRL(data.IbsProjetado), formatEmailBRL(data.CbsProjetado), formatEmailBRL(ibsCbsTotal))
}

// formatEmailBRL formats a float as Brazilian currency (without R$ prefix)
func formatEmailBRL(value float64) string {
	if value == 0 {
		return "0,00"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	intPart := int64(value)
	decPart := int64(math.Round((value - float64(intPart)) * 100))

	intStr := fmt.Sprintf("%d", intPart)
	var parts []string
	for i := len(intStr); i > 0; i -= 3 {
		start := i - 3
		if start < 0 {
			start = 0
		}
		parts = append([]string{intStr[start:i]}, parts...)
	}
	result := strings.Join(parts, ".") + fmt.Sprintf(",%02d", decPart)
	if negative {
		return "-" + result
	}
	return result
}

// getTimeBrasil returns current time in Brazil timezone formatted
func getTimeBrasil() string {
	// Brazil time is UTC-3
	loc := time.FixedZone("BRT", -3*3600)
	return time.Now().In(loc).Format("02/01/2006 as 15:04")
}

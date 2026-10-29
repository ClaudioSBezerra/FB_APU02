package handlers

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

// ─── Allowlist de IP do webhook da RFB ──────────────────────────────────────
//
// O guia da RFB recomenda restringir o webhook às faixas oficiais de origem (produção:
// 189.9.61.23/32). A RFB não assina o aviso, então a origem é o único controle disponível.
// RFB_WEBHOOK_ALLOWED_IPS (IPs ou CIDRs separados por vírgula) é opcional: vazia = sem
// restrição. Preenchida sem nenhuma entrada válida = nega tudo (fail-closed).

// cgnatNet é a faixa de NAT de operadora (100.64.0.0/10): net.IP.IsPrivate não a cobre.
var cgnatNet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// ipInterno reporta endereços que só existem dentro da nossa infraestrutura (proxies, rede
// docker). Nunca são a origem real de uma chamada da RFB.
func ipInterno(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || cgnatNet.Contains(ip)
}

// webhookClientIP devolve o IP real de quem chamou, atrás de Cloudflare/nginx. O nginx faz
// "X-Forwarded-For: <recebido>, <IP do proxy>", então a ÚLTIMA entrada é o proxy interno (por
// isso GetClientIP não serve aqui). A entrada mais à direita que NÃO seja interna é a que a
// nossa infraestrutura acrescentou (o Cloudflare anexa o IP que viu) e não pode ser forjada
// pelo cliente, que só consegue prefixar entradas à esquerda. Entrada ilegível devolve nil
// (nega). Premissa: todo hop entre a Internet e a API que seja público também acrescenta a sua
// entrada; se houver um proxy público extra na cadeia (ex.: Traefik atrás do Cloudflare), a
// entrada mais à direita será o IP dele e a allowlist negará a RFB — por isso o IP calculado
// é logado em todo POST: confira-o antes de preencher RFB_WEBHOOK_ALLOWED_IPS.
func webhookClientIP(r *http.Request) net.IP {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		partes := strings.Split(xff, ",")
		for i := len(partes) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(partes[i]))
			if ip == nil {
				// O que está à direita foi acrescentado pela nossa infra: se não é um IP, a cadeia é
				// inesperada. Seguir para a esquerda aceitaria uma entrada forjada pelo cliente.
				return nil
			}
			if !ipInterno(ip) {
				return ip
			}
		}
	}
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil && !ipInterno(ip) {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

// parseIPAllowlist converte "ip, cidr, ..." em redes. Entradas inválidas são devolvidas à parte.
func parseIPAllowlist(raw string) (redes []*net.IPNet, invalidas []string) {
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.Contains(tok, "/") {
			if _, n, err := net.ParseCIDR(tok); err == nil {
				redes = append(redes, n)
				continue
			}
		} else if ip := net.ParseIP(tok); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			redes = append(redes, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		invalidas = append(invalidas, tok)
	}
	return redes, invalidas
}

// rfbWebhookOrigemPermitida aplica RFB_WEBHOOK_ALLOWED_IPS (lida a cada chamada: mudar o .env e
// recriar o container basta). Devolve o IP calculado para o log de quem chama.
func rfbWebhookOrigemPermitida(r *http.Request) (bool, net.IP) {
	raw := strings.TrimSpace(os.Getenv("RFB_WEBHOOK_ALLOWED_IPS"))
	if raw == "" {
		return true, nil
	}
	redes, invalidas := parseIPAllowlist(raw)
	if len(invalidas) > 0 {
		log.Printf("[RFB Webhook] AVISO: RFB_WEBHOOK_ALLOWED_IPS com entradas inválidas ignoradas: %q", invalidas)
	}
	if len(redes) == 0 {
		log.Printf("[RFB Webhook] ERRO: RFB_WEBHOOK_ALLOWED_IPS preenchida sem nenhuma entrada válida — negando todo POST (fail-closed)")
		return false, webhookClientIP(r)
	}
	ip := webhookClientIP(r)
	if ip == nil {
		return false, nil
	}
	for _, n := range redes {
		if n.Contains(ip) {
			return true, ip
		}
	}
	return false, ip
}

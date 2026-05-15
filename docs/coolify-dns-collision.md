# Runbook: Colisão de DNS entre stacks no Coolify

**Problema descoberto em:** 2026-05-15  
**Afetou:** `apuracao.fbtax.cloud` (FB_APU02) — 502 em `/api/auth/login`  
**Causa raiz:** Múltiplos stacks com service chamado `api` compartilham a rede externa `coolify`, causando resolução de DNS cruzada

---

## O problema

O servidor Coolify (Hostinger) hospeda múltiplos produtos FBTax no mesmo host Docker:

| Stack      | Domínio                 | Porta API |
|------------|-------------------------|-----------|
| FB_APU01   | simulador.fbtax.cloud   | 8081      |
| FB_APU02   | apuracao.fbtax.cloud    | 8081      |
| FB_APU04   | simu.fbtax.cloud        | 8084      |
| FBTAX_CLOUD| www.fbtax.cloud         | 8083      |

Todos usam serviços chamados `api` e `web`. O container `web` de cada stack fica em 3 redes:
- `coolify` (externa, compartilhada por todos os stacks — necessária para o Traefik rotear o tráfego externo)
- rede privada do stack (`<stack-id>_fb_net`)
- rede padrão do Compose

O DNS do Docker retorna IPs de **todas** as redes de um container. Quando o nginx dentro do `web` resolve o hostname `api`, pode encontrar o container `api` de **outro stack** via rede `coolify` e usar o IP errado → conexão recusada → **502**.

### Por que o AWS funcionava e o Coolify não

O servidor AWS (`fctax.fcxlabs.com`) roda apenas 1 stack. Sem concorrência de nomes, o DNS sempre resolve corretamente.

---

## Diagnóstico rápido

Quando um produto está com 502 mas o container `api` está saudável:

```bash
# 1. Confirmar que a API está up
docker exec <web-container> sh -c "wget -qO- http://localhost:8081/api/health"
# Se falhar → problema real na API

# 2. Verificar qual IP o nginx está resolvendo
docker exec <web-container> sh -c "wget -qO- http://api:8081/api/health 2>&1"
# Se "Connection refused" → DNS está apontando para outro container

# 3. Ver IPs de todos os containers nas redes
docker inspect <api-container> --format '{{range $n, $net := .NetworkSettings.Networks}}{{$n}}={{$net.IPAddress}} {{end}}'
docker inspect <web-container> --format '{{range $n, $net := .NetworkSettings.Networks}}{{$n}}={{$net.IPAddress}} {{end}}'
# Se o IP que o web usa não está listado no api → colisão confirmada
```

---

## A solução

**Regra:** Nunca use `server api:<port>;` no nginx de um produto deployado no Coolify com múltiplos stacks.

### 1. `docker-compose.yml` — adicionar alias único ao service `api`

```yaml
services:
  api:
    networks:
      fb_net:
        aliases:
          - <produto>-api   # ex: apu02-api, apu04-api, fbtax-cloud-api
    # NÃO colocar api na rede coolify — somente web precisa do coolify (Traefik)
```

**Importante:** Remover `coolify` das redes do `api`. Apenas o `web` precisa estar em `coolify` (para o Traefik fazer o roteamento externo). Colocar o `api` em `coolify` polui o DNS de todos os outros stacks.

### 2. `frontend/nginx.conf` — usar o alias no upstream

```nginx
upstream backend {
    # Alias único evita colisão de DNS com outros stacks no coolify
    server <produto>-api:<port>;
}
```

O alias é definido dentro de `fb_net` (rede privada do stack) e portanto só existe nessa rede. O Docker DNS nunca vai confundir `apu02-api` com o container de outro produto.

---

## Status de cada produto

| Produto     | Alias aplicado      | Nginx atualizado | Data do fix |
|-------------|---------------------|------------------|-------------|
| FB_APU02    | `apu02-api`         | sim              | 2026-05-15  |
| FB_APU04    | `apu04-api`         | sim              | 2026-05-15  |
| FB_APU01    | `apu01-api`         | sim              | 2026-05-15  |
| FBTAX_CLOUD | `fbtax-cloud-api`   | sim              | 2026-05-15  |
| FB_APU03    | N/A (sem coolify)   | N/A              | não afetado |
| FB_SMARTPICK| pendente            | pendente         | —           |
| FB_FAROL    | pendente            | pendente         | —           |

---

## Checklist para novos produtos no Coolify

Ao criar um novo produto que vai rodar no mesmo servidor Coolify:

- [ ] O service `api` tem `aliases: [<produto>-api]` na rede privada do projeto
- [ ] O service `api` **não** está na rede `coolify`
- [ ] O service `web` está em `coolify` (para Traefik) e na rede privada do projeto
- [ ] O `nginx.conf` usa `server <produto>-api:<port>;` no upstream, nunca `server api:<port>;`
- [ ] O router Traefik usa um nome único (ex: `fbtax-cloud`, `simu`, `apuracao`) — sem repetição entre produtos

---

## Para FB_SMARTPICK e FB_FAROL

Esses repos não estavam disponíveis localmente em 2026-05-15. Aplicar o mesmo padrão:

1. Editar `docker-compose.yml`:
   ```yaml
   api:
     networks:
       fb_net:
         aliases:
           - smartpick-api   # ou farol-api
   ```
2. Editar `frontend/nginx.conf`:
   ```nginx
   upstream backend {
       server smartpick-api:<port>;
   }
   ```
3. Fazer commit e push para triggerar redeploy no Coolify

---

## Referência técnica

Commit do fix original (APU02): `bb95c44`  
Commits que aplicaram o fix nos demais produtos: ver git log de cada repo em 2026-05-15

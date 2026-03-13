# FBTax Cloud - Questionário de Implantação

**Cliente:** _______________________________________________
**Data:** _______________________________________________
**Responsável Técnico:** _______________________________________________

---

## 1. INFRAESTRUTURA DO SERVIDOR

### 1.1 Servidor
Você já possui um servidor para hospedar o FBTax Cloud?

- [ ] **Sim, já tenho servidor próprio**
  - Provedor: _______________________________________________
  - Sistema Operacional: [ ] Ubuntu [ ] Debian [ ] RHEL [ ] CentOS [ ] Outro: _______
  - Versão do SO: _______________________________________________

- [ ] **Não, preciso contratar**
  - Recomendamos: VPS com mínimo 4GB RAM, 2 vCPUs, 40GB SSD
  - Provedores sugeridos: AWS, Azure, Google Cloud, DigitalOcean, Hostinger

### 1.2 Recursos do Servidor
- **RAM disponível:** __________ GB (mínimo: 2GB, recomendado: 4GB+)
- **Processador:** __________ vCPUs (mínimo: 1, recomendado: 2+)
- **Armazenamento:** __________ GB (mínimo: 20GB, recomendado: 40GB+)
- **Tráfego mensal:** __________ GB

### 1.3 Acesso ao Servidor
- **Endereço IP do servidor:** _______________________________________________
- **Forma de acesso:** [ ] SSH com senha [ ] SSH com chave [ ] Console web
- **Usuário de acesso:** _______________________________________________
- **Você tem acesso root/sudo?** [ ] Sim [ ] Não

---

## 2. DOMÍNIO E DNS

### 2.1 Domínio
Você possui um domínio para acessar o sistema?

- [ ] **Sim, já tenho domínio**
  - Domínio: _______________________________________________
  - Registrar: _______________________________________________
  - Você tem acesso à configuração de DNS? [ ] Sim [ ] Não

- [ ] **Não, usarei apenas pelo IP do servidor**
  - Observação: Neste caso, o acesso será via `http://IP-DO-SERVIDOR`

- [ ] **Preciso contratar domínio**
  - Sugestões: Registro.br, GoDaddy, Hostinger, Namecheap

### 2.2 Certificado SSL (HTTPS)
Você precisa de acesso HTTPS (conexão segura)?

- [ ] **Sim** (recomendado para produção)
  - Podemos configurar certificado Let's Encrypt (gratuito)

- [ ] **Não, apenas HTTP por enquanto**

---

## 3. CONFIGURAÇÃO DE EMAIL (SMTP)

O sistema precisa de um servidor SMTP para enviar emails de recuperação de senha.

### 3.1 Servidor SMTP
Você possui servidor de email?

- [ ] **Sim, já tenho SMTP configurado**
  - **Host SMTP:** _______________________________________________
  - **Porta:** [ ] 587 (TLS) [ ] 465 (SSL) [ ] 25 [ ] Outra: _______
  - **Requer autenticação?** [ ] Sim [ ] Não
  - **Usuário SMTP:** _______________________________________________
  - **Senha SMTP:** _______________________________________________
  - **Email remetente:** _______________________________________________

- [ ] **Não, preciso de ajuda para configurar**
  - Opções: Google Workspace, Microsoft 365, SendGrid, Hostinger Email

- [ ] **Vou configurar depois**
  - ⚠️ Sistema funcionará, mas não enviará emails de recuperação de senha

---

## 4. SEGURANÇA E FIREWALL

### 4.1 Firewall
O servidor possui firewall configurado?

- [ ] **Sim**
  - Tipo: [ ] UFW [ ] firewalld [ ] iptables [ ] Firewall do provedor cloud
  - Você tem acesso para liberar portas? [ ] Sim [ ] Não

- [ ] **Não sei**

- [ ] **Não tem firewall**

### 4.2 Portas Necessárias
As seguintes portas precisam estar liberadas:
- **Porta 22** (SSH - acesso ao servidor)
- **Porta 80** (HTTP - acesso web)
- **Porta 443** (HTTPS - se usar SSL)

Você consegue liberar essas portas?
- [ ] Sim, eu mesmo faço
- [ ] Sim, mas preciso de orientação
- [ ] Não, preciso de suporte do provedor

---

## 5. BACKUP E RECUPERAÇÃO

### 5.1 Política de Backup
Como você deseja que os backups sejam feitos?

- [ ] **Backup automático diário**
  - Retenção: _____ dias (sugestão: 7-30 dias)

- [ ] **Backup manual (sob demanda)**

- [ ] **Backup gerenciado pelo provedor cloud**

### 5.2 Armazenamento de Backup
Onde os backups devem ser armazenados?

- [ ] **No próprio servidor**
- [ ] **Em storage externo** (S3, Azure Blob, etc.)
- [ ] **Não definido ainda**

---

## 6. USUÁRIOS E ACESSOS

### 6.1 Usuários Iniciais
Quantos usuários precisam de acesso inicial ao sistema?

- Quantidade de usuários: _______________________________________________
- Níveis de acesso necessários:
  - [ ] Administrador (acesso total)
  - [ ] Gerente (gestão de empresas)
  - [ ] Usuário padrão (consultas)

### 6.2 Estrutura Organizacional
- **Quantos ambientes/environments:** _______________________________________________
- **Quantos grupos empresariais:** _______________________________________________
- **Quantas empresas:** _______________________________________________

---

## 7. CRONOGRAMA E SUPORTE

### 7.1 Prazo de Implantação
Qual o prazo desejado para conclusão da implantação?

- [ ] Urgente (1-3 dias)
- [ ] Normal (1 semana)
- [ ] Flexível (2+ semanas)

### 7.2 Horário de Implantação
Existe alguma restrição de horário para instalação?

- [ ] **Não, qualquer horário**
- [ ] **Sim, apenas:** _______________________________________________
- [ ] **Preferência por horário comercial**
- [ ] **Preferência por fora do horário comercial**

### 7.3 Contato Técnico
Quem será o contato técnico durante a implantação?

- **Nome:** _______________________________________________
- **Cargo:** _______________________________________________
- **Email:** _______________________________________________
- **Telefone/WhatsApp:** _______________________________________________
- **Horário de disponibilidade:** _______________________________________________

### 7.4 Treinamento
Você precisa de treinamento para uso do sistema?

- [ ] **Sim**
  - Formato: [ ] Presencial [ ] Online [ ] Vídeo gravado
  - Número de participantes: _______

- [ ] **Não, apenas documentação**

---

## 8. OBSERVAÇÕES E REQUISITOS ESPECIAIS

Há alguma observação, requisito especial ou restrição que devemos conhecer?

```
_________________________________________________________________________

_________________________________________________________________________

_________________________________________________________________________

_________________________________________________________________________
```

---

## 9. CHECKLIST DE DOCUMENTOS

Por favor, nos envie (se aplicável):

- [ ] Credenciais de acesso SSH ao servidor
- [ ] Dados de acesso ao painel do provedor cloud (se necessário)
- [ ] Credenciais SMTP
- [ ] Documentação de firewall/rede (se houver requisitos especiais)
- [ ] Chave SSH pública (se usar autenticação por chave)

---

## 10. ACEITE E APROVAÇÃO

Ao assinar este questionário, confirmo que:
- As informações fornecidas estão corretas
- Compreendo os requisitos técnicos do sistema
- Autorizo o início da implantação conforme as informações acima

**Assinatura:** _______________________________________________
**Data:** _______________________________________________

---

**Para uso interno - Fortes Bezerra**

Data de recebimento: _______________________________________________
Responsável pela implantação: _______________________________________________
Previsão de conclusão: _______________________________________________
Status: [ ] Aguardando informações [ ] Em andamento [ ] Concluído

Observações internas:
```
_________________________________________________________________________

_________________________________________________________________________
```

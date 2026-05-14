-- Migration 106: Semear CFOPs 3xxx (entradas do exterior) na tabela cfop
-- e corrigir registros em nfe_entradas que defaultaram para 'C' por falta de mapeamento.
--
-- CFOP 3xxx = Entradas/Aquisições procedentes do exterior (importação).
-- Fornecedores estrangeiros não têm CNPJ; forn_cnpj fica vazio nessas notas.
-- Sem este seed, erp_bridge_batch.go defaultava tipo_cfop='C' para todos os 3xxx,
-- incluindo-os indevidamente na análise de créditos em risco.

INSERT INTO cfop (cfop, descricao_cfop, tipo) VALUES
-- Entradas de mercadorias do exterior
('3101', 'Compra para industrialização ou produção rural sob regime aduaneiro especial',  'R'),
('3102', 'Compra para comercialização do exterior',                                        'R'),
('3126', 'Compra para utilização na prestação de serviço sujeita ao ISSQN',               'C'),
('3127', 'Compra para industrialização sob o regime de drawback',                          'R'),
-- Devoluções de saídas para o exterior
('3201', 'Devolução de venda de produção do estabelecimento',                              'O'),
('3202', 'Devolução de venda de mercadoria adquirida ou recebida de terceiros',            'O'),
('3211', 'Devolução de venda de produção do estabelecimento, efetuada fora do estab.',    'O'),
('3212', 'Devolução de venda de mercadoria adquirida, efetuada fora do estab.',            'O'),
-- Energia elétrica
('3251', 'Compra de energia elétrica para distribuição ou comercialização',                'O'),
('3252', 'Compra de energia elétrica por estabelecimento industrial',                      'O'),
('3253', 'Compra de energia elétrica por estabelecimento comercial',                       'O'),
('3257', 'Compra de energia elétrica para consumo por demanda contratada',                 'O'),
-- Serviços
('3301', 'Aquisição de serviço de comunicação para execução de serviço da mesma natureza','S'),
('3351', 'Aquisição de serviço de transporte para execução de serviço da mesma natureza', 'S'),
('3352', 'Aquisição de serviço de transporte por estabelecimento industrial',              'S'),
('3353', 'Aquisição de serviço de transporte por estabelecimento comercial',               'S'),
('3354', 'Aquisição de serviço de transporte por estabelecimento de prestador de serviços','S'),
('3355', 'Aquisição de serviço de transporte por estabelecimento de geradora de energia',  'S'),
('3356', 'Aquisição de serviço de transporte por estabelecimento de produtor rural',       'S'),
-- Substituição tributária
('3401', 'Compra para industrialização em operação com merc. sujeita ao regime de ST',    'R'),
('3403', 'Compra para comercialização em operação com mercadoria sujeita ao regime de ST','R'),
-- Transferências
('3503', 'Compra de mercadoria para comercialização ou industrialização — transferência',  'T'),
-- Ativo imobilizado
('3551', 'Compra de bem para o ativo imobilizado',                                         'A'),
('3553', 'Devolução de venda de bem do ativo imobilizado',                                 'O'),
-- Créditos de impostos
('3601', 'Recebimento, por transferência, de saldo credor de ICMS',                       'O'),
('3651', 'Recebimento, por transferência, de créditos de impostos não utilizados',        'O'),
-- Doações e regimes especiais
('3701', 'Entrada de mercadoria recebida com fim específico de exportação',                'O'),
('3930', 'Lançamento efetuado a título de entrada de bem sob regime especial aduaneiro',  'O'),
-- Outros do exterior (inclui o CFOP 3949 — causa imediata do problema)
('3949', 'Outra entrada de mercadoria ou prestação de serviço procedente do exterior',    'O')

ON CONFLICT (cfop) DO UPDATE SET tipo = EXCLUDED.tipo;

-- Corrigir registros já importados em nfe_entradas que defaultaram para 'C'
-- por ausência do CFOP na tabela. Atualiza apenas quando o tipo diverge.
UPDATE nfe_entradas ne
SET tipo_cfop = c.tipo
FROM cfop c
WHERE c.cfop = ne.cfop
  AND ne.cfop LIKE '3%'
  AND ne.tipo_cfop != c.tipo;

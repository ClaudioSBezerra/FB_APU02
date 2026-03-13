-- Seed basic CFOPs to ensure reports work out-of-the-box
-- Focus on Revenda (R) and Saida Legacy (S) to populate "Commercial Operations" tab

INSERT INTO cfop (cfop, descricao_cfop, tipo) VALUES
-- SAIDAS ESTADUAIS (Revenda)
('5101', 'Venda de produção do estabelecimento', 'R'),
('5102', 'Venda de mercadoria adquirida ou recebida de terceiros', 'R'),
('5103', 'Venda de produção do estabelecimento, efetuada fora do estabelecimento', 'R'),
('5104', 'Venda de mercadoria adquirida ou recebida de terceiros, efetuada fora do estabelecimento', 'R'),
('5401', 'Venda de produção do estabelecimento em operação com produto sujeito ao regime de substituição tributária', 'R'),
('5403', 'Venda de mercadoria adquirida ou recebida de terceiros em operação com mercadoria sujeita ao regime de substituição tributária', 'R'),
('5405', 'Venda de mercadoria adquirida ou recebida de terceiros em operação com mercadoria sujeita ao regime de substituição tributária, na condição de contribuinte substituído', 'R'),

-- SAIDAS INTERESTADUAIS (Revenda)
('6101', 'Venda de produção do estabelecimento', 'R'),
('6102', 'Venda de mercadoria adquirida ou recebida de terceiros', 'R'),
('6107', 'Venda de produção do estabelecimento, destinada a não contribuinte', 'R'),
('6108', 'Venda de mercadoria adquirida ou recebida de terceiros, destinada a não contribuinte', 'R'),
('6401', 'Venda de produção do estabelecimento em operação com produto sujeito ao regime de substituição tributária', 'R'),
('6403', 'Venda de mercadoria adquirida ou recebida de terceiros em operação com mercadoria sujeita ao regime de substituição tributária', 'R'),
('6404', 'Venda de mercadoria sujeita ao regime de substituição tributária, cujo imposto já tenha sido retido anteriormente', 'R'),

-- ENTRADAS (Revenda - Compra para comercialização)
('1101', 'Compra para industrialização', 'R'),
('1102', 'Compra para comercialização', 'R'),
('1401', 'Compra para industrialização em operação com mercadoria sujeita ao regime de substituição tributária', 'R'),
('1403', 'Compra para comercialização em operação com mercadoria sujeita ao regime de substituição tributária', 'R'),
('2101', 'Compra para industrialização', 'R'),
('2102', 'Compra para comercialização', 'R'),
('2401', 'Compra para industrialização em operação com mercadoria sujeita ao regime de substituição tributária', 'R'),
('2403', 'Compra para comercialização em operação com mercadoria sujeita ao regime de substituição tributária', 'R')

ON CONFLICT (cfop) DO NOTHING;

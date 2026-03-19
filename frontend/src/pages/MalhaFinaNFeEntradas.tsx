import MalhaFinaPanel from './MalhaFinaPanel';

export default function MalhaFinaNFeEntradas() {
  return (
    <MalhaFinaPanel
      tipo="nfe-entradas"
      title="Malha Fina — NF-e Entradas"
      description="NF-e (mod. 55/65) identificadas pela Receita Federal que não foram importadas como entradas."
      rfbDisponivel={false}
    />
  );
}

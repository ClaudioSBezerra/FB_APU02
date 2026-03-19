import MalhaFinaPanel from './MalhaFinaPanel';

export default function MalhaFinaNFeSaidas() {
  return (
    <MalhaFinaPanel
      tipo="nfe-saidas"
      title="Malha Fina — NF-e Saídas"
      description="NF-e (mod. 55/65) identificadas pela Receita Federal que não foram importadas como saídas."
      rfbDisponivel={true}
    />
  );
}

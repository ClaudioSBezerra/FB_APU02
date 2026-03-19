import MalhaFinaPanel from './MalhaFinaPanel';

export default function MalhaFinaCTe() {
  return (
    <MalhaFinaPanel
      tipo="cte"
      title="Malha Fina — CT-e"
      description="CT-e (mod. 57) identificados pela Receita Federal que não foram importados nos registros da empresa."
      rfbDisponivel={false}
    />
  );
}

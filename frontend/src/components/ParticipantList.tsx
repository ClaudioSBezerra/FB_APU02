import { useEffect, useState } from 'react';
import { useAuth } from '@/contexts/AuthContext';

interface Participant {
  id: string;
  cod_part: string;
  nome: string;
  cnpj: string;
  cpf: string;
  ie: string;
}

interface ParticipantListProps {
  jobId: string;
}

export function ParticipantList({ jobId }: ParticipantListProps) {
  const { token } = useAuth();
  const [participants, setParticipants] = useState<Participant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!token) return;
    setLoading(true);
    fetch(`/api/jobs/${jobId}/participants`, {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    })
      .then((res) => {
        if (!res.ok) throw new Error('Erro ao buscar participantes');
        return res.json();
      })
      .then((data) => {
        setParticipants(data);
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message);
        setLoading(false);
      });
  }, [jobId]);

  if (loading) return <div className="mt-8 text-center text-gray-600">Carregando participantes...</div>;
  if (error) return <div className="mt-8 text-center text-red-600">Erro: {error}</div>;

  return (
    <div className="mt-8 w-full max-w-4xl bg-white rounded shadow-md overflow-hidden">
      <div className="px-6 py-4 border-b border-gray-200">
        <h2 className="text-xl font-semibold text-gray-800">Participantes Importados ({participants.length})</h2>
      </div>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Código</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Nome</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">CNPJ / CPF</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Inscrição Estadual</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {participants.map((p) => (
              <tr key={p.id} className="hover:bg-gray-50">
                <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">{p.cod_part}</td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">{p.nome}</td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{p.cnpj || p.cpf}</td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{p.ie}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
import React, { createContext, useContext, useState, useEffect } from 'react';
import { useAuth } from '@/contexts/AuthContext';

export interface Filial {
  cnpj: string;
  nome: string;
  apelido: string;
}

interface FilialContextType {
  filiais: Filial[];
  selectedFiliais: string[];     // CNPJs selecionados; vazio = todas
  loadingFiliais: boolean;
  toggleFilial: (cnpj: string) => void;
  selectAll: () => void;
  isSelected: (cnpj: string) => boolean; // true quando vazio (= todas) ou incluso
}

const FilialContext = createContext<FilialContextType | undefined>(undefined);

export const FilialProvider = ({ children }: { children: React.ReactNode }) => {
  const { companyId } = useAuth();
  const [filiais, setFiliais] = useState<Filial[]>([]);
  const [selectedFiliais, setSelectedFiliais] = useState<string[]>([]);
  const [loadingFiliais, setLoadingFiliais] = useState(false);

  const storageKey = companyId ? `filiais_selecionadas_${companyId}` : null;

  // Recarrega lista de filiais ao trocar empresa
  useEffect(() => {
    if (!companyId) return;

    setLoadingFiliais(true);
    fetch('/api/filiais')
      .then(r => r.ok ? r.json() : [])
      .then((data: Filial[]) => {
        const list = data || [];
        setFiliais(list);

        // Restaura seleção salva, validando CNPJs que ainda existem
        if (storageKey) {
          try {
            const saved = localStorage.getItem(storageKey);
            if (saved) {
              const parsed: string[] = JSON.parse(saved);
              const validCnpjs = new Set(list.map(f => f.cnpj));
              setSelectedFiliais(parsed.filter(c => validCnpjs.has(c)));
              return;
            }
          } catch {
            // ignore parse errors
          }
        }
        setSelectedFiliais([]);
      })
      .catch(() => {
        setFiliais([]);
        setSelectedFiliais([]);
      })
      .finally(() => setLoadingFiliais(false));
  }, [companyId]); // eslint-disable-line react-hooks/exhaustive-deps

  // Persiste seleção no localStorage sempre que mudar
  useEffect(() => {
    if (!storageKey) return;
    localStorage.setItem(storageKey, JSON.stringify(selectedFiliais));
  }, [selectedFiliais, storageKey]);

  const toggleFilial = (cnpj: string) => {
    setSelectedFiliais(prev =>
      prev.includes(cnpj) ? prev.filter(c => c !== cnpj) : [...prev, cnpj]
    );
  };

  const selectAll = () => setSelectedFiliais([]);

  const isSelected = (cnpj: string) =>
    selectedFiliais.length === 0 || selectedFiliais.includes(cnpj);

  return (
    <FilialContext.Provider value={{
      filiais,
      selectedFiliais,
      loadingFiliais,
      toggleFilial,
      selectAll,
      isSelected,
    }}>
      {children}
    </FilialContext.Provider>
  );
};

export const useFiliais = () => {
  const ctx = useContext(FilialContext);
  if (!ctx) throw new Error('useFiliais must be used within FilialProvider');
  return ctx;
};

import { useState, useEffect } from 'react';
import { useAuth } from '@/contexts/AuthContext';

interface FileUploadProps {
  onUploadComplete?: (jobId: string) => void;
}

export function FileUpload({ onUploadComplete }: FileUploadProps) {
  const [file, setFile] = useState<File | null>(null);
  const [uploadStatus, setUploadStatus] = useState<string>('');
  const [isUploading, setIsUploading] = useState(false);
  const [jobId, setJobId] = useState<string | null>(null);

  useEffect(() => {
    if (!jobId) return;

    const interval = setInterval(async () => {
      try {
        const res = await fetch(`/api/jobs/${jobId}`);
        if (!res.ok) return;

        const data = await res.json();
        
        if (data.status === 'completed') {
          setUploadStatus(`Concluído! ${data.message}`);
          if (onUploadComplete && jobId) onUploadComplete(jobId);
          setJobId(null);
          setIsUploading(false);
        } else if (data.status === 'error') {
          setUploadStatus(`Erro no processamento: ${data.message}`);
          setJobId(null);
          setIsUploading(false);
        } else {
          setUploadStatus(`Processando... Status: ${data.status}`);
        }
      } catch (error) {
        console.error("Error polling job status", error);
      }
    }, 2000);

    return () => clearInterval(interval);
  }, [jobId]);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      setFile(e.target.files[0]);
      setUploadStatus('');
    }
  };

  const handleUpload = async () => {
    if (!file) return;

    setIsUploading(true);
    setUploadStatus('Enviando...');

    const formData = new FormData();
    formData.append('file', file);

    try {
      const response = await fetch('/api/upload', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`
        },
        body: formData,
      });

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(errorText || response.statusText);
      }

      const data = await response.json();
      setUploadStatus(`Arquivo enviado! Iniciando processamento... (Job ID: ${data.job_id})`);
      setJobId(data.job_id); // Start polling
      setFile(null); // Clear input
      
      // Reset input value to allow selecting same file again
      const fileInput = document.getElementById('file-upload') as HTMLInputElement;
      if (fileInput) fileInput.value = '';
      
    } catch (error: any) {
      setUploadStatus(`Erro: ${error.message}`);
      setIsUploading(false);
    }
  };

  return (
    <div className="p-6 bg-white rounded shadow-md w-full max-w-md mt-6">
      <h2 className="text-xl font-semibold mb-4 text-gray-800">Upload de Arquivos</h2>
      
      <div className="mb-4">
        <label className="block text-gray-700 text-sm font-bold mb-2">
          Selecione um arquivo (.txt ou .xml)
        </label>
        <input 
          id="file-upload"
          type="file" 
          accept=".txt,.xml"
          onChange={handleFileChange}
          className="block w-full text-sm text-gray-500
            file:mr-4 file:py-2 file:px-4
            file:rounded-full file:border-0
            file:text-sm file:font-semibold
            file:bg-blue-50 file:text-blue-700
            hover:file:bg-blue-100"
        />
      </div>

      <button
        onClick={handleUpload}
        disabled={!file || isUploading}
        className={`w-full font-bold py-2 px-4 rounded focus:outline-none focus:shadow-outline transition-colors ${
          !file || isUploading 
            ? 'bg-gray-400 cursor-not-allowed' 
            : 'bg-blue-500 hover:bg-blue-700 text-white'
        }`}
      >
        {isUploading ? 'Enviando...' : 'Processar Arquivo'}
      </button>

      {uploadStatus && (
        <div className={`mt-4 p-3 rounded text-sm ${
          uploadStatus.startsWith('Erro') ? 'bg-red-100 text-red-700' : 'bg-green-100 text-green-700'
        }`}>
          {uploadStatus}
        </div>
      )}
    </div>
  );
}
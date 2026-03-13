# Referência da API - FB_APU01 v1

## Base URL
Local: `http://localhost:8080` (Direct) ou `http://localhost:3000/api` (via Proxy)

## Endpoints

### 1. Health Check
Verifica o estado de saúde do serviço e conexão com banco de dados.
- **GET** `/api/health`
- **Response 200 OK**:
  ```json
  {
    "status": "running",
    "timestamp": "2024-03-27T10:00:00Z",
    "service": "FB_APU01 Fiscal Engine",
    "version": "0.1.0",
    "database": "connected"
  }
  ```

### 2. Upload de Arquivo
Envia um arquivo SPED para processamento.
- **POST** `/api/upload`
- **Content-Type**: `multipart/form-data`
- **Body**: `file` (Binary)
- **Response 200 OK**:
  ```json
  {
    "message": "File uploaded successfully",
    "job_id": "550e8400-e29b-41d4-a716-446655440000",
    "filename": "SPED_012024.txt"
  }
  ```
- **Error 400**: Arquivo inválido ou extensão não permitida.
- **Error 413**: Arquivo excede o limite permitido (Configurável no Nginx).

### 3. Status do Job
Consulta o progresso do processamento de um arquivo.
- **GET** `/api/jobs/{id}`
- **Response 200 OK**:
  ```json
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "completed",
    "message": "Analyzed: EMPRESA X | Participants: 150",
    "created_at": "...",
    "updated_at": "..."
  }
  ```
- **Status Possíveis**: `pending`, `processing`, `completed`, `error`.

### 4. Listar Participantes
Retorna os participantes extraídos de um arquivo específico.
- **GET** `/api/jobs/{id}/participants`
- **Response 200 OK**:
  ```json
  [
    {
      "id": "...",
      "cod_part": "C001",
      "nome": "CLIENTE EXEMPLO LTDA",
      "cnpj": "12.345.678/0001-99",
      "cpf": "",
      "uf": "SP",
      "ie": "123456789"
    }
  ]
  ```
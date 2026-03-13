import * as XLSX from 'xlsx';

export function exportToExcel(data: Record<string, unknown>[], fileName: string, sheetName: string = 'Dados') {
  const ws = XLSX.utils.json_to_sheet(data);
  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, ws, sheetName);
  XLSX.writeFile(wb, `${fileName}.xlsx`);
}

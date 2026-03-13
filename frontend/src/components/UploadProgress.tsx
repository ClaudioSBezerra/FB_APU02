import { Progress } from '@/components/ui/progress';
import { Button } from '@/components/ui/button';
import { Loader2, Pause, Play, X, AlertCircle, CheckCircle, Upload } from 'lucide-react';

export interface UploadProgressType {
  status: 'idle' | 'uploading' | 'paused' | 'completed' | 'error';
  percentage: number;
  bytesUploaded: number;
  bytesTotal: number;
  speed: number; // bytes/sec
  remainingTime: number; // seconds
  errorMessage?: string;
}

interface UploadProgressProps {
  progress: UploadProgressType;
  fileName: string;
  onPause?: () => void;
  onResume?: () => void;
  onCancel?: () => void;
  onRetry?: () => void;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatTime(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`;
  const hours = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  return `${hours}h ${mins}m`;
}

function formatSpeed(bytesPerSecond: number): string {
  if (bytesPerSecond < 1024) return `${Math.round(bytesPerSecond)} B/s`;
  if (bytesPerSecond < 1024 * 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`;
  return `${(bytesPerSecond / (1024 * 1024)).toFixed(1)} MB/s`;
}

export function UploadProgressDisplay({
  progress,
  fileName,
  onPause,
  onResume,
  onCancel,
  onRetry,
}: UploadProgressProps) {
  const { status, percentage, bytesUploaded, bytesTotal, speed, remainingTime } = progress;

  if (status === 'idle') {
    return null;
  }

  return (
    <div className="space-y-3 p-4 bg-muted/50 rounded-lg border">
      {/* Header with file name and status icon */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 min-w-0">
          {status === 'uploading' && <Loader2 className="h-4 w-4 text-primary animate-spin flex-shrink-0" />}
          {status === 'paused' && <Pause className="h-4 w-4 text-warning flex-shrink-0" />}
          {status === 'completed' && <CheckCircle className="h-4 w-4 text-positive flex-shrink-0" />}
          {status === 'error' && <AlertCircle className="h-4 w-4 text-destructive flex-shrink-0" />}
          <span className="text-sm font-medium truncate flex-1 min-w-0">{fileName}</span>
        </div>
        
        <div className="flex items-center gap-1 flex-shrink-0">
          {status === 'uploading' && onPause && (
            <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onPause} title="Pausar">
              <Pause className="h-3.5 w-3.5" />
            </Button>
          )}
          {status === 'paused' && onResume && (
            <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onResume} title="Retomar">
              <Play className="h-3.5 w-3.5" />
            </Button>
          )}
          {(status === 'uploading' || status === 'paused') && onCancel && (
            <Button variant="ghost" size="icon" className="h-7 w-7 text-destructive" onClick={onCancel} title="Cancelar">
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
          {status === 'error' && onRetry && (
            <Button variant="ghost" size="sm" className="h-7" onClick={onRetry}>
              <Upload className="h-3.5 w-3.5 mr-1" />
              Tentar novamente
            </Button>
          )}
        </div>
      </div>

      {/* Progress bar */}
      <Progress value={percentage} className="h-2" />

      {/* Stats row */}
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <div className="flex items-center gap-3">
          <span>{percentage}%</span>
          <span>{formatBytes(bytesUploaded)} / {formatBytes(bytesTotal)}</span>
        </div>
        
        {status === 'uploading' && speed > 0 && (
          <div className="flex items-center gap-3">
            <span>{formatSpeed(speed)}</span>
            {remainingTime > 0 && (
              <span>~{formatTime(remainingTime)}</span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
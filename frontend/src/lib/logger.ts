type LogLevel = 'info' | 'warn' | 'error' | 'debug';

interface LogEntry {
  timestamp: string;
  level: LogLevel;
  message: string;
  data?: unknown;
  context?: string;
}

class Logger {
  private static formatEntry(level: LogLevel, message: string, data?: unknown, context?: string): LogEntry {
    return {
      timestamp: new Date().toISOString(),
      level,
      message,
      data,
      context,
    };
  }

  private static print(entry: LogEntry) {
    const style = {
      info: 'color: #0ea5e9', // Sky blue
      warn: 'color: #eab308', // Yellow
      error: 'color: #ef4444', // Red
      debug: 'color: #a8a29e', // Stone
    };

    if (process.env.NODE_ENV === 'development') {
      console.groupCollapsed(
        `%c[${entry.level.toUpperCase()}] ${entry.context ? `[${entry.context}] ` : ''}${entry.message}`,
        style[entry.level]
      );
      if (entry.data) console.log('Data:', entry.data);
      console.log('Timestamp:', entry.timestamp);
      console.groupEnd();
    } else {
      // In production, you might want to send this to a monitoring service (e.g., Sentry, LogRocket)
      if (entry.level === 'error') {
        console.error(JSON.stringify(entry));
      } else if (entry.level === 'warn') {
        console.warn(JSON.stringify(entry));
      } else {
        console.log(JSON.stringify(entry));
      }
    }
  }

  static info(message: string, data?: unknown, context?: string) {
    this.print(this.formatEntry('info', message, data, context));
  }

  static warn(message: string, data?: unknown, context?: string) {
    this.print(this.formatEntry('warn', message, data, context));
  }

  static error(message: string, data?: unknown, context?: string) {
    this.print(this.formatEntry('error', message, data, context));
  }

  static debug(message: string, data?: unknown, context?: string) {
    this.print(this.formatEntry('debug', message, data, context));
  }
}

export const logger = Logger;
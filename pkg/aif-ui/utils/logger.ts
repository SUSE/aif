/* eslint-disable no-console */
/**
 * SUSE AI Extension Logging Service
 * Following Rancher patterns for proper error handling and user notifications
 */

export enum LogLevel {
  DEBUG = 0,
  INFO = 1,
  WARN = 2,
  ERROR = 3
}

interface LogContext {
  component?: string;
  action?: string;
  data?: unknown;
}

class Logger {
  private isDevelopment = false;
  private logLevel: LogLevel = LogLevel.INFO;
  private store: unknown = null;

  constructor() {
    // Detect development mode
    this.isDevelopment = process.env.NODE_ENV === 'development' ||
                        // eslint-disable-next-line @typescript-eslint/no-explicit-any
                        (typeof window !== 'undefined' && (window as any).__DEV__);

    // Set log level based on environment
    this.logLevel = this.isDevelopment ? LogLevel.DEBUG : LogLevel.INFO;
  }

  setStore(store: unknown) {
    this.store = store;
  }

  private shouldLog(level: LogLevel): boolean {
    return level >= this.logLevel;
  }

  private formatMessage(level: string, message: string, context?: LogContext): string {
    const prefix = `[SUSE-AI${context?.component ? `:${context.component}` : ''}]`;
    const action = context?.action ? ` ${context.action}:` : '';
    return `${prefix}${action} ${message}`;
  }

  debug(message: string, context?: LogContext) {
    if (this.shouldLog(LogLevel.DEBUG)) {
      const formatted = this.formatMessage('DEBUG', message, context);
      if (context?.data) {
        console.debug(formatted, context.data);
      } else {
        console.debug(formatted);
      }
    }
  }

  info(message: string, context?: LogContext) {
    if (this.shouldLog(LogLevel.INFO)) {
      const formatted = this.formatMessage('INFO', message, context);
      if (context?.data) {
        console.info(formatted, context.data);
      } else {
        console.info(formatted);
      }
    }
  }

  warn(message: string, context?: LogContext) {
    if (this.shouldLog(LogLevel.WARN)) {
      const formatted = this.formatMessage('WARN', message, context);
      if (context?.data) {
        console.warn(formatted, context.data);
      } else {
        console.warn(formatted);
      }
    }
  }

  error(message: string, error?: Error | unknown, context?: LogContext) {
    if (this.shouldLog(LogLevel.ERROR)) {
      const formatted = this.formatMessage('ERROR', message, context);
      if (error) {
        console.error(formatted, error);
      } else {
        console.error(formatted);
      }
    }

    // Show user-facing error notification
    if (this.store && !this.isDevelopment) {
      try {
        (this.store as { dispatch: (action: string, payload: unknown) => void }).dispatch('growl/error', {
          title: 'SUSE AI Extension Error',
          message: message,
          timeout: 8000
        });
      } catch {
        // Never let a logging failure propagate into the caller's error handler
      }
    }
  }

  // Specialized logging methods
  apiCall(method: string, url: string, context?: unknown) {
    this.debug(`API ${method.toUpperCase()} ${url}`, {
      component: 'API',
      action: 'request',
      data: context
    });
  }

  apiSuccess(method: string, url: string, response?: unknown) {
    this.debug(`API ${method.toUpperCase()} ${url} succeeded`, {
      component: 'API',
      action: 'success',
      data: response
    });
  }

  apiError(method: string, url: string, error: unknown) {
    this.error(`API ${method.toUpperCase()} ${url} failed`, error, {
      component: 'API',
      action: 'error'
    });
  }

  userAction(action: string, data?: unknown) {
    this.info(`User action: ${action}`, {
      component: 'UI',
      action: 'user',
      data
    });
  }

  userSuccess(message: string) {
    this.info(message, { component: 'UI', action: 'success' });

    // Show user-facing success notification
    if (this.store) {
      try {
        (this.store as { dispatch: (action: string, payload: unknown) => void }).dispatch('growl/success', {
          title: 'Success',
          message,
          timeout: 4000
        });
      } catch {
        // Never let a logging failure propagate
      }
    }
  }

  userError(message: string, error?: unknown) {
    this.error(message, error, { component: 'UI', action: 'error' });
  }

  // Development-only logging
  devLog(message: string, data?: unknown) {
    if (this.isDevelopment) {
      console.log(`[SUSE-AI:DEV] ${message}`, data || '');
    }
  }

  // Group logging for complex operations
  group(name: string) {
    if (this.isDevelopment && this.shouldLog(LogLevel.DEBUG)) {
      console.group(`[SUSE-AI] ${name}`);
    }
  }

  groupEnd() {
    if (this.isDevelopment && this.shouldLog(LogLevel.DEBUG)) {
      console.groupEnd();
    }
  }
}

// Export singleton instance
export const logger = new Logger();

// Convenience exports
export const log = {
  debug: (msg: string, ctx?: LogContext) => logger.debug(msg, ctx),
  info: (msg: string, ctx?: LogContext) => logger.info(msg, ctx),
  warn: (msg: string, ctx?: LogContext) => logger.warn(msg, ctx),
  error: (msg: string, err?: Error | unknown, ctx?: LogContext) => logger.error(msg, err, ctx),

  // Specialized methods
  api: {
    call: (method: string, url: string, ctx?: unknown) => logger.apiCall(method, url, ctx),
    success: (method: string, url: string, res?: unknown) => logger.apiSuccess(method, url, res),
    error: (method: string, url: string, err: unknown) => logger.apiError(method, url, err)
  },

  user: {
    action: (action: string, data?: unknown) => logger.userAction(action, data),
    success: (msg: string) => logger.userSuccess(msg),
    error: (msg: string, err?: unknown) => logger.userError(msg, err)
  },

  dev: (msg: string, data?: unknown) => logger.devLog(msg, data),
  group: (name: string) => logger.group(name),
  groupEnd: () => logger.groupEnd()
};

export default logger;
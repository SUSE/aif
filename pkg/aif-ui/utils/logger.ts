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
  data?: any;
}

class Logger {
  private isDevelopment = false;
  private logLevel: LogLevel = LogLevel.INFO;
  private store: any = null;

  constructor() {
    // Detect development mode
    this.isDevelopment = process.env.NODE_ENV === 'development' ||
                        (typeof window !== 'undefined' && (window as any).__DEV__);

    // Set log level based on environment
    this.logLevel = this.isDevelopment ? LogLevel.DEBUG : LogLevel.INFO;
  }

  setStore(store: any) {
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

  error(message: string, error?: Error | any, context?: LogContext) {
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
      this.store.dispatch('growl/error', {
        title: 'SUSE AI Extension Error',
        message: message,
        timeout: 8000
      });
    }
  }

  // Specialized logging methods
  apiCall(method: string, url: string, context?: any) {
    this.debug(`API ${method.toUpperCase()} ${url}`, {
      component: 'API',
      action: 'request',
      data: context
    });
  }

  apiSuccess(method: string, url: string, response?: any) {
    this.debug(`API ${method.toUpperCase()} ${url} succeeded`, {
      component: 'API',
      action: 'success',
      data: response
    });
  }

  apiError(method: string, url: string, error: any) {
    this.error(`API ${method.toUpperCase()} ${url} failed`, error, {
      component: 'API',
      action: 'error'
    });
  }

  userAction(action: string, data?: any) {
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
      this.store.dispatch('growl/success', {
        title: 'Success',
        message,
        timeout: 4000
      });
    }
  }

  userError(message: string, error?: any) {
    this.error(message, error, { component: 'UI', action: 'error' });
  }

  // Development-only logging
  devLog(message: string, data?: any) {
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
  error: (msg: string, err?: any, ctx?: LogContext) => logger.error(msg, err, ctx),

  // Specialized methods
  api: {
    call: (method: string, url: string, ctx?: any) => logger.apiCall(method, url, ctx),
    success: (method: string, url: string, res?: any) => logger.apiSuccess(method, url, res),
    error: (method: string, url: string, err: any) => logger.apiError(method, url, err)
  },

  user: {
    action: (action: string, data?: any) => logger.userAction(action, data),
    success: (msg: string) => logger.userSuccess(msg),
    error: (msg: string, err?: any) => logger.userError(msg, err)
  },

  dev: (msg: string, data?: any) => logger.devLog(msg, data),
  group: (name: string) => logger.group(name),
  groupEnd: () => logger.groupEnd()
};

export default logger;
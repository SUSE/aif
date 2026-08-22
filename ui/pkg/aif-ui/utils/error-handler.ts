/**
 * Standardized Error Handling Utility
 * Replaces complex error handling chains with simple, consistent patterns
 */

import type { Dispatchable, RancherError } from '../types/rancher-types';
import { NOTIFICATION_DURATION } from './constants';
import { logger } from './logger';

export interface StandardError {
  message: string;
  status?: number;
  code?: string;
  details?: string;
  retryable?: boolean;
}

interface HttpErrorShape {
  _status?: unknown;
  code?: unknown;
  status?: unknown;
  statusCode?: unknown;
  response?: { status?: unknown };
}

// rancher/request rejects with the parsed response body rather than an axios
// Error. The HTTP status is attached as a non-enumerable `_status`, including
// when the body is plain text. Norman may instead return a numeric string.
export function httpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined;

  const candidate = error as HttpErrorShape;
  const rawStatuses = [
    candidate._status,
    candidate.code,
    candidate.status,
    candidate.statusCode,
    candidate.response?.status,
  ];

  for (const rawStatus of rawStatuses) {
    const status = typeof rawStatus === 'string' ? Number.parseInt(rawStatus, 10) : rawStatus;
    if (typeof status === 'number' && Number.isFinite(status)) return status;
  }

  return undefined;
}

export class ErrorHandler {
  private store: Dispatchable;
  private component: string;

  constructor(store: Dispatchable, component: string) {
    this.store = store;
    this.component = component;
  }

  /**
   * Handle API errors with consistent patterns
   */
  handleApiError(error: unknown, operation: string, context?: Record<string, unknown>): StandardError {
    const standardError = this.normalizeError(error);

    // Log the error for debugging
    logger.warn(`${this.component}: ${operation} failed`, {
      component: this.component,
      action: operation,
      data: { error: standardError, context }
    });

    // Show user-friendly notification for non-retryable errors
    if (!standardError.retryable) {
      this.showUserNotification(operation, standardError);
    }

    return standardError;
  }

  /**
   * Normalize different error types into a consistent format
   */
  public normalizeError(error: unknown): StandardError {
    const status = httpStatus(error);
    const rawCode = typeof error === 'object' && error !== null
      ? (error as HttpErrorShape).code
      : undefined;

    // Standard Error instances may still carry axios/Rancher status fields,
    // but transport errors also need message/code-based retry classification.
    if (error instanceof Error) {
      const rancherError = error as Error & RancherError;

      return {
        message:   error.message,
        status,
        code:      typeof rawCode === 'string' || typeof rawCode === 'number' ? String(rawCode) : undefined,
        details:   this.extractErrorDetails(rancherError),
        retryable: this.isRetryableStatus(status) || this.isRetryableTransportError(error)
      };
    }

    // Handle RancherError
    if (status !== undefined || this.isRancherError(error)) {
      const rancherError = error as RancherError;

      return {
        message:   this.extractErrorMessage(rancherError),
        status,
        code:      typeof rawCode === 'string' || typeof rawCode === 'number' ? String(rawCode) : undefined,
        details:   this.extractErrorDetails(rancherError),
        retryable: this.isRetryableStatus(status) || this.isRetryableTransportError(error)
      };
    }

    // Handle unknown errors
    return {
      message: typeof error === 'string' ? error : 'Unknown error occurred',
      retryable: false
    };
  }

  private extractErrorMessage(error: RancherError): string {
    if (error.message) return error.message;
    if (typeof error.data === 'string') return error.data;
    if (error.data && typeof error.data === 'object' && 'message' in error.data) {
      return String(error.data.message);
    }

    const responseData = error.response?.data;
    if (typeof responseData === 'string') return responseData;
    if (responseData && typeof responseData === 'object' && 'message' in responseData) {
      return String(responseData.message);
    }

    return 'API request failed';
  }

  /**
   * Extract detailed error information from Rancher error responses
   */
  private extractErrorDetails(error: RancherError): string | undefined {
    const details = [];

    // Try to get message from response data
    const responseData = error.response?.data;
    if (responseData) {
      if (typeof responseData === 'object' && 'message' in responseData) {
        details.push(responseData.message as string);
      }
      if (typeof responseData === 'object' && 'error' in responseData) {
        details.push(responseData.error as string);
      }
    }

    // Try to get message from error data
    if (error.data && typeof error.data === 'object' && 'message' in error.data) {
      details.push(error.data.message as string);
    }

    return details.length > 0 ? details.join('; ') : undefined;
  }

  /**
   * Check if an error is retryable based on status code
   */
  private isRetryableStatus(status?: number): boolean {
    if (!status) return false;

    // Retryable status codes
    return [408, 429, 500, 502, 503, 504].includes(status);
  }

  private isRetryableTransportError(error: unknown): boolean {
    if (!(error instanceof Error)) return false;

    const code = (error as Error & { code?: unknown }).code;
    if (typeof code === 'string' && [
      'ECONNABORTED',
      'ECONNRESET',
      'ETIMEDOUT',
      'ERR_NETWORK',
    ].includes(code)) {
      return true;
    }

    return /network error|failed to fetch|timed?\s*out/i.test(error.message);
  }

  /**
   * Type guard for RancherError
   */
  private isRancherError(error: unknown): error is RancherError {
    return typeof error === 'object' &&
           error !== null &&
           !(error instanceof Error) &&
           ('message' in error || 'status' in error || 'response' in error);
  }

  /**
   * Show user-friendly error notification
   */
  private showUserNotification(operation: string, error: StandardError) {
    const title = this.getOperationTitle(operation);
    const message = this.getUserFriendlyMessage(error);

    this.store.dispatch('growl/error', {
      title,
      message,
      timeout: NOTIFICATION_DURATION.EXTENDED
    });
  }

  /**
   * Get user-friendly operation title
   */
  private getOperationTitle(operation: string): string {
    const titles: Record<string, string> = {
      'install': 'Installation Failed',
      'upgrade': 'Upgrade Failed',
      'uninstall': 'Uninstall Failed',
      'fetch': 'Data Fetch Failed',
      'validate': 'Validation Failed'
    };

    return titles[operation] || 'Operation Failed';
  }

  /**
   * Get user-friendly error message
   */
  private getUserFriendlyMessage(error: StandardError): string {
    if (error.status === 404) {
      return 'The requested resource was not found. It may have been deleted or moved.';
    }

    if (error.status === 403) {
      return 'You do not have permission to perform this action. Please contact your administrator.';
    }

    if (error.status === 409) {
      return 'A conflict occurred. The resource may have been modified by another user.';
    }

    if (error.status && error.status >= 500) {
      return 'A server error occurred. Please try again later or contact support if the problem persists.';
    }

    // Use the detailed message if available, otherwise use a generic message
    if (error.details) {
      return `${error.message}: ${error.details}`;
    }

    return error.message || 'An unexpected error occurred. Please try again.';
  }

  /**
   * Retry operation with exponential backoff
   */
  async withRetry<T>(
    operation: () => Promise<T>,
    operationName: string,
    maxRetries = 3,
    baseDelayMs = 1000
  ): Promise<T> {
    let lastError: StandardError | undefined;

    for (let attempt = 0; attempt <= maxRetries; attempt++) {
      try {
        return await operation();
      } catch (error) {
        lastError = this.handleApiError(error, operationName, { attempt, maxRetries });

        if (attempt < maxRetries && lastError.retryable) {
          const delay = baseDelayMs * Math.pow(2, attempt);
          logger.debug(`Retrying ${operationName} in ${delay}ms (attempt ${attempt + 1}/${maxRetries})`, {
            component: this.component,
            action: 'retry',
            data: { operation: operationName, attempt, delay }
          });
          await new Promise(resolve => setTimeout(resolve, delay));
        } else {
          break;
        }
      }
    }

    throw new Error(lastError?.message || 'Operation failed after retries');
  }
}

/**
 * Factory function to create ErrorHandler instance
 */
export function createErrorHandler(store: Dispatchable, component: string): ErrorHandler {
  return new ErrorHandler(store, component);
}

/**
 * Simple error handling for cases where you don't need full ErrorHandler
 */
export function handleSimpleError(error: unknown, fallbackMessage = 'Operation failed'): string {
  if (error instanceof Error) {
    return error.message;
  }

  if (typeof error === 'string') {
    return error;
  }

  if (typeof error === 'object' && error !== null && 'message' in error) {
    return (error as { message: string }).message;
  }

  if (typeof error === 'object' && error !== null && 'data' in error) {
    const data = (error as { data?: unknown }).data;
    if (typeof data === 'string') return data;
    if (typeof data === 'object' && data !== null && 'message' in data) {
      return String((data as { message: unknown }).message);
    }
  }

  return fallbackMessage;
}

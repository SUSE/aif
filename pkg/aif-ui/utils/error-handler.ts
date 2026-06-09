/**
 * Standardized Error Handling Utility
 * Replaces complex error handling chains with simple, consistent patterns
 */

import type { RancherStore, RancherError } from '../types/rancher-types';
import { logger } from './logger';

export interface StandardError {
  message: string;
  status?: number;
  code?: string;
  details?: string;
  retryable?: boolean;
}

export class ErrorHandler {
  private store: RancherStore;
  private component: string;

  constructor(store: RancherStore, component: string) {
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
    // Handle RancherError
    if (this.isRancherError(error)) {
      // Kubernetes API errors have 'code' as HTTP status (number), 'status' as "Failure" (string)
      // Rancher errors may have 'status' or 'response.status' as HTTP status
      const httpStatus = (error as any).code || error.status || error.response?.status;

      return {
        message: error.message || 'API request failed',
        status: typeof httpStatus === 'number' ? httpStatus : undefined,
        code: (error as any).code?.toString() || error.code?.toString(),
        details: this.extractErrorDetails(error),
        retryable: this.isRetryableStatus(httpStatus)
      };
    }

    // Handle standard Error
    if (error instanceof Error) {
      return {
        message: error.message,
        retryable: false
      };
    }

    // Handle unknown errors
    return {
      message: typeof error === 'string' ? error : 'Unknown error occurred',
      retryable: false
    };
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

  /**
   * Type guard for RancherError
   */
  private isRancherError(error: unknown): error is RancherError {
    return typeof error === 'object' &&
           error !== null &&
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
      timeout: 10000 // 10 seconds
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
export function createErrorHandler(store: RancherStore, component: string): ErrorHandler {
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

  return fallbackMessage;
}
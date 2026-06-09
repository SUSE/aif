/**
 * Promise Utilities for SUSE AI Extension
 * Following standard patterns for consistent async operation handling
 * Provides retry logic, timeout handling, and promise composition utilities
 */

import { TIMEOUT_VALUES, RETRY_CONFIG } from './constants';

// === Retry Configuration ===
export interface RetryOptions {
  maxAttempts?: number;
  baseDelay?: number;
  maxDelay?: number;
  backoffFactor?: number;
  timeout?: number;
  retryCondition?: (error: any) => boolean;
  onRetry?: (attempt: number, error: any) => void;
}

export interface TimeoutOptions {
  timeout: number;
  message?: string;
}

export interface ThrottleOptions {
  maxConcurrent: number;
  delay?: number;
}

// === Retry with Exponential Backoff ===
export async function retryWithBackoff<T>(
  fn: () => Promise<T>,
  maxAttempts: number = RETRY_CONFIG.MAX_ATTEMPTS,
  baseDelay: number = RETRY_CONFIG.BASE_DELAY,
  backoffFactor: number = RETRY_CONFIG.BACKOFF_FACTOR
): Promise<T> {
  let lastError: any;
  
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return await fn();
    } catch (error) {
      lastError = error;
      
      if (attempt === maxAttempts) {
        throw error;
      }
      
      // Calculate delay with exponential backoff
      const delay = Math.min(
        baseDelay * Math.pow(backoffFactor, attempt - 1),
        RETRY_CONFIG.MAX_DELAY
      );
      
      console.warn(`Attempt ${attempt} failed, retrying in ${delay}ms:`, (error as Error)?.message || error);
      await sleep(delay);
    }
  }
  
  throw lastError;
}

/**
 * Advanced retry with configurable options
 */
export async function retry<T>(
  fn: () => Promise<T>,
  options: RetryOptions = {}
): Promise<T> {
  const {
    maxAttempts = RETRY_CONFIG.MAX_ATTEMPTS,
    baseDelay = RETRY_CONFIG.BASE_DELAY,
    maxDelay = RETRY_CONFIG.MAX_DELAY,
    backoffFactor = RETRY_CONFIG.BACKOFF_FACTOR,
    timeout = TIMEOUT_VALUES.MEDIUM,
    retryCondition = () => true,
    onRetry
  } = options;
  
  let lastError: any;
  
  // Wrap with timeout
  const fnWithTimeout = timeout > 0 ? withTimeout(fn, timeout) : fn;
  
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return await fnWithTimeout();
    } catch (error) {
      lastError = error;
      
      // Check if we should retry this error
      if (!retryCondition(error)) {
        throw error;
      }
      
      if (attempt === maxAttempts) {
        throw error;
      }
      
      // Calculate delay with exponential backoff
      const delay = Math.min(
        baseDelay * Math.pow(backoffFactor, attempt - 1),
        maxDelay
      );
      
      // Call retry callback if provided
      if (onRetry) {
        onRetry(attempt, error);
      }
      
      await sleep(delay);
    }
  }
  
  throw lastError;
}

// === Timeout Handling ===

/**
 * Add timeout to a promise
 */
export function withTimeout<T>(
  promise: () => Promise<T>,
  timeoutMs: number,
  timeoutMessage?: string
): () => Promise<T> {
  return () => new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(timeoutMessage || `Operation timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    
    promise()
      .then(result => {
        clearTimeout(timer);
        resolve(result);
      })
      .catch(error => {
        clearTimeout(timer);
        reject(error);
      });
  });
}

/**
 * Create a timeout promise
 */
export function timeout(ms: number, message?: string): Promise<never> {
  return new Promise((_, reject) => {
    setTimeout(() => {
      reject(new Error(message || `Timeout after ${ms}ms`));
    }, ms);
  });
}

// === Promise Composition ===

/**
 * Race multiple promises with timeout
 */
export async function raceWithTimeout<T>(
  promises: Promise<T>[],
  timeoutMs: number,
  timeoutMessage?: string
): Promise<T> {
  return Promise.race([
    ...promises,
    timeout(timeoutMs, timeoutMessage)
  ]);
}

/**
 * Settle all promises with timeout for each
 */
export async function allSettledWithTimeout<T>(
  promises: (() => Promise<T>)[],
  timeoutMs: number
): Promise<PromiseSettledResult<T>[]> {
  const promisesWithTimeout = promises.map(p => 
    withTimeout(p, timeoutMs)()
      .then(value => ({ status: 'fulfilled' as const, value }))
      .catch(reason => ({ status: 'rejected' as const, reason }))
  );
  
  return Promise.all(promisesWithTimeout);
}

// === Concurrency Control ===

/**
 * Throttle promise execution to limit concurrency
 */
export async function throttlePromises<T>(
  promiseFunctions: (() => Promise<T>)[],
  options: ThrottleOptions = { maxConcurrent: 3 }
): Promise<T[]> {
  const { maxConcurrent, delay = 0 } = options;
  const results: T[] = new Array(promiseFunctions.length);
  const executing: Promise<void>[] = [];
  
  for (let i = 0; i < promiseFunctions.length; i++) {
    const promise = promiseFunctions[i]().then(result => {
      results[i] = result;
    });
    
    const executing_promise = promise.then(() => {
      executing.splice(executing.indexOf(executing_promise), 1);
    });
    
    executing.push(executing_promise);
    
    if (executing.length >= maxConcurrent) {
      await Promise.race(executing);
    }
    
    if (delay > 0 && i < promiseFunctions.length - 1) {
      await sleep(delay);
    }
  }
  
  await Promise.all(executing);
  return results;
}

/**
 * Execute promises in batches
 */
export async function batchPromises<T>(
  promiseFunctions: (() => Promise<T>)[],
  batchSize = 5,
  delay = 0
): Promise<T[]> {
  const results: T[] = [];
  
  for (let i = 0; i < promiseFunctions.length; i += batchSize) {
    const batch = promiseFunctions.slice(i, i + batchSize);
    const batchResults = await Promise.all(batch.map(fn => fn()));
    results.push(...batchResults);
    
    // Add delay between batches if specified
    if (delay > 0 && i + batchSize < promiseFunctions.length) {
      await sleep(delay);
    }
  }
  
  return results;
}

// === Queue Management ===

export class PromiseQueue {
  private queue: (() => Promise<any>)[] = [];
  private running: Promise<any>[] = [];
  private maxConcurrent: number;
  private paused = false;

  constructor(maxConcurrent = 1) {
    this.maxConcurrent = maxConcurrent;
  }

  /**
   * Add a promise function to the queue
   */
  add<T>(promiseFunction: () => Promise<T>): Promise<T> {
    return new Promise((resolve, reject) => {
      this.queue.push(() =>
        promiseFunction().then(resolve).catch(reject)
      );
      this.processQueue();
    });
  }

  /**
   * Pause queue processing
   */
  pause(): void {
    this.paused = true;
  }

  /**
   * Resume queue processing
   */
  resume(): void {
    this.paused = false;
    this.processQueue();
  }

  /**
   * Clear all pending items from queue
   */
  clear(): void {
    this.queue.length = 0;
  }

  /**
   * Get queue status
   */
  getStatus(): {
    pending: number;
    running: number;
    paused: boolean;
  } {
    return {
      pending: this.queue.length,
      running: this.running.length,
      paused: this.paused
    };
  }

  private async processQueue(): Promise<void> {
    if (this.paused || this.running.length >= this.maxConcurrent) {
      return;
    }

    const next = this.queue.shift();
    if (!next) {
      return;
    }

    const promise = next().finally(() => {
      const index = this.running.indexOf(promise);
      if (index > -1) {
        this.running.splice(index, 1);
      }
      this.processQueue(); // Process next item
    });

    this.running.push(promise);
    
    // Start processing more items if we can
    this.processQueue();
  }
}

// === Progress Tracking ===

export interface ProgressTracker {
  total: number;
  completed: number;
  failed: number;
  percentage: number;
  isComplete: boolean;
  errors: Array<{ index: number; error: any }>;
}

/**
 * Execute promises with progress tracking
 */
export async function executeWithProgress<T>(
  promiseFunctions: (() => Promise<T>)[],
  onProgress?: (progress: ProgressTracker) => void,
  options: { maxConcurrent?: number; continueOnError?: boolean } = {}
): Promise<{ results: (T | null)[]; errors: Array<{ index: number; error: any }> }> {
  const { maxConcurrent = 3, continueOnError = true } = options;
  const total = promiseFunctions.length;
  const results: (T | null)[] = new Array(total).fill(null);
  const errors: Array<{ index: number; error: any }> = [];
  let completed = 0;
  let failed = 0;

  const updateProgress = () => {
    const progress: ProgressTracker = {
      total,
      completed,
      failed,
      percentage: Math.round((completed + failed) / total * 100),
      isComplete: completed + failed === total,
      errors
    };
    
    if (onProgress) {
      onProgress(progress);
    }
  };

  // Execute with concurrency control
  await throttlePromises(
    promiseFunctions.map((fn, index) => async () => {
      try {
        const result = await fn();
        results[index] = result;
        completed++;
      } catch (error) {
        errors.push({ index, error });
        failed++;
        
        if (!continueOnError) {
          throw error;
        }
      } finally {
        updateProgress();
      }
    }),
    { maxConcurrent }
  );

  return { results, errors };
}

// === Utility Functions ===

/**
 * Sleep for specified milliseconds
 */
export function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Debounce a promise-returning function
 */
export function debouncePromise<T extends any[], R>(
  fn: (...args: T) => Promise<R>,
  delay: number
): (...args: T) => Promise<R> {
  let timeoutId: NodeJS.Timeout;
  let latestPromise: Promise<R>;

  return (...args: T): Promise<R> => {
    return new Promise((resolve, reject) => {
      clearTimeout(timeoutId);
      
      timeoutId = setTimeout(async () => {
        try {
          const result = await fn(...args);
          resolve(result);
        } catch (error) {
          reject(error);
        }
      }, delay);
    });
  };
}

/**
 * Throttle a promise-returning function
 */
export function throttlePromise<T extends any[], R>(
  fn: (...args: T) => Promise<R>,
  delay: number
): (...args: T) => Promise<R> {
  let lastExecution = 0;
  let pendingPromise: Promise<R> | null = null;

  return (...args: T): Promise<R> => {
    const now = Date.now();
    
    if (now - lastExecution >= delay && !pendingPromise) {
      lastExecution = now;
      pendingPromise = fn(...args).finally(() => {
        pendingPromise = null;
      });
      return pendingPromise;
    } else if (pendingPromise) {
      return pendingPromise;
    } else {
      return new Promise((resolve, reject) => {
        setTimeout(async () => {
          try {
            lastExecution = Date.now();
            const result = await fn(...args);
            resolve(result);
          } catch (error) {
            reject(error);
          }
        }, delay - (now - lastExecution));
      });
    }
  };
}

/**
 * Memoize a promise-returning function with TTL
 */
export function memoizePromise<T extends any[], R>(
  fn: (...args: T) => Promise<R>,
  ttlMs: number = 5 * 60 * 1000, // 5 minutes default
  keyFn?: (...args: T) => string
): (...args: T) => Promise<R> {
  const cache = new Map<string, { value: Promise<R>; expiry: number }>();

  return (...args: T): Promise<R> => {
    const key = keyFn ? keyFn(...args) : JSON.stringify(args);
    const now = Date.now();
    
    const cached = cache.get(key);
    if (cached && cached.expiry > now) {
      return cached.value;
    }

    const promise = fn(...args).catch(error => {
      // Remove failed promises from cache immediately
      cache.delete(key);
      throw error;
    });

    cache.set(key, {
      value: promise,
      expiry: now + ttlMs
    });

    return promise;
  };
}

/**
 * Create a cancelable promise
 */
export interface CancelablePromise<T> extends Promise<T> {
  cancel: () => void;
  isCanceled: () => boolean;
}

export function makeCancelable<T>(promise: Promise<T>): CancelablePromise<T> {
  let isCanceled = false;

  const wrappedPromise = new Promise<T>((resolve, reject) => {
    promise
      .then(value => {
        if (isCanceled) {
          reject(new Error('Promise was canceled'));
        } else {
          resolve(value);
        }
      })
      .catch(error => {
        if (isCanceled) {
          reject(new Error('Promise was canceled'));
        } else {
          reject(error);
        }
      });
  }) as CancelablePromise<T>;

  wrappedPromise.cancel = () => {
    isCanceled = true;
  };

  wrappedPromise.isCanceled = () => isCanceled;

  return wrappedPromise;
}

/**
 * Convert callback-based function to promise
 */
export function promisify<T>(
  fn: (callback: (error: any, result?: T) => void) => void
): () => Promise<T> {
  return () => new Promise<T>((resolve, reject) => {
    fn((error, result) => {
      if (error) {
        reject(error);
      } else {
        resolve(result!);
      }
    });
  });
}

// === Error Handling Utilities ===

/**
 * Check if error is retryable
 */
export function isRetryableError(error: any): boolean {
  // Network errors are generally retryable
  if (error.code === 'ENOTFOUND' || error.code === 'ECONNREFUSED' || error.code === 'ETIMEDOUT') {
    return true;
  }
  
  // HTTP 5xx errors are retryable
  if (error.response?.status >= 500) {
    return true;
  }
  
  // Rate limiting (429) is retryable
  if (error.response?.status === 429) {
    return true;
  }
  
  // Temporary failures
  if (error.message?.includes('temporary') || error.message?.includes('timeout')) {
    return true;
  }
  
  return false;
}

/**
 * Create standardized error with retry information
 */
export function createRetryError(
  originalError: any,
  attempts: number,
  maxAttempts: number
): Error & { isRetryError: boolean; attempts: number; maxAttempts: number } {
  const error = new Error(
    `Operation failed after ${attempts}/${maxAttempts} attempts. Last error: ${originalError.message || originalError}`
  ) as any;
  
  error.isRetryError = true;
  error.attempts = attempts;
  error.maxAttempts = maxAttempts;
  error.originalError = originalError;
  
  return error;
}
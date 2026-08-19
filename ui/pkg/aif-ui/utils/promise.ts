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

// === Utility Functions ===

/**
 * Sleep for specified milliseconds
 */
export function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}



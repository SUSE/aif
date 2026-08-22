import { describe, expect, it, vi } from 'vitest';
import { createErrorHandler, httpStatus } from '../error-handler';
import type { Dispatchable } from '../../types/rancher-types';

function rancherFailure(status: number, data: unknown = `${status} request failed`) {
  const body = { data };

  Object.defineProperty(body, '_status', { value: status });
  return body;
}

const store = {
  dispatch: vi.fn(async () => undefined),
} as unknown as Dispatchable;

describe('httpStatus', () => {
  it('reads Rancher request statuses from non-enumerable _status', () => {
    expect(httpStatus(rancherFailure(404, '404 page not found'))).toBe(404);
  });

  it('parses Norman string statuses', () => {
    expect(httpStatus({ status: '403' })).toBe(403);
  });
});

describe('ErrorHandler.normalizeError', () => {
  const errorHandler = createErrorHandler(store, 'ErrorHandlerTest');

  it('normalizes a plain Rancher response body with only _status', () => {
    expect(errorHandler.normalizeError(rancherFailure(404, '404 page not found'))).toMatchObject({
      message:   '404 page not found',
      status:    404,
      retryable: false,
    });
  });

  it('marks a text-body server rejection as retryable', () => {
    expect(errorHandler.normalizeError(rancherFailure(503))).toMatchObject({
      status:    503,
      retryable: true,
    });
  });

  it('marks transport timeouts as retryable', () => {
    const timeout = Object.assign(new Error('request timed out'), { code: 'ETIMEDOUT' });

    expect(errorHandler.normalizeError(timeout)).toMatchObject({
      message:   'request timed out',
      retryable: true,
    });
  });
});

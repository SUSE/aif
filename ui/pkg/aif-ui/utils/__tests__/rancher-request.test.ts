import { describe, expect, it } from 'vitest';
import { rancherHttpStatus, requestErrorMessage } from '../rancher-request';

describe('Rancher response errors', () => {
  it.each([
    { _status: 404 },
    { status: 'Failure', code: 404 },
    { status: '404' },
    { response: { status: 404 } },
  ])('reads HTTP status from %j', (error) => {
    expect(rancherHttpStatus(error)).toBe(404);
  });

  it('does not mistake a DOM exception or network error for HTTP status', () => {
    expect(rancherHttpStatus({ code: 20, status: 0 })).toBeUndefined();
  });

  it.each([
    [{ message: 'forbidden' }, 'forbidden'],
    [{ data: { message: 'forbidden' } }, 'forbidden'],
    [{ data: 'upstream unavailable' }, 'upstream unavailable'],
    [new Error('request failed'), 'request failed'],
    ['request failed', 'request failed'],
  ])('formats a response without requiring the token service', (error, expected) => {
    expect(requestErrorMessage(error)).toBe(expected);
  });
});

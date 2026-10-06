import { describe, expect, it } from 'vitest';
import { installRecordingWorkloadsFirst } from '../helm-install-order';

describe('installRecordingWorkloadsFirst', () => {
  it('records every cluster as pending before the install starts, then records the results', async() => {
    const calls: string[] = [];

    await installRecordingWorkloadsFirst(['local', 'c-1'], {
      recordPending: async(id) => {
        calls.push(`pending:${ id }`);
      },
      install: async() => {
        calls.push('install');
      },
      recordResult: async(id) => {
        calls.push(`result:${ id }`);
      },
    });

    expect(calls).toEqual(['pending:local', 'pending:c-1', 'install', 'result:local', 'result:c-1']);
  });

  it('still records the results when the install fails, and rethrows', async() => {
    const calls: string[] = [];

    await expect(installRecordingWorkloadsFirst(['local'], {
      recordPending: async(id) => {
        calls.push(`pending:${ id }`);
      },
      install: async() => {
        throw new Error('boom');
      },
      recordResult: async(id) => {
        calls.push(`result:${ id }`);
      },
    })).rejects.toThrow('boom');

    expect(calls).toEqual(['pending:local', 'result:local']);
  });

  it('records the clusters concurrently rather than one after another', async() => {
    const started: string[] = [];
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });

    const run = installRecordingWorkloadsFirst(['local', 'c-1'], {
      recordPending: async(id) => {
        started.push(id);
        await gate;
      },
      install:      async() => {},
      recordResult: async() => {},
    });

    await Promise.resolve();
    expect(started).toEqual(['local', 'c-1']);
    release();
    await run;
  });
});

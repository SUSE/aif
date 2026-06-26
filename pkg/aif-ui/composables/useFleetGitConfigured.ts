import { ref } from 'vue';
import { getSettings } from '../utils/operator-api';

type SettingsResponse = { spec?: { fleet?: { repoURL?: string } } };

export function useFleetGitConfigured() {
  const fleetGitConfigured = ref(false);

  async function fetchFleetGitConfigured(): Promise<void> {
    try {
      const settings = await getSettings() as SettingsResponse;
      fleetGitConfigured.value = !!settings?.spec?.fleet?.repoURL;
    } catch {
      fleetGitConfigured.value = false;
    }
  }

  return { fleetGitConfigured, fetchFleetGitConfigured };
}

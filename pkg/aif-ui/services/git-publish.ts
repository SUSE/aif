import { publishToFleetGit as operatorPublish } from '../utils/operator-api';
import { buildFleetBundleYAML }                   from './fleet-bundle';

export interface GitPublishParams {
  bundleName:       string;
  chartName:        string;
  chartVersion:     string;
  chartRepoUrl:     string;
  helmSecretName:   string | null;
  values:           Record<string, unknown>;
  pullSecretNames:  string[];
  targetClusterIds: string[];
  targetNamespace:  string;
  library?:         'suse-ai' | 'nvidia';
}

// publishToFleetGit builds the Fleet Bundle YAML and commits it to the git repo
// configured in Settings. Returns the git commit hash.
export async function publishToFleetGit(params: GitPublishParams): Promise<string> {
  const yaml = buildFleetBundleYAML({
    bundleName:       params.bundleName,
    chartName:        params.chartName,
    chartVersion:     params.chartVersion,
    chartRepoUrl:     params.chartRepoUrl,
    helmSecretName:   params.helmSecretName,
    values:           params.values,
    pullSecretNames:  params.pullSecretNames,
    targetClusterIds: params.targetClusterIds,
    targetNamespace:  params.targetNamespace,
    library:          params.library,
  });

  const { commit } = await operatorPublish(params.bundleName, yaml);
  return commit;
}

package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/SUSE/aif/pkg/git"
)

// gitRepoEngine is the production FleetGitRepoEngine. Holds:
//   - client.Client for SSA on fleet.cattle.io/v1alpha1 GitRepo CRs.
//   - git.Engine for pushing rendered manifests to the remote.
//   - settings (FleetSettings) under a RWMutex: UpdateSettings is rare,
//     Apply is frequent; sole-writer pattern matches the Bundle engine.
type gitRepoEngine struct {
	logger *slog.Logger
	client client.Client
	git    git.Engine

	settingsMu sync.RWMutex
	settings   FleetSettings
}

// NewGitRepoEngine constructs the production FleetGitRepoEngine. Panics
// if c or g is nil — both are required collaborators (the client SSAs
// the GitRepo CR; the git.Engine pushes the manifest tree). Failing at
// construction beats a nil-deref at the first Apply.
func NewGitRepoEngine(logger *slog.Logger, c client.Client, g git.Engine) FleetGitRepoEngine {
	if c == nil {
		panic("fleet.NewGitRepoEngine: client.Client is nil")
	}
	if g == nil {
		panic("fleet.NewGitRepoEngine: git.Engine is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &gitRepoEngine{logger: logger, client: c, git: g}
}

func (e *gitRepoEngine) UpdateSettings(s FleetSettings) {
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	e.settings = s
	// Forward to the underlying git.Engine so it sees credentials and
	// repo/branch on the next Push. The constructor panics on nil git,
	// so no nil-check is needed here (fast-fail at construction beats
	// silent miss at first push).
	e.git.UpdateSettings(git.EngineSettings{
		RepoURL: s.GitRepoURL,
		Branch:  s.GitBranch,
		Auth:    s.GitAuth,
	})
}

func (e *gitRepoEngine) snapshot() FleetSettings {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.settings
}

// Apply is the engine's main entry point. Steps:
//  1. Validate.
//  2. Build one ManifestSubtree per cluster, single Push for the lot.
//  3. SSA each GitRepo CR with the stable field-manager owner.
//  4. Read back each GitRepo CR; mirror status into PerCluster.
//
// Fails fast on the first SSA conflict or apply error and returns a
// zero GitRepoObservedStatus (mirroring bundleEngine.Apply). The caller
// in pkg/workload/deployer.go already discards the observed status on
// error, so partial PerCluster slices would have no consumer; returning
// zero keeps the contract honest. If a downstream reducer ever needs
// per-cluster failure detail, switch this to collect-all-errors at the
// same time.
func (e *gitRepoEngine) Apply(ctx context.Context, spec GitRepoDeploymentSpec) (GitRepoObservedStatus, error) {
	if err := validateGitRepoSpec(spec); err != nil {
		return GitRepoObservedStatus{}, fmt.Errorf("%w: %v", ErrGitRepoInvalidSpec, err)
	}
	s := e.snapshot()
	if s.GitRepoURL == "" {
		// Fail closed: the engine has not yet received credentials from
		// SettingsReconciler. Surface as git.ErrNotConfigured so callers
		// can distinguish "settings not pushed" from "remote unreachable".
		return GitRepoObservedStatus{}, git.ErrNotConfigured
	}

	subtrees := make([]git.ManifestSubtree, 0, len(spec.TargetClusters))
	for _, c := range spec.TargetClusters {
		sub, err := BuildManifestTree(spec, c)
		if err != nil {
			return GitRepoObservedStatus{}, fmt.Errorf("build subtree for %q: %w", c, err)
		}
		subtrees = append(subtrees, sub)
	}

	if _, err := e.git.Push(ctx, git.PushRequest{
		Subtrees:      subtrees,
		CommitMessage: fmt.Sprintf("aif: apply workload %s", spec.WorkloadID),
		AuthorName:    "AIF Operator",
		AuthorEmail:   "aif-operator@suse.com",
	}); err != nil {
		return GitRepoObservedStatus{}, fmt.Errorf("git push: %w", err)
	}

	out := GitRepoObservedStatus{PerCluster: make([]ClusterDeploymentObserved, 0, len(spec.TargetClusters))}
	for _, cluster := range spec.TargetClusters {
		gr, err := buildGitRepoCR(spec, cluster, s)
		if err != nil {
			return GitRepoObservedStatus{}, fmt.Errorf("build GitRepo CR for %q: %w", cluster, err)
		}
		// Server-side-apply: idempotent on identical spec, surfaces conflicts
		// cleanly. ForceOwnership lets us reclaim fields if a previous run was
		// interrupted with a different field manager.
		//
		// TODO: migrate to client.Client.Apply() once an ApplyConfiguration is
		// available for fleetv1.GitRepo (controller-runtime v0.23.3 deprecates
		// the client.Apply Patch constant in favour of the typed API). Mirrors
		// the outstanding migration in pkg/fleet/bundle_engine.go and
		// internal/api/settings.go.
		if err := e.client.Patch(ctx, gr, client.Apply, //nolint:staticcheck // SA1019: see TODO above
			client.FieldOwner(fieldManager),
			client.ForceOwnership); err != nil {
			if apierrors.IsConflict(err) {
				return GitRepoObservedStatus{}, fmt.Errorf("%w: %s/%s: %v", ErrGitRepoConflict, gr.Namespace, gr.Name, err)
			}
			return GitRepoObservedStatus{}, fmt.Errorf("%w: %s/%s: %v", ErrGitRepoApplyFailed, gr.Namespace, gr.Name, err)
		}

		var got fleetv1.GitRepo
		if err := e.client.Get(ctx, client.ObjectKeyFromObject(gr), &got); err != nil {
			return GitRepoObservedStatus{}, fmt.Errorf("readback GitRepo %s/%s: %w", gr.Namespace, gr.Name, err)
		}
		out.PerCluster = append(out.PerCluster, mirrorGitRepoStatus(got.Status, cluster))
	}
	return out, nil
}

// Teardown deletes every GitRepo CR labeled with this workload, across
// all clusters. Iterates and ignores NotFound so the call is idempotent.
func (e *gitRepoEngine) Teardown(ctx context.Context, namespace, workloadID string) error {
	var list fleetv1.GitRepoList
	if err := e.client.List(ctx, &list,
		client.InNamespace(namespace),
		client.MatchingLabels{labelWorkload: workloadID, labelManagedBy: managedByValue},
	); err != nil {
		return fmt.Errorf("list GitRepos for teardown: %w", err)
	}
	for i := range list.Items {
		gr := &list.Items[i]
		if err := e.client.Delete(ctx, gr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete GitRepo %s/%s: %w", gr.Namespace, gr.Name, err)
		}
	}
	return nil
}

// BuildManifestTree assembles the file set for one cluster's subtree.
// Exported so engine tests can inspect it without round-tripping through
// the git.Engine fake.
//
// Layout per the design's per-cluster path layout:
//
//	{Path}/fleet.yaml
//	{Path}/manifests/00-namespace.yaml
//	{Path}/manifests/{1N-..|1-N-..}-{component}.yaml  (one per component)
func BuildManifestTree(spec GitRepoDeploymentSpec, cluster string) (git.ManifestSubtree, error) {
	path := gitRepoPath(spec.WorkloadID, cluster)
	files := map[string][]byte{
		"fleet.yaml":                  fleetYAMLForBundle(spec.WorkloadNS),
		"manifests/00-namespace.yaml": namespaceYAML(spec.WorkloadNS),
	}
	seen := map[string]struct{}{}
	for i, c := range spec.Components {
		name := git.SanitizeComponentNameUnique(c.Name, seen)
		seen[name] = struct{}{}
		valuesYAML, err := yaml.Marshal(c.Values)
		if err != nil {
			return git.ManifestSubtree{}, fmt.Errorf("marshal component %q values: %w", c.Name, err)
		}
		// For P4-3 the per-component file is the raw values YAML; Fleet's
		// helm renderer applies it against the chart ref recorded in
		// fleet.yaml. Sufficient for the engine round-trip; richer
		// per-component manifest shape lands when the chart-render pipeline
		// moves into pkg/git (out of scope here).
		files["manifests/"+git.ManifestFilename(i, name)] = valuesYAML
	}
	return git.ManifestSubtree{Path: path, Files: files}, nil
}

func namespaceYAML(ns string) []byte {
	return []byte(fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", ns))
}

func fleetYAMLForBundle(ns string) []byte {
	return []byte(fmt.Sprintf("defaultNamespace: %s\n", ns))
}

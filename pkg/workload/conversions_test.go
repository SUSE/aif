package workload

import (
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWorkloadToDeployRequest_AppSource(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec: aifv1.WorkloadSpec{
			Name: "my-llm",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App:  &aifv1.AppRef{Repo: "oci://r", Chart: "c", Version: "1.0"},
			},
			ValueOverrides: map[string]string{"my-llm": "replicaCount: 5"},
		},
	}

	req := WorkloadToDeployRequest(w, nil)

	if req.Namespace != "ns" || req.ID != "wid" || req.SpecName != "my-llm" {
		t.Errorf("got %+v, want ns/wid/my-llm", req)
	}
	if req.Source.Kind != SourceKindApp {
		t.Errorf("Source.Kind=%q, want App", req.Source.Kind)
	}
	if req.Source.App == nil || req.Source.App.Chart != "c" {
		t.Errorf("Source.App=%+v", req.Source.App)
	}
	if req.Replicas != 1 {
		t.Errorf("Replicas=%d, want default 1", req.Replicas)
	}
	if got := req.Overrides["my-llm"]; got != "replicaCount: 5" {
		t.Errorf("Overrides[my-llm]=%q", got)
	}
}

func TestWorkloadToDeployRequest_ReplicasOverride(t *testing.T) {
	r := int32(7)
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec: aifv1.WorkloadSpec{
			Name:     "n",
			Replicas: &r,
			Source:   aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
		},
	}
	if got := WorkloadToDeployRequest(w, nil).Replicas; got != 7 {
		t.Errorf("Replicas=%d, want 7", got)
	}
}

func TestWorkloadToDeployRequest_BlueprintSource(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec: aifv1.WorkloadSpec{
			Name: "n",
			Source: aifv1.WorkloadSource{
				Kind:      aifv1.WorkloadSourceKindBlueprint,
				Blueprint: &aifv1.BlueprintRef{Name: "rag", Version: "1.2.0"},
			},
		},
	}
	req := WorkloadToDeployRequest(w, nil)
	if req.Source.Kind != SourceKindBlueprint || req.Source.Blueprint == nil ||
		req.Source.Blueprint.Name != "rag" || req.Source.Blueprint.Version != "1.2.0" {
		t.Errorf("Source=%+v", req.Source)
	}
}

func TestWorkloadToDeployRequest_PreviousFromStatus(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "wid"},
		Spec:       aifv1.WorkloadSpec{Name: "n", Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}}},
		Status: aifv1.WorkloadStatus{
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "c1", ReleaseName: "wid-c1", Status: "deployed", Revision: 3},
			},
		},
	}
	req := WorkloadToDeployRequest(w, nil)
	if len(req.Previous) != 1 || req.Previous[0].Name != "c1" || req.Previous[0].Revision != 3 {
		t.Errorf("Previous=%+v", req.Previous)
	}
}

func TestApplyDeployResult_WritesComponents(t *testing.T) {
	// Pre-set Phase to assert ApplyDeployResult does NOT overwrite it
	// (the controller owns Phase via RecomputePhase, post-P5-1).
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhaseDeploying}}
	r := DeployResult{
		Components: []ComponentRelease{
			{Name: "c1", ReleaseName: "wid-c1", ChartRef: "oci://x/c1:1", Status: "deployed", Revision: 2},
		},
	}

	ApplyDeployResult(w, r)

	if w.Status.Phase != aifv1.WorkloadPhaseDeploying {
		t.Errorf("Phase=%q, want Deploying (ApplyDeployResult must not touch Phase)", w.Status.Phase)
	}
	if len(w.Status.ComponentReleases) != 1 || w.Status.ComponentReleases[0].Status != "deployed" {
		t.Errorf("ComponentReleases=%+v", w.Status.ComponentReleases)
	}
}

func TestApplyDeployResult_PreservesUnrelatedStatusFields(t *testing.T) {
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{
		Replicas:      3,
		ReadyReplicas: 2,
		Conditions: []metav1.Condition{
			{Type: conditions.TypeReady, Status: metav1.ConditionTrue, Reason: "X"},
		},
	}}

	ApplyDeployResult(w, DeployResult{})

	if w.Status.Replicas != 3 || w.Status.ReadyReplicas != 2 {
		t.Errorf("replicas wiped: %d/%d", w.Status.Replicas, w.Status.ReadyReplicas)
	}
	if len(w.Status.Conditions) != 1 {
		t.Errorf("Conditions wiped: %+v", w.Status.Conditions)
	}
}

func TestApplyDeployResult_WritesPerCluster(t *testing.T) {
	w := &aifv1.Workload{}
	r := DeployResult{
		PerCluster: []ClusterDeploymentStatusDomain{
			{ClusterName: "prod-east", Phase: ClusterRunning, FleetState: "Ready"},
			{ClusterName: "prod-west", Phase: ClusterDeploying, FleetState: "Pending"},
		},
	}

	ApplyDeployResult(w, r)

	if len(w.Status.PerCluster) != 2 {
		t.Fatalf("PerCluster len=%d, want 2", len(w.Status.PerCluster))
	}
	if w.Status.PerCluster[0].ClusterName != "prod-east" || w.Status.PerCluster[0].FleetState != "Ready" {
		t.Errorf("PerCluster[0]=%+v", w.Status.PerCluster[0])
	}
	if w.Status.PerCluster[1].Phase != string(ClusterDeploying) {
		t.Errorf("PerCluster[1].Phase=%q, want %q", w.Status.PerCluster[1].Phase, ClusterDeploying)
	}
}

// TestPhaseInputFromCR_PerClusterPhasesProjected verifies that the
// per-cluster phases written to status.perCluster by ApplyDeployResult
// round-trip back into PhaseInput.PerClusterPhases — the input Rule 0
// of RecomputePhase consumes to declare Fleet authoritative for the
// helm/Fleet path.
func TestPhaseInputFromCR_PerClusterPhasesProjected(t *testing.T) {
	w := &aifv1.Workload{
		Status: aifv1.WorkloadStatus{
			PerCluster: []aifv1.ClusterDeploymentStatus{
				{ClusterName: "prod-east", Phase: string(ClusterRunning), FleetState: "Ready"},
				{ClusterName: "prod-west", Phase: string(ClusterFailed), FleetState: "ErrApplied"},
			},
		},
	}
	in := PhaseInputFromCR(w)
	if len(in.PerClusterPhases) != 2 {
		t.Fatalf("PerClusterPhases len=%d, want 2", len(in.PerClusterPhases))
	}
	if in.PerClusterPhases[0] != ClusterRunning {
		t.Errorf("PerClusterPhases[0]=%q, want %q", in.PerClusterPhases[0], ClusterRunning)
	}
	if in.PerClusterPhases[1] != ClusterFailed {
		t.Errorf("PerClusterPhases[1]=%q, want %q", in.PerClusterPhases[1], ClusterFailed)
	}
}

// TestPhaseInputFromCR_NoPerClusterStays_Nil keeps the in-cluster Helm
// path's status.perCluster empty so Rule 0 is bypassed and Rules 1-6
// take over.
func TestPhaseInputFromCR_NoPerClusterStays_Nil(t *testing.T) {
	w := &aifv1.Workload{Status: aifv1.WorkloadStatus{}}
	in := PhaseInputFromCR(w)
	if in.PerClusterPhases != nil {
		t.Errorf("PerClusterPhases=%v, want nil for empty status.perCluster", in.PerClusterPhases)
	}
}

func TestPhaseInputFromCR_Defaults(t *testing.T) {
	w := &aifv1.Workload{
		Spec: aifv1.WorkloadSpec{
			Name: "n",
			Source: aifv1.WorkloadSource{
				Kind: aifv1.WorkloadSourceKindApp,
				App:  &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"},
			},
			// Replicas nil, Strategy nil — all defaults apply.
		},
		Status: aifv1.WorkloadStatus{Phase: aifv1.WorkloadPhaseDeploying},
	}

	in := PhaseInputFromCR(w)

	// spec.replicas is nil → DesiredReplicas defaults to 0 (kubebuilder fills
	// 1 at admission; this fallback covers envtest paths without defaulting).
	if in.DesiredReplicas != 0 {
		t.Errorf("DesiredReplicas=%d, want 0 (default for nil spec.replicas)", in.DesiredReplicas)
	}
	// Pre-P5-2: status.readyReplicas=0 is synthesised to equal DesiredReplicas
	// so rule 4 fires for healthy deploys until the pod informer lands.
	if in.ReadyReplicas != in.DesiredReplicas {
		t.Errorf("ReadyReplicas=%d, want DesiredReplicas=%d (pre-P5-2 default)", in.ReadyReplicas, in.DesiredReplicas)
	}
	if in.FailureThreshold != DefaultFailureThreshold {
		t.Errorf("FailureThreshold=%d, want %d (default)", in.FailureThreshold, DefaultFailureThreshold)
	}
	if in.PriorPhase != PhaseDeploying {
		t.Errorf("PriorPhase=%q, want Deploying", in.PriorPhase)
	}
}

func TestPhaseInputFromCR_KubebuilderDefaultedReplicas(t *testing.T) {
	// Simulates envtest path: kubebuilder defaulting fills spec.replicas=1
	// at admission; PhaseInputFromCR must propagate that to DesiredReplicas
	// and synthesise ReadyReplicas to match (pre-P5-2 default).
	replicas := int32(1)
	w := &aifv1.Workload{
		Spec:   aifv1.WorkloadSpec{Replicas: &replicas},
		Status: aifv1.WorkloadStatus{},
	}
	in := PhaseInputFromCR(w)
	if in.DesiredReplicas != 1 {
		t.Errorf("DesiredReplicas=%d, want 1", in.DesiredReplicas)
	}
	if in.ReadyReplicas != 1 {
		t.Errorf("ReadyReplicas=%d, want 1 (pre-P5-2 default = DesiredReplicas)", in.ReadyReplicas)
	}
}

func TestPhaseInputFromCR_ReadsNestedFailureThreshold(t *testing.T) {
	threshold := int32(7)
	replicas := int32(4)
	w := &aifv1.Workload{
		Spec: aifv1.WorkloadSpec{
			Replicas: &replicas,
			Strategy: &aifv1.DeploymentStrategy{
				AutomaticRecovery: &aifv1.AutomaticRecoveryStrategy{
					Enabled:          true,
					FailureThreshold: &threshold,
				},
			},
		},
		Status: aifv1.WorkloadStatus{
			ReadyReplicas:        2,
			RecoveryFailureCount: 1,
			ComponentReleases: []aifv1.ComponentReleaseStatus{
				{Name: "c1", Status: "deployed"},
			},
		},
	}

	in := PhaseInputFromCR(w)

	if in.DesiredReplicas != 4 {
		t.Errorf("DesiredReplicas=%d, want 4", in.DesiredReplicas)
	}
	if in.ReadyReplicas != 2 {
		t.Errorf("ReadyReplicas=%d, want 2", in.ReadyReplicas)
	}
	if in.RecoveryFailureCount != 1 {
		t.Errorf("RecoveryFailureCount=%d, want 1", in.RecoveryFailureCount)
	}
	if in.FailureThreshold != 7 {
		t.Errorf("FailureThreshold=%d, want 7", in.FailureThreshold)
	}
	if len(in.Components) != 1 || in.Components[0].Name != "c1" {
		t.Errorf("Components=%+v", in.Components)
	}
}

func TestPhaseInputFromCR_AutomaticRecoveryEnabled(t *testing.T) {
	// Covers the full nil-chain guard around
	// Spec.Strategy.AutomaticRecovery.Enabled. The Enabled flag keys the
	// three branches of ARCHITECTURE.md §4.4 rule 2 — getting this default
	// wrong silently flips a Failed Workload to Degraded (or vice versa),
	// so we exercise every level of the nil chain.
	enabledTrue := true
	cases := []struct {
		name string
		spec aifv1.WorkloadSpec
		want bool
	}{
		{
			name: "Strategy nil → false",
			spec: aifv1.WorkloadSpec{},
			want: false,
		},
		{
			name: "Strategy non-nil, AutomaticRecovery nil → false",
			spec: aifv1.WorkloadSpec{
				Strategy: &aifv1.DeploymentStrategy{},
			},
			want: false,
		},
		{
			name: "AutomaticRecovery present, Enabled=false → false",
			spec: aifv1.WorkloadSpec{
				Strategy: &aifv1.DeploymentStrategy{
					AutomaticRecovery: &aifv1.AutomaticRecoveryStrategy{
						Enabled: false,
					},
				},
			},
			want: false,
		},
		{
			name: "AutomaticRecovery present, Enabled=true → true",
			spec: aifv1.WorkloadSpec{
				Strategy: &aifv1.DeploymentStrategy{
					AutomaticRecovery: &aifv1.AutomaticRecoveryStrategy{
						Enabled: enabledTrue,
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &aifv1.Workload{Spec: tc.spec}
			in := PhaseInputFromCR(w)
			if in.AutomaticRecoveryEnabled != tc.want {
				t.Errorf("AutomaticRecoveryEnabled=%v, want %v", in.AutomaticRecoveryEnabled, tc.want)
			}
		})
	}
}

func TestPhaseToCR_MapsAllPhases(t *testing.T) {
	cases := []struct {
		in   Phase
		want aifv1.WorkloadPhase
	}{
		{PhasePending, aifv1.WorkloadPhasePending},
		{PhaseDeploying, aifv1.WorkloadPhaseDeploying},
		{PhaseRunning, aifv1.WorkloadPhaseRunning},
		{PhaseDegraded, aifv1.WorkloadPhaseDegraded},
		{PhaseFailed, aifv1.WorkloadPhaseFailed},
		{PhaseRecoveryInProgress, aifv1.WorkloadPhaseRecoveryInProgress},
		{Phase(""), aifv1.WorkloadPhase("")},
	}
	for _, tc := range cases {
		if got := PhaseToCR(tc.in); got != tc.want {
			t.Errorf("PhaseToCR(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWorkloadToDeployRequest_PopulatesNewFields(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "n", Name: "x", UID: "u-1"},
		TypeMeta:   metav1.TypeMeta{APIVersion: "ai.suse.com/v1alpha1", Kind: "Workload"},
		Spec: aifv1.WorkloadSpec{
			Name:           "n",
			Source:         aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
			DeployStrategy: aifv1.DeployStrategyTypeHelm,
			TargetClusters: []string{"a", "b"},
		},
	}
	req := WorkloadToDeployRequest(w, []byte("creds"))
	if req.DeployStrategy != "helm" {
		t.Fatalf("DeployStrategy = %q", req.DeployStrategy)
	}
	if len(req.TargetClusters) != 2 {
		t.Fatalf("TargetClusters len = %d", len(req.TargetClusters))
	}
	if string(req.PullSecretData) != "creds" {
		t.Fatalf("PullSecretData not propagated")
	}
	if req.Owner.UID != "u-1" || req.Owner.Kind != "Workload" {
		t.Fatalf("Owner = %+v", req.Owner)
	}
}

func TestWorkloadToDeployRequest_DefaultsDeployStrategy(t *testing.T) {
	w := &aifv1.Workload{
		ObjectMeta: metav1.ObjectMeta{Namespace: "n", Name: "x"},
		Spec: aifv1.WorkloadSpec{
			Name:   "n",
			Source: aifv1.WorkloadSource{Kind: aifv1.WorkloadSourceKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "1"}},
		},
	}
	req := WorkloadToDeployRequest(w, nil)
	if req.DeployStrategy != "helm" {
		t.Fatalf("expected default helm, got %q", req.DeployStrategy)
	}
}

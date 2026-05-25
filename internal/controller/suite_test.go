package controller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	fleetv1 "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/internal/controller"
	"github.com/SUSE/aif/internal/manager"
	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/workload"
)

var (
	testEnv            *envtest.Environment
	k8sClient          client.Client
	cancelFn           context.CancelFunc
	settingsApplier    *controller.FakeSettingsApplier // P5-7: assert snapshot propagation
	fakeDeployer       *workload.FakeDeployer          // P4-2: Workload deployment test double
	workloadReconciler *controller.WorkloadReconciler  // P4-3b: lifted so Fleet-integration spec can swap Deployer field
)

func TestControllers(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest suite (run 'make test-controllers')")
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testEnv = &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Paths: []string{
				filepath.Join("..", "..", "charts", "aif-operator", "crds"),
				filepath.Join("..", "..", "test", "crds", "fleet"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    true,
		},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("testdata")},
		},
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(aifv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(fleetv1.AddToScheme(scheme.Scheme)).To(Succeed())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    testEnv.WebhookInstallOptions.LocalServingPort,
			Host:    testEnv.WebhookInstallOptions.LocalServingHost,
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	Expect(err).NotTo(HaveOccurred())

	Expect((&controller.BundleReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder("bundle-controller"),
	}).SetupWithManager(mgr)).To(Succeed())

	Expect((&controller.BlueprintReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder("blueprint-controller"),
		Manager:  blueprint.New(nil),
	}).SetupWithManager(mgr)).To(Succeed())

	fakeDeployer = &workload.FakeDeployer{}
	// P5-1: the envtest suite uses the K8s-backed repository (not the
	// in-memory FakeRepository) because reconciles are driven by real
	// CR Create/Update/Delete against envtest's apiserver — a fake repo
	// would diverge from the watch source. FakeRepository is exercised
	// in pkg-level unit tests.
	workloadReconciler = &controller.WorkloadReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorder("workload-controller"),
		Deployer:          fakeDeployer,
		Repository:        workload.NewK8sRepository(mgr.GetClient()).AsRepository(),
		OperatorNamespace: "aif",
	}
	Expect(workloadReconciler.SetupWithManager(mgr)).To(Succeed())

	settingsApplier = &controller.FakeSettingsApplier{}
	Expect((&controller.SettingsReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder("settings-controller"),
		Applier:  settingsApplier,
	}).SetupWithManager(mgr)).To(Succeed())

	Expect(manager.SetupWebhooks(mgr)).To(Succeed())

	var ctx context.Context
	ctx, cancelFn = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()

	k8sClient = mgr.GetClient()
	Eventually(func() error {
		return k8sClient.List(ctx, &aifv1.BundleList{})
	}, 30*time.Second).Should(Succeed())

	// P4-3b: Pre-create the operator namespace and the docker-config
	// pull-secret so WorkloadReconciler (which now fetches
	// suse-registry-creds from the operator namespace before deploying)
	// doesn't trip the PullSecretNotReady branch in existing specs.
	Expect(k8sClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "aif"},
	})).To(Succeed())
	Expect(k8sClient.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "suse-registry-creds", Namespace: "aif"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	})).To(Succeed())
})

var _ = AfterSuite(func() {
	cancelFn()
	Expect(testEnv.Stop()).To(Succeed())
})

// BeforeEach at suite level: clear the recording applier between every spec
// across every Describe block. Without this, leftover snapshots from one
// block could leak into another's introspection — load-bearing on Ginkgo
// declaration order otherwise.
var _ = BeforeEach(func() {
	if settingsApplier != nil {
		settingsApplier.Reset()
	}
	if fakeDeployer != nil {
		fakeDeployer.Reset()
	}
})

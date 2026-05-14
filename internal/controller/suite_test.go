package controller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/internal/controller"
	"github.com/SUSE/aif/internal/manager"
	"github.com/SUSE/aif/pkg/blueprint"
)

var (
	testEnv         *envtest.Environment
	k8sClient       client.Client
	cancelFn        context.CancelFunc
	settingsApplier *controller.FakeSettingsApplier // P5-7: assert snapshot propagation
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
			Paths:              []string{filepath.Join("..", "..", "charts", "aif-operator", "crds")},
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

	Expect((&controller.WorkloadReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder("workload-controller"),
	}).SetupWithManager(mgr)).To(Succeed())

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
})

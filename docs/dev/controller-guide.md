# Controller Developer Guide

How to write a Kubernetes controller for AIF. Every controller follows the same structure — this guide shows the canonical pattern with copy-pasteable skeletons.

For the full design rationale, see [ARCHITECTURE.md §8](../spec/ARCHITECTURE.md#8-controller-design).

---

## 1. Reconciler Struct

Every reconciler embeds `client.Client` and carries the dependencies it needs. Name it `{Kind}Reconciler`. Inject domain logic via an interface (the "port"), never a concrete struct.

```go
type ThingReconciler struct {
    client.Client
    Scheme   *runtime.Scheme
    Recorder events.EventRecorder
    Service  thing.Service // domain port — see pkg/<domain>/interface.go
}
```

**Imports:**

```go
import (
    "context"
    "fmt"

    aifv1 "github.com/SUSE/aif/api/v1alpha1"
    "github.com/SUSE/aif/pkg/conditions"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/client-go/tools/events"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
    "sigs.k8s.io/controller-runtime/pkg/log"
)
```

---

## 2. Reconcile Loop

The canonical 5-step pattern. Every controller follows this exact structure:

```go
const thingFinalizerName = "ai.suse.com/cleanup"

func (r *ThingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // controller-runtime injects a logr.Logger into the context — this is the
    // one exception to CLAUDE.md's "use log/slog exclusively" rule.
    logger := log.FromContext(ctx)

    // 1. Fetch the resource
    var obj aifv1.Thing
    if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
        if errors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        logger.Error(err, "failed to get Thing")
        return ctrl.Result{}, err
    }

    // 2. Handle deletion
    if !obj.DeletionTimestamp.IsZero() {
        return r.handleDeletion(ctx, &obj)
    }

    // 3. Ensure finalizer
    if !controllerutil.ContainsFinalizer(&obj, thingFinalizerName) {
        controllerutil.AddFinalizer(&obj, thingFinalizerName)
        if err := r.Update(ctx, &obj); err != nil {
            return ctrl.Result{}, err
        }
        return ctrl.Result{Requeue: true}, nil
    }

    // 4. Business logic
    if err := r.reconcile(ctx, &obj); err != nil {
        logger.Error(err, "reconciliation failed")
        return ctrl.Result{}, err
    }

    // 5. Update status (always, even on error path above)
    if err := r.Status().Update(ctx, &obj); err != nil {
        logger.Error(err, "failed to update status")
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

**Key rules:**
- The business-logic helper (step 4) sets conditions on the in-memory object and returns `nil` even on validation failure — this ensures step 5 always runs and persists the conditions. Only return an error from `reconcile()` for transient failures that should trigger a requeue.
- Return `ctrl.Result{RequeueAfter: 30 * time.Second}` for in-progress deployments (see `WorkloadReconciler`).
- Never call `r.Status().Update` inside `reconcile()` — keep the single status write in the outer function.

---

## 3. Finalizer + Cleanup

The finalizer constant is `ai.suse.com/cleanup`. It is added on first reconcile (step 3 above) and removed only after cleanup completes:

```go
func (r *ThingReconciler) handleDeletion(ctx context.Context, obj *aifv1.Thing) (ctrl.Result, error) {
    if !controllerutil.ContainsFinalizer(obj, thingFinalizerName) {
        return ctrl.Result{}, nil
    }

    // Run cleanup logic here (e.g. delete owned Helm releases, remove Secrets)

    controllerutil.RemoveFinalizer(obj, thingFinalizerName)
    if err := r.Update(ctx, obj); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

---

## 4. Status Conditions

Use `conditions.Set()` (wraps `meta.SetStatusCondition`) — never hand-roll condition merging. This preserves `LastTransitionTime` when status hasn't changed.

```go
func (r *ThingReconciler) reconcile(ctx context.Context, obj *aifv1.Thing) error {
    if err := r.Service.Validate(ctx, obj); err != nil {
        conditions.Set(&obj.Status.Conditions, metav1.Condition{
            Type:               conditions.TypeReady,
            Status:             metav1.ConditionFalse,
            Reason:             conditions.ReasonInvalidSpec,
            Message:            fmt.Sprintf("Validation failed: %v", err),
            ObservedGeneration: obj.Generation,
        })
        r.Recorder.Eventf(obj, nil, "Warning", conditions.ReasonInvalidSpec,
            conditions.ActionValidating, err.Error())
        obj.Status.ObservedGeneration = obj.Generation
        return nil // don't requeue — user must fix spec
    }

    conditions.Set(&obj.Status.Conditions, metav1.Condition{
        Type:               conditions.TypeReady,
        Status:             metav1.ConditionTrue,
        Reason:             conditions.ReasonReconciled,
        Message:            "Thing successfully reconciled",
        ObservedGeneration: obj.Generation,
    })
    obj.Status.ObservedGeneration = obj.Generation
    return nil
}
```

**Available constants** — see [`pkg/conditions/types.go`](../../pkg/conditions/types.go):
- Types: `TypeReady`, `TypeProgressing`, `TypeDegraded`
- Reasons: `ReasonReconciled`, `ReasonInvalidSpec`, `ReasonSecretNotFound`, and others
- Actions: `ActionValidating`, `ActionReconciling`, `ActionDeleting`, and others (see [`pkg/conditions/actions.go`](../../pkg/conditions/actions.go))

---

## 5. RBAC Markers

Every controller declares its RBAC via kubebuilder markers above the `Reconcile` method. The standard set for a namespaced CRD:

```go
// +kubebuilder:rbac:groups=ai.suse.com,resources=things,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.suse.com,resources=things/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=things/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
```

Run `make manifests` after adding or changing markers — this regenerates the `ClusterRole` in the Helm chart.

---

## 6. SetupWithManager

### Basic (single resource)

```go
func (r *ThingReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&aifv1.Thing{}).
        Complete(r)
}
```

### Cross-resource watch

When your controller needs to react to changes in a different CRD (e.g. BlueprintReconciler watches Workloads to maintain `deploymentCount`):

```go
func (r *ThingReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&aifv1.Thing{}).
        Watches(
            &aifv1.Other{},
            handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
                o := obj.(*aifv1.Other)
                // Map the Other to the Thing(s) it affects
                return []reconcile.Request{{
                    NamespacedName: types.NamespacedName{Name: o.Spec.ThingRef},
                }}
            }),
            builder.WithPredicates(predicate.Funcs{
                CreateFunc:  func(e event.CreateEvent) bool { return true },
                DeleteFunc:  func(e event.DeleteEvent) bool { return true },
                UpdateFunc:  func(e event.UpdateEvent) bool { return false },
                GenericFunc: func(e event.GenericEvent) bool { return false },
            }),
        ).
        Complete(r)
}
```

See [`internal/controller/blueprint_controller.go`](../../internal/controller/blueprint_controller.go) for the real example.

---

## 7. Test Scaffold

Controller integration tests use **Ginkgo + Gomega + envtest**. The shared suite setup lives in [`internal/controller/suite_test.go`](../../internal/controller/suite_test.go).

### Test file structure

```go
package controller_test

import (
    "context"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    aifv1 "github.com/SUSE/aif/api/v1alpha1"
    "github.com/SUSE/aif/pkg/conditions"
)

var _ = Describe("ThingReconciler", func() {
    const timeout = 30 * time.Second
    const interval = 250 * time.Millisecond
    ctx := context.Background()

    findReady := func(conds []metav1.Condition) *metav1.Condition {
        for i := range conds {
            if conds[i].Type == conditions.TypeReady {
                return &conds[i]
            }
        }
        return nil
    }

    It("should reconcile a valid Thing to Ready=True", func() {
        obj := &aifv1.Thing{
            ObjectMeta: metav1.ObjectMeta{
                Name:      "test-thing",
                Namespace: "default",
            },
            Spec: aifv1.ThingSpec{ /* minimal valid spec */ },
        }
        Expect(k8sClient.Create(ctx, obj)).To(Succeed())

        Eventually(func(g Gomega) {
            var fetched aifv1.Thing
            g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &fetched)).To(Succeed())
            rc := findReady(fetched.Status.Conditions)
            g.Expect(rc).NotTo(BeNil())
            g.Expect(rc.Status).To(Equal(metav1.ConditionTrue))
            g.Expect(rc.Reason).To(Equal(conditions.ReasonReconciled))
        }, timeout, interval).Should(Succeed())

        // Verify finalizer was added
        var fetched aifv1.Thing
        Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &fetched)).To(Succeed())
        Expect(fetched.Finalizers).To(ContainElement("ai.suse.com/cleanup"))
    })
})
```

### Running tests

```bash
make test-controllers    # sets KUBEBUILDER_ASSETS and runs Ginkgo
```

---

## 8. Reference Files

| Pattern | File | Notes |
|---------|------|-------|
| Full reconciler (validation, self-healing) | `internal/controller/bundle_controller.go` | Best starting point |
| Minimal reconciler | `internal/controller/workload_controller.go` | Simplest example |
| Cross-resource watch | `internal/controller/blueprint_controller.go` | `Watches` + predicates |
| Settings propagation | `internal/controller/settings_controller.go` | Engine push pattern |
| Condition constants | `pkg/conditions/types.go` | All Type/Reason strings |
| Condition helper | `pkg/conditions/set.go` | `conditions.Set()` wrapper |
| Test suite setup | `internal/controller/suite_test.go` | Shared envtest bootstrap |
| Condition actions | `pkg/conditions/actions.go` | Event action strings |
| Test assertions | `internal/controller/bundle_controller_test.go` | `Eventually` + `Gomega` |

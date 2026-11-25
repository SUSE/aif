/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aiworkload

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func factsClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := kruntime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := networkingv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	s.AddKnownTypeWithName(mgmtNodeGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(mgmtNodeGVK.GroupVersion().WithKind("NodeList"), &unstructured.UnstructuredList{})
	s.AddKnownTypeWithName(mgmtClusterGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(provClusterGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(provClusterGVK.GroupVersion().WithKind("ClusterList"), &unstructured.UnstructuredList{})
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func node(name string, addrs ...corev1.NodeAddress) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.NodeStatus{Addresses: addrs}}
}

func addr(t corev1.NodeAddressType, a string) corev1.NodeAddress {
	return corev1.NodeAddress{Type: t, Address: a}
}

func TestIngressIP_LocalPrefersExternal(t *testing.T) {
	c := factsClient(t,
		node("b", addr(corev1.NodeInternalIP, "10.0.0.2"), addr(corev1.NodeExternalIP, "1.2.3.4")),
		node("a", addr(corev1.NodeInternalIP, "10.0.0.1")),
	)
	got, err := rancherClusterFacts{Client: c}.IngressIP(context.Background(), "local")
	if err != nil || got != "1.2.3.4" {
		t.Fatalf("IngressIP = %q, %v; want 1.2.3.4", got, err)
	}
}

func TestIngressIP_LocalInternalFallbackSorted(t *testing.T) {
	c := factsClient(t,
		node("b", addr(corev1.NodeInternalIP, "10.0.0.2")),
		node("a", addr(corev1.NodeInternalIP, "10.0.0.1")),
	)
	got, err := rancherClusterFacts{Client: c}.IngressIP(context.Background(), "local")
	if err != nil || got != "10.0.0.1" {
		t.Fatalf("IngressIP = %q, %v; want 10.0.0.1", got, err)
	}
}

func TestIngressIP_LocalNoNodesErrors(t *testing.T) {
	if _, err := (rancherClusterFacts{Client: factsClient(t)}).IngressIP(context.Background(), "local"); err == nil {
		t.Fatal("want error with no node addresses")
	}
}

func lbService(ns, name, appName, ip string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: map[string]string{"app.kubernetes.io/name": appName}},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: ip}},
		}},
	}
}

func TestIngressIP_LoadBalancerWins(t *testing.T) {
	unrelated := lbService("other", "svc", "my-app", "9.9.9.9")
	clusterIP := lbService("kube-system", "internal", "rke2-ingress-nginx", "7.7.7.7")
	clusterIP.Spec.Type = corev1.ServiceTypeClusterIP
	c := factsClient(t,
		node("a", addr(corev1.NodeExternalIP, "1.2.3.4")),
		unrelated,
		clusterIP,
		lbService("kube-system", "rke2-ingress-nginx-controller", "rke2-ingress-nginx", "5.6.7.8"),
	)
	got, err := rancherClusterFacts{Client: c}.IngressIP(context.Background(), "local")
	if err != nil || got != "5.6.7.8" {
		t.Fatalf("IngressIP = %q, %v; want 5.6.7.8", got, err)
	}
}

func TestIngressIP_PendingLoadBalancerWaits(t *testing.T) {
	pending := lbService("kube-system", "rke2-ingress-nginx-controller", "rke2-ingress-nginx", "")
	pending.Status.LoadBalancer.Ingress = nil
	c := factsClient(t, node("a", addr(corev1.NodeExternalIP, "1.2.3.4")), pending)
	_, err := rancherClusterFacts{Client: c}.IngressIP(context.Background(), "local")
	var pe *ingressIPPendingError
	if !errors.Is(err, errIngressIPPending) || !errors.As(err, &pe) || !strings.Contains(err.Error(), "kube-system/rke2-ingress-nginx-controller") || pe.FallbackIP != "1.2.3.4" {
		t.Fatalf("IngressIP err = %v; want errIngressIPPending naming the Service with node IP fallback 1.2.3.4", err)
	}

	failing := factsClient(t, pending.DeepCopy())
	listErr := rancherClusterFacts{Client: interceptor.NewClient(failing.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.NodeList); ok {
				return errors.New("timeout")
			}
			return c.List(ctx, list, opts...)
		},
	})}
	_, err = listErr.IngressIP(context.Background(), "local")
	if !errors.As(err, &pe) || pe.FallbackErr == nil || !strings.Contains(pe.FallbackErr.Error(), "timeout") {
		t.Fatalf("node List error during a wait: err = %v; want still pending with FallbackErr", err)
	}

	hostnameOnly := lbService("kube-system", "rke2-ingress-nginx-controller", "rke2-ingress-nginx", "")
	hostnameOnly.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}}
	c = factsClient(t, node("a", addr(corev1.NodeExternalIP, "1.2.3.4")), hostnameOnly)
	if got, err := (rancherClusterFacts{Client: c}).IngressIP(context.Background(), "local"); err != nil || got != "1.2.3.4" {
		t.Fatalf("hostname-only LB: IngressIP = %q, %v; want node IP fallback", got, err)
	}
}

func mgmtNode(ns, name string, addrs map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(mgmtNodeGVK)
	u.SetNamespace(ns)
	u.SetName(name)
	var list []any
	for _, typ := range []string{"InternalIP", "ExternalIP"} {
		if a, ok := addrs[typ]; ok {
			list = append(list, map[string]any{"type": typ, "address": a})
		}
	}
	_ = unstructured.SetNestedSlice(u.Object, list, "status", "internalNodeStatus", "addresses")
	return u
}

func TestIngressIP_Downstream(t *testing.T) {
	c := factsClient(t,
		mgmtNode("c-abc", "m-b", map[string]string{"InternalIP": "10.1.0.2", "ExternalIP": "4.3.2.1"}),
		mgmtNode("c-abc", "m-a", map[string]string{"InternalIP": "10.1.0.1"}),
		mgmtNode("c-other", "m-0", map[string]string{"ExternalIP": "8.8.8.8"}),
	)
	got, err := rancherClusterFacts{Client: c}.IngressIP(context.Background(), "c-abc")
	if err != nil || got != "4.3.2.1" {
		t.Fatalf("IngressIP = %q, %v; want 4.3.2.1", got, err)
	}

	c = factsClient(t,
		mgmtNode("c-abc", "m-b", map[string]string{"InternalIP": "10.1.0.2"}),
		mgmtNode("c-abc", "m-a", map[string]string{"InternalIP": "10.1.0.1"}),
	)
	got, err = rancherClusterFacts{Client: c}.IngressIP(context.Background(), "c-abc")
	if err != nil || got != "10.1.0.1" {
		t.Fatalf("IngressIP = %q, %v; want 10.1.0.1", got, err)
	}
}

func ingressClass(name string, isDefault bool) *networkingv1.IngressClass {
	ic := &networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if isDefault {
		ic.Annotations = map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"}
	}
	return ic
}

func TestDefaultIngressClass_Local(t *testing.T) {
	ctx := context.Background()
	got, err := rancherClusterFacts{Client: factsClient(t, ingressClass(classNginx, true), ingressClass(classTraefik, false))}.DefaultIngressClass(ctx, "local")
	if err != nil || got != classNginx {
		t.Fatalf("annotated default: got %q, %v", got, err)
	}
	got, err = rancherClusterFacts{Client: factsClient(t, ingressClass(classTraefik, false))}.DefaultIngressClass(ctx, "local")
	if err != nil || got != classTraefik {
		t.Fatalf("single class: got %q, %v", got, err)
	}
	_, err = rancherClusterFacts{Client: factsClient(t, ingressClass(classNginx, false), ingressClass(classTraefik, false))}.DefaultIngressClass(ctx, "local")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("two classes without default: want ambiguous error, got %v", err)
	}
	if _, err = (rancherClusterFacts{Client: factsClient(t)}).DefaultIngressClass(ctx, "local"); err == nil {
		t.Fatal("no classes: want error")
	}
}

func provCluster(name, clusterName, k8sVersion string, ingress any, rke bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(provClusterGVK)
	u.SetNamespace("fleet-default")
	u.SetName(name)
	_ = unstructured.SetNestedField(u.Object, clusterName, "status", "clusterName")
	if k8sVersion != "" {
		_ = unstructured.SetNestedField(u.Object, k8sVersion, "spec", "kubernetesVersion")
	}
	if rke {
		mgc := map[string]any{"cni": "calico"}
		if ingress != nil {
			mgc["ingress-controller"] = ingress
		}
		_ = unstructured.SetNestedField(u.Object, mgc, "spec", "rkeConfig", "machineGlobalConfig")
	}
	return u
}

func TestDefaultIngressClass_DownstreamRKE2(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		version string
		ingress any
		want    string
	}{
		{classTraefik, "v1.33.5+rke2r1", classTraefik, classTraefik},
		{"ingress-nginx", "v1.33.5+rke2r1", "ingress-nginx", classNginx},
		{"list first wins", "v1.33.5+rke2r1", []any{classTraefik, "ingress-nginx"}, classTraefik},
		{"absent", "v1.33.5+rke2r1", nil, classNginx},
		{"absent no version", "", nil, classNginx},
		{"absent rke2 1.36", "v1.36.0+rke2r1", nil, classTraefik},
		{"absent k3s", "v1.33.5+k3s1", nil, classTraefik},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := factsClient(t,
				provCluster("other", "c-zzz", "v1.33.5+rke2r1", classTraefik, true),
				provCluster("gpu", "c-abc", tc.version, tc.ingress, true),
			)
			got, err := rancherClusterFacts{Client: c}.DefaultIngressClass(ctx, "c-abc")
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}

	noMGC := provCluster("gpu", "c-abc", "v1.33.5+rke2r1", nil, false)
	_ = unstructured.SetNestedMap(noMGC.Object, map[string]any{}, "spec", "rkeConfig")
	if got, err := (rancherClusterFacts{Client: factsClient(t, noMGC)}).DefaultIngressClass(ctx, "c-abc"); err != nil || got != classNginx {
		t.Fatalf("rkeConfig without machineGlobalConfig: got %q, %v; want nginx", got, err)
	}

	for name, c := range map[string]client.Client{
		"no rkeConfig":       factsClient(t, provCluster("imported", "c-abc", "", nil, false)),
		"no matching":        factsClient(t, provCluster("other", "c-zzz", "", classTraefik, true)),
		"ingress none":       factsClient(t, provCluster("gpu", "c-abc", "", "none", true)),
		"unknown controller": factsClient(t, provCluster("gpu", "c-abc", "", "contour", true)),
	} {
		_, err := rancherClusterFacts{Client: c}.DefaultIngressClass(ctx, "c-abc")
		if err == nil || !strings.Contains(err.Error(), "set the ingress class explicitly") {
			t.Errorf("%s: want error telling the user to set the class explicitly, got %v", name, err)
		}
	}
}

func mgmtCluster(name, driver, provider, gitVersion string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(mgmtClusterGVK)
	u.SetName(name)
	if driver != "" {
		_ = unstructured.SetNestedField(u.Object, driver, "status", "driver")
	}
	if provider != "" {
		_ = unstructured.SetNestedField(u.Object, provider, "status", "provider")
	}
	if gitVersion != "" {
		_ = unstructured.SetNestedField(u.Object, gitVersion, "status", "version", "gitVersion")
	}
	return u
}

func TestDefaultIngressClass_DownstreamImported(t *testing.T) {
	ctx := context.Background()
	imported := provCluster("imported", "c-abc", "", nil, false)
	cases := []struct {
		name string
		objs []client.Object
		want string
	}{
		{"rke2 1.34", []client.Object{imported, mgmtCluster("c-abc", "rke2", "rke2", "v1.34.4+rke2r1")}, classNginx},
		{"rke2 1.36", []client.Object{imported, mgmtCluster("c-abc", "rke2", "rke2", "v1.36.1+rke2r1")}, classTraefik},
		{"k3s", []client.Object{imported, mgmtCluster("c-abc", "k3s", "k3s", "v1.34.2+k3s1")}, classTraefik},
		{"k3s no version", []client.Object{imported, mgmtCluster("c-abc", "k3s", "k3s", "")}, classTraefik},
		{"provider fallback", []client.Object{imported, mgmtCluster("c-abc", "", "rke2", "v1.34.4+rke2r1")}, classNginx},
		{"no provisioning cluster", []client.Object{mgmtCluster("c-abc", "rke2", "rke2", "v1.34.4+rke2r1")}, classNginx},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rancherClusterFacts{Client: factsClient(t, tc.objs...)}.DefaultIngressClass(ctx, "c-abc")
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name string
		objs []client.Object
		want string
	}{
		{"eks", []client.Object{imported, mgmtCluster("c-abc", "EKS", "eks", "v1.34.1-eks-1")}, `not an RKE2/K3s cluster (driver "EKS")`},
		{"generic", []client.Object{imported, mgmtCluster("c-abc", "imported", "", "v1.34.1")}, `not an RKE2/K3s cluster (driver "imported")`},
		{"record missing", []client.Object{imported, mgmtCluster("c-zzz", "rke2", "rke2", "v1.34.4+rke2r1")}, "Rancher cluster record of cluster c-abc was not found"},
	} {
		_, err := rancherClusterFacts{Client: factsClient(t, tc.objs...)}.DefaultIngressClass(ctx, "c-abc")
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "set the ingress class explicitly") {
			t.Errorf("%s: want error containing %q and telling the user to set the class explicitly, got %v", tc.name, tc.want, err)
		}
	}

	forbidden := interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if obj.GetObjectKind().GroupVersionKind() == mgmtClusterGVK {
			return apierrors.NewForbidden(schema.GroupResource{Group: mgmtClusterGVK.Group, Resource: "clusters"}, key.Name, errors.New("denied"))
		}
		return c.Get(ctx, key, obj, opts...)
	}}
	c := fake.NewClientBuilder().WithScheme(factsClient(t).Scheme()).WithObjects(imported).WithInterceptorFuncs(forbidden).Build()
	if _, err := (rancherClusterFacts{Client: c}).DefaultIngressClass(ctx, "c-abc"); err == nil || !strings.Contains(err.Error(), "get Rancher cluster c-abc") || !apierrors.IsForbidden(err) {
		t.Errorf("forbidden: want wrapped Forbidden error from get Rancher cluster, got %v", err)
	}
}

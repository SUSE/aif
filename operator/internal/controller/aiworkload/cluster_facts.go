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
	stderrors "errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterFacts answers the cluster questions automatic Blueprint values need.
type ClusterFacts interface {
	IngressIP(ctx context.Context, clusterID string) (string, error)
	DefaultIngressClass(ctx context.Context, clusterID string) (string, error)
}

var (
	mgmtNodeGVK    = schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Node"}
	mgmtClusterGVK = schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Cluster"}
	provClusterGVK = schema.GroupVersionKind{Group: "provisioning.cattle.io", Version: "v1", Kind: "Cluster"}
)

// errIngressIPPending marks an ingress IP that isn't known yet but will be.
var errIngressIPPending = stderrors.New("ingress IP pending")

// ingressIPPendingError reports an ingress LoadBalancer Service still being
// provisioned. A node IP would be pinned for good, so callers wait for a while
// before falling back to FallbackIP (empty, with FallbackErr, when there is none).
type ingressIPPendingError struct {
	Service     string
	FallbackIP  string
	FallbackErr error
}

func (e *ingressIPPendingError) Error() string {
	return fmt.Sprintf("ingress controller LoadBalancer Service %s has no external IP yet", e.Service)
}
func (e *ingressIPPendingError) Unwrap() error { return errIngressIPPending }

var ingressControllerAppNames = []string{"rke2-ingress-nginx", "ingress-nginx", "rke2-traefik", "traefik"}

const (
	defaultIngressClassAnnotation = "ingressclass.kubernetes.io/is-default-class"

	classNginx   = "nginx"
	classTraefik = "traefik"
)

// rancherClusterFacts reads the local cluster directly and downstream clusters
// through Rancher's management (Node, Cluster) and provisioning (Cluster) objects.
type rancherClusterFacts struct {
	Client client.Reader
}

func isLocalCluster(clusterID string) bool { return clusterID == "" || clusterID == "local" }

type nodeAddrs struct {
	name     string
	internal string
	external string
}

func (f rancherClusterFacts) IngressIP(ctx context.Context, clusterID string) (string, error) {
	var nodes []nodeAddrs
	pending := ""
	if isLocalCluster(clusterID) {
		ip, svc, err := f.localLoadBalancerIP(ctx)
		if err != nil || ip != "" {
			return ip, err
		}
		pending = svc
		var list corev1.NodeList
		if err := f.Client.List(ctx, &list); err != nil {
			err = fmt.Errorf("list nodes: %w", err)
			if pending != "" {
				return "", &ingressIPPendingError{Service: pending, FallbackErr: err}
			}
			return "", err
		}
		for _, n := range list.Items {
			na := nodeAddrs{name: n.Name}
			for _, a := range n.Status.Addresses {
				na.add(string(a.Type), a.Address)
			}
			nodes = append(nodes, na)
		}
	} else {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(mgmtNodeGVK.GroupVersion().WithKind("NodeList"))
		if err := f.Client.List(ctx, list, client.InNamespace(clusterID)); err != nil {
			return "", fmt.Errorf("list Rancher nodes of cluster %s: %w", clusterID, err)
		}
		for _, n := range list.Items {
			na := nodeAddrs{name: n.GetName()}
			addrs, _, _ := unstructured.NestedSlice(n.Object, "status", "internalNodeStatus", "addresses")
			for _, a := range addrs {
				m, _ := a.(map[string]any)
				typ, _ := m["type"].(string)
				address, _ := m["address"].(string)
				na.add(typ, address)
			}
			nodes = append(nodes, na)
		}
	}
	ip, err := pickNodeIP(nodes, clusterID)
	if pending != "" {
		return "", &ingressIPPendingError{Service: pending, FallbackIP: ip, FallbackErr: err}
	}
	return ip, err
}

// add keeps the first IPv4 address of each type; sslip.io names need dotted IPv4.
func (n *nodeAddrs) add(typ, address string) {
	if ip := net.ParseIP(address); ip == nil || ip.To4() == nil {
		return
	}
	switch typ {
	case string(corev1.NodeInternalIP):
		if n.internal == "" {
			n.internal = address
		}
	case string(corev1.NodeExternalIP):
		if n.external == "" {
			n.external = address
		}
	}
}

// pickNodeIP prefers any node's ExternalIP over InternalIPs, scanning nodes by name.
func pickNodeIP(nodes []nodeAddrs, clusterID string) (string, error) {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].name < nodes[j].name })
	for _, n := range nodes {
		if n.external != "" {
			return n.external, nil
		}
	}
	for _, n := range nodes {
		if n.internal != "" {
			return n.internal, nil
		}
	}
	return "", fmt.Errorf("no node IPv4 address found on cluster %s", clusterID)
}

// localLoadBalancerIP returns the ingress controller's LoadBalancer IP or,
// when there is none yet, the first LoadBalancer Service still being provisioned.
func (f rancherClusterFacts) localLoadBalancerIP(ctx context.Context) (ip, pending string, err error) {
	req, err := labels.NewRequirement("app.kubernetes.io/name", selection.In, ingressControllerAppNames)
	if err != nil {
		return "", "", err
	}
	var list corev1.ServiceList
	if err := f.Client.List(ctx, &list, client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*req)}); err != nil {
		return "", "", fmt.Errorf("list ingress controller services: %w", err)
	}
	sort.Slice(list.Items, func(i, j int) bool {
		a, b := list.Items[i], list.Items[j]
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	for _, s := range list.Items {
		if s.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		for _, in := range s.Status.LoadBalancer.Ingress {
			if in.IP != "" {
				return in.IP, "", nil
			}
		}
		if pending == "" && len(s.Status.LoadBalancer.Ingress) == 0 {
			pending = s.Namespace + "/" + s.Name
		}
	}
	return "", pending, nil
}

func (f rancherClusterFacts) DefaultIngressClass(ctx context.Context, clusterID string) (string, error) {
	if isLocalCluster(clusterID) {
		return f.localDefaultIngressClass(ctx)
	}
	return f.downstreamDefaultIngressClass(ctx, clusterID)
}

func (f rancherClusterFacts) localDefaultIngressClass(ctx context.Context) (string, error) {
	var list networkingv1.IngressClassList
	if err := f.Client.List(ctx, &list); err != nil {
		return "", fmt.Errorf("list IngressClasses: %w", err)
	}
	all := make([]string, 0, len(list.Items))
	var defaults []string
	for _, ic := range list.Items {
		all = append(all, ic.Name)
		if ic.Annotations[defaultIngressClassAnnotation] == "true" {
			defaults = append(defaults, ic.Name)
		}
	}
	sort.Strings(all)
	sort.Strings(defaults)
	switch {
	case len(defaults) == 1:
		return defaults[0], nil
	case len(defaults) > 1:
		return "", fmt.Errorf("default IngressClass is ambiguous: %s are all marked default; set the ingress class explicitly", strings.Join(defaults, ", "))
	case len(all) == 1:
		return all[0], nil
	case len(all) == 0:
		return "", fmt.Errorf("the local cluster has no IngressClass; set the ingress class explicitly")
	default:
		return "", fmt.Errorf("default IngressClass is ambiguous: %s exist and none is marked default; set the ingress class explicitly", strings.Join(all, ", "))
	}
}

func (f rancherClusterFacts) downstreamDefaultIngressClass(ctx context.Context, clusterID string) (string, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(provClusterGVK.GroupVersion().WithKind("ClusterList"))
	if err := f.Client.List(ctx, list, client.InNamespace("fleet-default")); err != nil {
		return "", fmt.Errorf("list provisioning clusters: %w", err)
	}
	for _, c := range list.Items {
		if name, _, _ := unstructured.NestedString(c.Object, "status", "clusterName"); name != clusterID {
			continue
		}
		if _, ok, _ := unstructured.NestedMap(c.Object, "spec", "rkeConfig"); !ok {
			break
		}
		mgc, _, _ := unstructured.NestedMap(c.Object, "spec", "rkeConfig", "machineGlobalConfig")
		version, _, _ := unstructured.NestedString(c.Object, "spec", "kubernetesVersion")
		controller := firstIngressController(mgc["ingress-controller"])
		if controller == "" {
			controller = defaultIngressController(version)
		}
		return ingressClassForController(clusterID, controller)
	}
	return f.importedDefaultIngressClass(ctx, clusterID)
}

// importedDefaultIngressClass covers clusters without rkeConfig (imported, hosted,
// RKE1); only RKE2/K3s ones are detectable, from their management Cluster.
func (f rancherClusterFacts) importedDefaultIngressClass(ctx context.Context, clusterID string) (string, error) {
	mc := &unstructured.Unstructured{}
	mc.SetGroupVersionKind(mgmtClusterGVK)
	if err := f.Client.Get(ctx, client.ObjectKey{Name: clusterID}, mc); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("the Rancher cluster record of cluster %s was not found, so its ingress controller can't be detected; set the ingress class explicitly in the install wizard's Values", clusterID)
		}
		return "", fmt.Errorf("get Rancher cluster %s: %w", clusterID, err)
	}
	driver, _, _ := unstructured.NestedString(mc.Object, "status", "driver")
	if driver == "" {
		driver, _, _ = unstructured.NestedString(mc.Object, "status", "provider")
	}
	switch strings.ToLower(driver) {
	case "k3s":
		return classTraefik, nil
	case "rke2":
		version, _, _ := unstructured.NestedString(mc.Object, "status", "version", "gitVersion")
		return ingressClassForController(clusterID, defaultIngressController(version))
	}
	return "", fmt.Errorf("cluster %s is not an RKE2/K3s cluster (driver %q), so its ingress controller can't be detected; set the ingress class explicitly in the install wizard's Values", clusterID, driver)
}

func ingressClassForController(clusterID, controller string) (string, error) {
	switch controller {
	case "ingress-nginx":
		return classNginx, nil
	case classTraefik:
		return classTraefik, nil
	}
	return "", fmt.Errorf("cluster %s runs ingress controller %q; set the ingress class explicitly", clusterID, controller)
}

// firstIngressController returns the configured controller; RKE2 accepts a list
// and makes the first entry the default class.
func firstIngressController(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) > 0 {
			s, _ := t[0].(string)
			return s
		}
	}
	return ""
}

// defaultIngressController mirrors the distribution defaults: K3s ships Traefik;
// RKE2 ships ingress-nginx through v1.35 and Traefik from v1.36 on
// (docs.rke2.io/networking/networking_services, checked 2026-11-06).
func defaultIngressController(kubernetesVersion string) string {
	if strings.Contains(kubernetesVersion, "k3s") {
		return classTraefik
	}
	parts := strings.SplitN(strings.TrimPrefix(kubernetesVersion, "v"), ".", 3)
	if len(parts) >= 2 {
		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		if major > 1 || (major == 1 && minor >= 36) {
			return classTraefik
		}
	}
	return "ingress-nginx"
}

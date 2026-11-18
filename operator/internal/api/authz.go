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

package api

import (
	"fmt"
	"log"
	"net/http"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The API authorizes each request forwarded by Rancher's APIService aggregation
// with a SubjectAccessReview for the impersonated user. Keep in sync with
// charts/aif-operator/templates/rbac/manager-role.yaml.
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

// Rule is the Kubernetes permission a route requires, checked against the
// ai-factory.suse.com API group.
type Rule struct {
	Verb, Resource string
	// Namespace is a fixed namespace to check in; "" means cluster scope
	// (or all namespaces for a namespaced resource).
	Namespace string
	// NamespaceFromPath and NameFromPath take the namespace and the object
	// name from the {namespace} and {name} path values.
	NamespaceFromPath, NameFromPath bool
}

// Authorizer is the API's router: it registers routes so that every request
// is authorized with a SubjectAccessReview for the caller identity (UserFrom)
// before the route's handler runs. A route without a rule cannot be
// registered.
//
// It owns the ServeMux and is itself the http.Handler that
// NewAggregatedHandler serves, so handlers (Handler.Register) and main never
// get a mux to register an unauthorized route on. Code in this package could
// still reach the mux field, so ServeHTTP also refuses any route that was not
// registered through HandleFunc.
type Authorizer struct {
	client client.Client
	rules  map[string]Rule
	// mux is written only by HandleFunc, which wraps every route in authorize.
	mux *http.ServeMux
	// registered holds the patterns registered through HandleFunc.
	registered map[string]bool
}

var _ http.Handler = (*Authorizer)(nil)

// NewAuthorizer returns an Authorizer that checks requests against rules,
// keyed by ServeMux pattern, using c to create SubjectAccessReviews.
func NewAuthorizer(c client.Client, rules map[string]Rule) *Authorizer {
	return &Authorizer{client: c, rules: rules, mux: http.NewServeMux(), registered: map[string]bool{}}
}

// Verbs and resources used by DefaultRules.
const (
	verbGet    = "get"
	verbList   = "list"
	verbCreate = "create"
	verbUpdate = "update"
	verbDelete = "delete"

	resourceSettings          = "settings"
	resourceBlueprints        = "blueprints"
	resourceBlueprintCatalogs = "blueprintcatalogs"
	resourceAIWorkloads       = "aiworkloads"
)

// DefaultRules is the authorization rule for every API route.
//
// The version and catalog routes require get on settings, so they are
// reserved to users who can read the operator configuration. The validation
// and git publish routes require update on settings because they act with
// the operator's stored credentials, which is equivalent to managing
// Settings.
//
// Every modification of an existing object requires update: saving Settings,
// editing a Blueprint or a workload, and the workload upgrade, rollback and
// retry actions. A permission therefore never depends on whether a handler
// issues an Update or a Patch to the Kubernetes API, which the operator does
// with its own identity.
func DefaultRules(operatorNamespace string) map[string]Rule {
	settings := func(verb string) Rule {
		return Rule{Verb: verb, Resource: resourceSettings, Namespace: operatorNamespace}
	}
	blueprint := func(verb string, byName bool) Rule {
		return Rule{Verb: verb, Resource: resourceBlueprints, NameFromPath: byName}
	}
	workload := func(verb string) Rule {
		return Rule{Verb: verb, Resource: resourceAIWorkloads, NamespaceFromPath: true, NameFromPath: true}
	}
	return map[string]Rule{
		"GET /api/v1/version":                         settings(verbGet),
		"GET /api/v1/settings":                        settings(verbGet),
		"PUT /api/v1/settings":                        settings(verbUpdate),
		"GET /api/v1/settings/registry-status":        settings(verbGet),
		"POST /api/v1/settings/validate-credentials":  settings(verbUpdate),
		"POST /api/v1/settings/validate-chart-access": settings(verbUpdate),
		"POST /api/v1/git/publish":                    settings(verbUpdate),
		"GET /api/v1/catalog":                         settings(verbGet),

		"GET /api/v1/blueprint-catalogs": {Verb: verbList, Resource: resourceBlueprintCatalogs},

		"GET /api/v1/blueprints":           blueprint(verbList, false),
		"GET /api/v1/blueprints/{name}":    blueprint(verbGet, true),
		"POST /api/v1/blueprints":          blueprint(verbCreate, false),
		"PUT /api/v1/blueprints/{name}":    blueprint(verbUpdate, true),
		"DELETE /api/v1/blueprints/{name}": blueprint(verbDelete, true),

		// An empty namespace on list means all namespaces.
		"GET /api/v1/aiworkloads":                                         {Verb: verbList, Resource: resourceAIWorkloads},
		"POST /api/v1/namespaces/{namespace}/aiworkloads":                 {Verb: verbCreate, Resource: resourceAIWorkloads, NamespaceFromPath: true},
		"PATCH /api/v1/namespaces/{namespace}/aiworkloads/{name}":         workload(verbUpdate),
		"DELETE /api/v1/namespaces/{namespace}/aiworkloads/{name}":        workload(verbDelete),
		"POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/upgrade":  workload(verbUpdate),
		"POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/rollback": workload(verbUpdate),
		"POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/retry":    workload(verbUpdate),
	}
}

// HandleFunc registers h for pattern behind the pattern's rule. It panics
// when pattern has no rule, so an unauthorized route fails at startup.
func (a *Authorizer) HandleFunc(pattern string, h http.HandlerFunc) {
	rule, ok := a.rules[pattern]
	if !ok {
		panic(fmt.Sprintf("api: no authorization rule for route %q", pattern))
	}
	a.registered[pattern] = true
	a.mux.HandleFunc(pattern, a.authorize(rule, h))
}

// ServeHTTP routes r to the handler registered for it; unknown routes get
// the ServeMux's 404 (or 405 for a known path with another method). A route
// registered on mux without HandleFunc, and so without authorization, fails
// closed with 500 before its handler runs.
func (a *Authorizer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The ServeMux's own 404 and 405 handlers match no pattern; a redirect to
	// the canonical path reports the pattern it leads to.
	if _, pattern := a.mux.Handler(r); pattern != "" && !a.registered[pattern] {
		log.Printf("api: route %q was not registered through the Authorizer; refusing it", pattern)
		writeError(w, http.StatusInternalServerError, &APIError{Code: ErrCodeInternal, Message: "route is not authorized"})
		return
	}
	a.mux.ServeHTTP(w, r)
}

// authorize returns 401 without a caller identity, 403 when the
// SubjectAccessReview denies the request and 500 when it cannot be made.
func (a *Authorizer) authorize(rule Rule, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, &APIError{Code: ErrCodeUnauthorized, Message: "authentication required"})
			return
		}

		attrs := &authorizationv1.ResourceAttributes{
			Group:     aiplatformv1alpha1.GroupVersion.Group,
			Version:   aiplatformv1alpha1.GroupVersion.Version,
			Resource:  rule.Resource,
			Verb:      rule.Verb,
			Namespace: rule.Namespace,
		}
		if rule.NamespaceFromPath {
			attrs.Namespace = r.PathValue("namespace")
		}
		if rule.NameFromPath {
			attrs.Name = r.PathValue("name")
		}
		// UID is deliberately not set: the caller identity carries none.
		sar := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
			User:               u.GetName(),
			Groups:             u.GetGroups(),
			ResourceAttributes: attrs,
		}}
		if extra := u.GetExtra(); len(extra) > 0 {
			sar.Spec.Extra = make(map[string]authorizationv1.ExtraValue, len(extra))
			for k, v := range extra {
				sar.Spec.Extra[k] = v
			}
		}

		if err := a.client.Create(r.Context(), sar); err != nil {
			log.Printf("api: SubjectAccessReview failed for %s %s: %v", rule.Verb, rule.Resource, err)
			writeError(w, http.StatusInternalServerError, &APIError{Code: ErrCodeInternal, Message: "authorization check failed"})
			return
		}
		if !sar.Status.Allowed {
			writeError(w, http.StatusForbidden, &APIError{Code: ErrCodeForbidden, Message: "forbidden"})
			return
		}
		next(w, r)
	}
}

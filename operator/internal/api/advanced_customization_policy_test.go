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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apiserver/pkg/admission"
	plugincel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/validating"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/cel/environment"
	openapispec "k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/yaml"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const (
	policyTemplate = "../../../charts/aif-operator/templates/advanced-customization-policy.yaml"
	aiWorkloadCRD  = "../../config/crd/bases/ai-factory.suse.com_aiworkloads.yaml"
	policyOperator = "system:serviceaccount:aif-operator:aif-operator"
)

var (
	aiWorkloadGVK = schema.GroupVersionKind{Group: "ai-factory.suse.com", Version: "v1alpha1", Kind: "AIWorkload"}
	aiWorkloadGVR = schema.GroupVersionResource{Group: "ai-factory.suse.com", Version: "v1alpha1", Resource: "aiworkloads"}
)

// loadAdvancedPolicy renders the chart template for release aif-operator in
// namespace aif-operator with the capability present and the toggle on.
func loadAdvancedPolicy(t *testing.T) (*admissionregistrationv1.ValidatingAdmissionPolicy, *admissionregistrationv1.ValidatingAdmissionPolicyBinding) {
	t.Helper()
	raw, err := os.ReadFile(policyTemplate)
	if err != nil {
		t.Fatal(err)
	}
	s := regexp.MustCompile(`(?s)\{\{- /\*.*?\*/\}\}\n`).ReplaceAllString(string(raw), "")
	s = regexp.MustCompile(`(?m)^\s*\{\{- (if .*|end|include "aif-operator.labels" .*?)\s*\}\}\n`).ReplaceAllString(s, "")
	s = strings.NewReplacer(`{{ .Release.Namespace }}`, "aif-operator", `{{ include "aif-operator.fullname" . }}`, "aif-operator").Replace(s)
	if strings.Contains(s, "{{") {
		t.Fatalf("unrendered template directive left:\n%s", s)
	}
	docs := strings.Split(s, "\n---\n")
	if len(docs) != 2 {
		t.Fatalf("want policy and binding, got %d documents", len(docs))
	}
	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	if err := yaml.UnmarshalStrict([]byte(docs[0]), policy); err != nil {
		t.Fatal(err)
	}
	if err := yaml.UnmarshalStrict([]byte(docs[1]), binding); err != nil {
		t.Fatal(err)
	}
	return policy, binding
}

type crdSchemaResolver struct{ schema *openapispec.Schema }

func (r crdSchemaResolver) ResolveSchema(gvk schema.GroupVersionKind) (*openapispec.Schema, error) {
	return r.schema, nil
}

func aiWorkloadSchema(t *testing.T) *openapispec.Schema {
	t.Helper()
	raw, err := os.ReadFile(aiWorkloadCRD)
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(crd.Spec.Versions[0].Schema.OpenAPIV3Schema)
	if err != nil {
		t.Fatal(err)
	}
	s := &openapispec.Schema{}
	if err := json.Unmarshal(b, s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestAdvancedCustomizationPolicy_TypeChecks compiles every expression against
// the AIWorkload CRD schema (what the apiserver reports in
// status.typeChecking) and against the CEL environment of a 1.30 apiserver,
// the oldest that serves ValidatingAdmissionPolicy v1.
func TestAdvancedCustomizationPolicy_TypeChecks(t *testing.T) {
	policy, binding := loadAdvancedPolicy(t)
	if binding.Spec.PolicyName != policy.Name || len(binding.Spec.ValidationActions) != 1 || binding.Spec.ValidationActions[0] != admissionregistrationv1.Deny {
		t.Fatalf("binding = %+v, want Deny on %s", binding.Spec, policy.Name)
	}

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(aiWorkloadGVK, meta.RESTScopeNamespace)
	tc := &validating.TypeChecker{SchemaResolver: crdSchemaResolver{aiWorkloadSchema(t)}, RestMapper: mapper}
	if w := tc.Check(policy); len(w) != 0 {
		t.Fatalf("type checking warnings: %+v", w)
	}
	ctx := tc.CreateContext(policy)
	// The type checker sees variables as dyn, so check each expression with
	// the variables it uses inlined.
	exprs := map[string]string{}
	ref := regexp.MustCompile(`variables\.(\w+)`)
	inline := func(e string) string {
		return ref.ReplaceAllStringFunc(e, func(m string) string { return "(" + exprs[m] + ")" })
	}
	for _, v := range policy.Spec.Variables {
		exprs["variables."+v.Name] = inline(v.Expression)
	}
	for _, m := range policy.Spec.MatchConditions {
		exprs["matchConditions."+m.Name] = m.Expression
	}
	exprs["validations[0]"] = inline(policy.Spec.Validations[0].Expression)
	for name, e := range exprs {
		if res := tc.CheckExpression(ctx, e); len(res) != 0 {
			t.Errorf("%s: %s", name, res.String())
		}
	}

	compiler, err := plugincel.NewCompositedCompiler(environment.MustBaseEnvSet(version.MajorMinor(1, 29)))
	if err != nil {
		t.Fatal(err)
	}
	opts := plugincel.OptionalVariableDeclarations{HasAuthorizer: true}
	for _, v := range policy.Spec.Variables {
		if res := compiler.CompileAndStoreVariable(&validating.Variable{Name: v.Name, Expression: v.Expression}, opts, environment.NewExpressions); res.Error != nil {
			t.Errorf("variables.%s on a 1.30 apiserver: %v", v.Name, res.Error)
		}
	}
	for _, e := range append([]string{policy.Spec.Validations[0].Expression}, policy.Spec.MatchConditions[0].Expression) {
		if res := compiler.CompileCELExpression(&validating.ValidationCondition{Expression: e}, opts, environment.NewExpressions); res.Error != nil {
			t.Errorf("%q on a 1.30 apiserver: %v", e, res.Error)
		}
	}
}

// policyValidator assembles the policy the way the apiserver's
// ValidatingAdmissionPolicy plugin does.
func policyValidator(t *testing.T, policy *admissionregistrationv1.ValidatingAdmissionPolicy) validating.Validator {
	t.Helper()
	compiler, err := plugincel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
	if err != nil {
		t.Fatal(err)
	}
	opts := plugincel.OptionalVariableDeclarations{HasAuthorizer: true}
	vars := make([]plugincel.NamedExpressionAccessor, 0, len(policy.Spec.Variables))
	for _, v := range policy.Spec.Variables {
		vars = append(vars, &validating.Variable{Name: v.Name, Expression: v.Expression})
	}
	compiler.CompileAndStoreVariables(vars, opts, environment.StoredExpressions)
	conds := make([]plugincel.ExpressionAccessor, 0, len(policy.Spec.MatchConditions))
	for i := range policy.Spec.MatchConditions {
		conds = append(conds, (*matchconditions.MatchCondition)(&policy.Spec.MatchConditions[i]))
	}
	matcher := matchconditions.NewMatcher(compiler.CompileCondition(conds, opts, environment.StoredExpressions), policy.Spec.FailurePolicy, "policy", "validate", policy.Name)
	validations := make([]plugincel.ExpressionAccessor, 0, len(policy.Spec.Validations))
	for _, v := range policy.Spec.Validations {
		validations = append(validations, &validating.ValidationCondition{Expression: v.Expression, Message: v.Message, Reason: v.Reason})
	}
	return validating.NewValidator(compiler.CompileCondition(validations, opts, environment.StoredExpressions), matcher,
		compiler.CompileCondition(nil, opts, environment.StoredExpressions), compiler.CompileCondition(nil, opts, environment.StoredExpressions),
		policy.Spec.FailurePolicy)
}

func workloadObject(t *testing.T, s *aiplatformv1alpha1.AIWorkloadSpec) runtime.Object {
	t.Helper()
	if s == nil {
		return nil
	}
	// Decoded from JSON, as the apiserver decodes a request body.
	b, err := json.Marshal(&aiplatformv1alpha1.AIWorkload{Spec: *s})
	if err != nil {
		t.Fatal(err)
	}
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(b, &u.Object); err != nil {
		t.Fatal(err)
	}
	u.SetGroupVersionKind(aiWorkloadGVK)
	u.SetNamespace("default")
	u.SetName("wl")
	return u
}

// evaluatePolicy returns whether the write is denied and how many
// authorization checks the policy made.
func evaluatePolicy(t *testing.T, v validating.Validator, existing, next *aiplatformv1alpha1.AIWorkloadSpec, username string, allowed bool) (bool, int) {
	t.Helper()
	decisions, asked := policyDecisions(t, v, existing, next, username, allowed)
	denied := false
	for _, d := range decisions {
		if d.Evaluation == validating.EvalError {
			t.Fatalf("policy evaluation error: %s", d.Message)
		}
		if d.Action == validating.ActionDeny {
			denied = true
			if d.Reason != "Forbidden" || !strings.Contains(d.Message, "permission to update Blueprints") {
				t.Errorf("deny decision = %+v", d)
			}
		}
	}
	return denied, asked
}

func policyDecisions(t *testing.T, v validating.Validator, existing, next *aiplatformv1alpha1.AIWorkloadSpec, username string, allowed bool) ([]validating.PolicyDecision, int) {
	t.Helper()
	op := admission.Update
	if existing == nil {
		op = admission.Create
	}
	obj, old := workloadObject(t, next), workloadObject(t, existing)
	attrs := admission.NewAttributesRecord(obj, old, aiWorkloadGVK, "default", "wl", aiWorkloadGVR, "", op, nil, false, &user.DefaultInfo{Name: username})
	asked := 0
	authz := authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
		asked++
		if a.GetAPIGroup() != "ai-factory.suse.com" || a.GetResource() != "blueprints" || a.GetVerb() != "update" || a.GetNamespace() != "" || a.GetUser().GetName() != username {
			t.Errorf("unexpected authorization check %+v", a)
		}
		if allowed {
			return authorizer.DecisionAllow, "", nil
		}
		return authorizer.DecisionNoOpinion, "", nil
	})
	res := v.Validate(context.Background(), aiWorkloadGVR, &admission.VersionedAttributes{Attributes: attrs, VersionedObject: obj, VersionedOldObject: old, VersionedKind: aiWorkloadGVK},
		nil, nil, celconfig.RuntimeCELCostBudget, authz)
	return res.Decisions, asked
}

// TestAdvancedCustomizationPolicy_MatchesAPI checks that the admission policy
// and advancedChanges agree (the API without Blueprint knowledge, which the
// policy does not have) on what needs the permission.
func TestAdvancedCustomizationPolicy_MatchesAPI(t *testing.T) {
	policy, _ := loadAdvancedPolicy(t)
	v := policyValidator(t, policy)

	deps := func(d ...string) *[]string { d = append([]string{}, d...); return &d }
	k := func(dir string, files map[string]string) *aiplatformv1alpha1.ComponentKustomize {
		return &aiplatformv1alpha1.ComponentKustomize{Dir: dir, Files: files}
	}
	ov := func(name string, d *[]string, kz *aiplatformv1alpha1.ComponentKustomize) aiplatformv1alpha1.ComponentValueOverride {
		return aiplatformv1alpha1.ComponentValueOverride{ComponentName: name, DependsOn: d, Kustomize: kz}
	}
	two := []string{"c1", "c2"}
	spec := func(ns string, clusters []string, ovs ...aiplatformv1alpha1.ComponentValueOverride) *aiplatformv1alpha1.AIWorkloadSpec {
		return &aiplatformv1alpha1.AIWorkloadSpec{
			DisplayName: "wl", TargetNamespace: ns, TargetClusters: clusters, ComponentValues: ovs,
			Source: aiplatformv1alpha1.AIWorkloadSource{SourceType: aiplatformv1alpha1.AIWorkloadSourceBlueprint, Blueprint: &aiplatformv1alpha1.BlueprintSource{Name: "rag", Version: "1.0.0"}},
		}
	}
	plain := func(ovs ...aiplatformv1alpha1.ComponentValueOverride) *aiplatformv1alpha1.AIWorkloadSpec {
		return spec("ai", two, ovs...)
	}
	files := map[string]string{"o/kustomization.yaml": "patches: []\n"}
	bumped := spec("ai", two, ov("a", nil, k("o", files)))
	bumped.Source.Blueprint.Version = "1.1.0"
	switched := func(ovs ...aiplatformv1alpha1.ComponentValueOverride) *aiplatformv1alpha1.AIWorkloadSpec {
		s := plain(ovs...)
		s.Source.Blueprint.Name = "other"
		return s
	}
	toApp := plain(ov("a", nil, k("o", files)))
	toApp.Source = aiplatformv1alpha1.AIWorkloadSource{SourceType: aiplatformv1alpha1.AIWorkloadSourceApp, App: &aiplatformv1alpha1.AppSource{ChartRepo: "r", ChartName: "a", ChartVersion: "1", Release: "a"}}

	cases := map[string]struct {
		existing, next *aiplatformv1alpha1.AIWorkloadSpec
		want           bool
	}{
		"create without advanced fields":      {nil, plain(ov("a", nil, nil)), false},
		"create without componentValues":      {nil, plain(), false},
		"create with kustomize":               {nil, plain(ov("a", nil, k("o", files))), true},
		"create with empty dependsOn":         {nil, plain(ov("a", deps(), nil)), true},
		"nil to empty dependsOn":              {plain(ov("a", nil, nil)), plain(ov("a", deps(), nil)), true},
		"empty dependsOn unchanged":           {plain(ov("a", deps(), nil)), plain(ov("a", deps(), nil)), false},
		"dependsOn changed":                   {plain(ov("a", deps("b"), nil)), plain(ov("a", deps("c"), nil)), true},
		"dependsOn reordered":                 {plain(ov("a", deps("b", "c"), nil)), plain(ov("a", deps("c", "b"), nil)), false},
		"dependsOn name added":                {plain(ov("a", deps("b"), nil)), plain(ov("a", deps("b", "c"), nil)), true},
		"dependsOn name dropped":              {plain(ov("a", deps("b", "c"), nil)), plain(ov("a", deps("b"), nil)), true},
		"dependsOn removed":                   {plain(ov("a", deps("b"), k("o", nil))), plain(ov("a", nil, k("o", nil))), false},
		"kustomize unchanged":                 {plain(ov("a", nil, k("o", files))), plain(ov("a", nil, k("o", files))), false},
		"kustomize dir changed":               {plain(ov("a", nil, k("o", nil))), plain(ov("a", nil, k("p", nil))), true},
		"kustomize file changed":              {plain(ov("a", nil, k("o", files))), plain(ov("a", nil, k("o", map[string]string{"o/kustomization.yaml": "resources: [x.yaml]\n"}))), true},
		"kustomize removed":                   {plain(ov("a", nil, k("o", nil))), plain(ov("a", nil, nil)), false},
		"component order independent":         {plain(ov("a", deps("b"), nil), ov("b", nil, k("o", nil))), plain(ov("b", nil, k("o", nil)), ov("a", deps("b"), nil)), false},
		"moved to another component":          {plain(ov("a", nil, k("o", nil))), plain(ov("b", nil, k("o", nil))), true},
		"only first stored entry counts":      {plain(ov("a", nil, nil), ov("a", deps("b"), k("o", nil))), plain(ov("a", nil, k("o", nil))), true},
		"first stored dependsOn counts":       {plain(ov("a", nil, nil), ov("a", deps("b"), nil)), plain(ov("a", deps("b"), nil)), true},
		"first stored entry kept":             {plain(ov("a", deps("b"), k("o", nil)), ov("a", nil, nil)), plain(ov("a", deps("b"), k("o", nil))), false},
		"overlay kept, cluster added":         {plain(ov("a", nil, k("o", files))), spec("ai", []string{"c1", "c2", "local"}, ov("a", nil, k("o", files))), true},
		"overlay kept, cluster swapped":       {plain(ov("a", nil, k("o", files))), spec("ai", []string{"c1", "local"}, ov("a", nil, k("o", files))), true},
		"overlay kept, cluster removed":       {plain(ov("a", nil, k("o", files))), spec("ai", []string{"c1"}, ov("a", nil, k("o", files))), false},
		"overlay kept, targets reordered":     {plain(ov("a", nil, k("o", files))), spec("ai", []string{"c2", "c1"}, ov("a", nil, k("o", files))), false},
		"overlay kept, namespace changed":     {plain(ov("a", nil, k("o", files))), spec("ai2", two, ov("a", nil, k("o", files))), true},
		"overlay kept, version changed":       {plain(ov("a", nil, k("o", files))), bumped, false},
		"overlay kept, Blueprint switched":    {plain(ov("a", nil, k("o", files))), switched(ov("a", nil, k("o", files))), true},
		"overlay kept, source type switched":  {plain(ov("a", nil, k("o", files))), toApp, true},
		"overlay removed, Blueprint switched": {plain(ov("a", nil, k("o", files))), switched(), false},
		"no overlay, Blueprint switched":      {plain(ov("a", deps("b"), nil)), switched(ov("a", deps("b"), nil)), false},
		"overlay removed, cluster added":      {plain(ov("a", nil, k("o", files))), spec("ai", []string{"c1", "c2", "local"}), false},
		"no overlay, cluster added":           {plain(ov("a", deps("b"), nil)), spec("ai", []string{"c1", "c2", "local"}, ov("a", deps("b"), nil)), false},
		"no overlay, namespace changed":       {plain(), spec("ai2", two), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := advancedChanges(tc.existing, *tc.next, nil); got != tc.want {
				t.Fatalf("advancedChanges = %v, want %v", got, tc.want)
			}
			denied, asked := evaluatePolicy(t, v, tc.existing, tc.next, "alice", false)
			if denied != tc.want || asked != map[bool]int{true: 1, false: 0}[tc.want] {
				t.Fatalf("policy denied = %v after %d checks, want %v", denied, asked, tc.want)
			}
			if tc.want {
				if denied, asked := evaluatePolicy(t, v, tc.existing, tc.next, "bob", true); denied || asked != 1 {
					t.Fatalf("permitted user: denied = %v after %d checks", denied, asked)
				}
				if denied, asked := evaluatePolicy(t, v, tc.existing, tc.next, policyOperator, false); denied || asked != 0 {
					t.Fatalf("operator ServiceAccount: denied = %v after %d checks, want exempt", denied, asked)
				}
			}
		})
	}
}

// The API prunes stored dependsOn names the Blueprint no longer has; the
// policy cannot see the Blueprint and needs the permission for that edit.
func TestAdvancedCustomizationPolicy_StricterOnPrunedDependsOn(t *testing.T) {
	policy, _ := loadAdvancedPolicy(t)
	stale := []string{"b", "gone"}
	pruned := []string{"b"}
	existing := &aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: "ai", ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "a", DependsOn: &stale}}}
	next := &aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: "ai", ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "a", DependsOn: &pruned}}}
	if advancedChanges(existing, *next, map[string]bool{"a": true, "b": true}) {
		t.Fatal("API should accept the pruned dependsOn")
	}
	if denied, _ := evaluatePolicy(t, policyValidator(t, policy), existing, next, "alice", false); !denied {
		t.Fatal("policy should require the permission")
	}
}

// A large but realistic workload stays inside the apiserver's CEL cost budget,
// so the Fail policy does not block it.
func TestAdvancedCustomizationPolicy_LargeWorkloadWithinCostBudget(t *testing.T) {
	policy, _ := loadAdvancedPolicy(t)
	v := policyValidator(t, policy)
	content := strings.Repeat("x", 4<<10)
	clusters := make([]string, 100)
	for i := range clusters {
		clusters[i] = fmt.Sprintf("c-%d", i)
	}
	spec := &aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: "ai", TargetClusters: clusters}
	for i := range 200 {
		deps := []string{fmt.Sprintf("d-%d", i), fmt.Sprintf("e-%d", i)}
		spec.ComponentValues = append(spec.ComponentValues, aiplatformv1alpha1.ComponentValueOverride{
			ComponentName: fmt.Sprintf("comp-%d", i), DependsOn: &deps,
			Kustomize: &aiplatformv1alpha1.ComponentKustomize{Dir: "o", Files: map[string]string{"o/kustomization.yaml": content}},
		})
	}
	if denied, asked := evaluatePolicy(t, v, spec, spec, "alice", false); denied || asked != 0 {
		t.Fatalf("unchanged large workload: denied = %v after %d checks", denied, asked)
	}
}

// Over the cost budget the policy errors, and failurePolicy Fail denies a user
// without the permission instead of letting the write skip the check.
func TestAdvancedCustomizationPolicy_CostErrorDenies(t *testing.T) {
	policy, _ := loadAdvancedPolicy(t)
	spec := &aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: "ai"}
	deps := []string{"d"}
	for i := range 2000 {
		spec.ComponentValues = append(spec.ComponentValues, aiplatformv1alpha1.ComponentValueOverride{ComponentName: fmt.Sprintf("comp-%d", i), DependsOn: &deps})
	}
	decisions, _ := policyDecisions(t, policyValidator(t, policy), spec, spec, "alice", false)
	if len(decisions) != 1 || decisions[0].Evaluation != validating.EvalError || decisions[0].Action != validating.ActionDeny {
		t.Fatalf("decisions = %+v, want one denying evaluation error", decisions)
	}
}

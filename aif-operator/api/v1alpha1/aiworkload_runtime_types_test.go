package v1alpha1

import (
	"encoding/json"
	"testing"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestAIWorkloadRuntimeTypesCompile(t *testing.T) {
	// Verify all runtime type definitions compile and instantiate
	_ = InputValue{Name: "param1"}
	_ = OutputValue{Name: "result1"}
	_ = ValidationResult{RuleName: "rule1", Passed: true}
	_ = SecretCheckResult{ClusterID: "c1", SecretName: "secret1", Exists: true}
	_ = RequirementCheckResult{ClusterID: "c1", CapabilityName: "gpu", Available: true}
}

func TestInputValueJSON(t *testing.T) {
	// InputValue with required fields round-trips through JSON
	val := apixv1.JSON{Raw: []byte(`{"key":"value"}`)}
	in := InputValue{
		Name:  "param1",
		Value: &val,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out InputValue
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != "param1" {
		t.Errorf("expected Name round-trip, got %q", out.Name)
	}
	if out.Value == nil {
		t.Errorf("expected Value round-trip, got nil")
	}
}

func TestOutputValueJSON(t *testing.T) {
	// OutputValue with all fields round-trips through JSON
	val := apixv1.JSON{Raw: []byte(`{"result":"data"}`)}
	in := OutputValue{
		Name:  "result1",
		Value: &val,
		Label: "Result Output",
		Type:  "object",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out OutputValue
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != "result1" {
		t.Errorf("expected Name round-trip, got %q", out.Name)
	}
	if out.Label != "Result Output" {
		t.Errorf("expected Label round-trip, got %q", out.Label)
	}
	if out.Type != "object" {
		t.Errorf("expected Type round-trip, got %q", out.Type)
	}

	// Empty optional fields are omitted from JSON
	minimal := OutputValue{Name: "result2"}
	minimalJSON, _ := json.Marshal(minimal)
	if jsonHasKey(minimalJSON, "label") || jsonHasKey(minimalJSON, "type") {
		t.Errorf("expected label,type omitted when empty, got %s", minimalJSON)
	}
}

func TestValidationResultJSON(t *testing.T) {
	// ValidationResult with all fields round-trips through JSON
	in := ValidationResult{
		RuleName: "cpu_min",
		Passed:   false,
		Message:  "CPU requirement not met",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out ValidationResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.RuleName != "cpu_min" {
		t.Errorf("expected RuleName round-trip, got %q", out.RuleName)
	}
	if out.Passed != false {
		t.Errorf("expected Passed=false, got %v", out.Passed)
	}
	if out.Message != "CPU requirement not met" {
		t.Errorf("expected Message round-trip, got %q", out.Message)
	}

	// Empty message is omitted from JSON
	passing := ValidationResult{RuleName: "check", Passed: true}
	passingJSON, _ := json.Marshal(passing)
	if jsonHasKey(passingJSON, "message") {
		t.Errorf("expected message omitted when empty, got %s", passingJSON)
	}
}

func TestSecretCheckResultJSON(t *testing.T) {
	// SecretCheckResult with all fields round-trips through JSON
	in := SecretCheckResult{
		ClusterID:   "cluster-1",
		SecretName:  "api-credentials",
		Exists:      true,
		MissingKeys: []string{"token", "api-key"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out SecretCheckResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ClusterID != "cluster-1" {
		t.Errorf("expected ClusterID round-trip, got %q", out.ClusterID)
	}
	if out.SecretName != "api-credentials" {
		t.Errorf("expected SecretName round-trip, got %q", out.SecretName)
	}
	if out.Exists != true {
		t.Errorf("expected Exists=true, got %v", out.Exists)
	}
	if len(out.MissingKeys) != 2 || out.MissingKeys[0] != "token" {
		t.Errorf("expected MissingKeys round-trip, got %v", out.MissingKeys)
	}

	// Empty MissingKeys is omitted from JSON
	intact := SecretCheckResult{ClusterID: "cluster-2", SecretName: "secret", Exists: true}
	intactJSON, _ := json.Marshal(intact)
	if jsonHasKey(intactJSON, "missingKeys") {
		t.Errorf("expected missingKeys omitted when empty, got %s", intactJSON)
	}
}

func TestRequirementCheckResultJSON(t *testing.T) {
	// RequirementCheckResult with all fields round-trips through JSON
	in := RequirementCheckResult{
		ClusterID:      "cluster-1",
		CapabilityName: "nvidia-gpu",
		Available:      true,
		Details:        "NVIDIA GPU v100 available",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid json: %s", b)
	}
	var out RequirementCheckResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ClusterID != "cluster-1" {
		t.Errorf("expected ClusterID round-trip, got %q", out.ClusterID)
	}
	if out.CapabilityName != "nvidia-gpu" {
		t.Errorf("expected CapabilityName round-trip, got %q", out.CapabilityName)
	}
	if out.Available != true {
		t.Errorf("expected Available=true, got %v", out.Available)
	}
	if out.Details != "NVIDIA GPU v100 available" {
		t.Errorf("expected Details round-trip, got %q", out.Details)
	}

	// Empty details is omitted from JSON
	unavailable := RequirementCheckResult{ClusterID: "cluster-2", CapabilityName: "tpu", Available: false}
	unavailableJSON, _ := json.Marshal(unavailable)
	if jsonHasKey(unavailableJSON, "details") {
		t.Errorf("expected details omitted when empty, got %s", unavailableJSON)
	}
}

func TestAIWorkloadRuntimeTypesV2Preview(t *testing.T) {
	// Verify v2 preview types are usable in AIWorkload status
	v1 := apixv1.JSON{Raw: []byte(`"value"`)}
	v2 := apixv1.JSON{Raw: []byte(`42`)}
	_ = InputValue{
		Name:  "user_input",
		Value: &v1,
	}
	_ = OutputValue{
		Name:  "computed_output",
		Value: &v2,
	}
	_ = ValidationResult{
		RuleName: "schema_check",
		Passed:   true,
	}
	_ = SecretCheckResult{
		ClusterID:  "prod-1",
		SecretName: "credentials",
		Exists:     true,
	}
	_ = RequirementCheckResult{
		ClusterID:      "prod-1",
		CapabilityName: "gpu",
		Available:      true,
	}
}

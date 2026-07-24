package main

// programDisplayNames maps NGC program/subscription codes to nicer display names.
// This is NOT a gate: any code in the NGC "productNames" label group, plus any
// "*_supported" designation, is surfaced automatically (see programLabels). This
// map only prettifies the label text. A code without an entry falls back to the
// API's resolved value and then to a humanized code — and the refresh run logs
// those so a nicer name can be added here. Names follow the NGC API Field Value
// Reference.
func programDisplayNames() map[string]string {
	return map[string]string{
		"nvaie_supported":         "NVIDIA AI Enterprise Supported",
		"omniverse_supported":     "NVIDIA Omniverse Enterprise Supported",
		"nv-ai-enterprise":        "NVIDIA AI Enterprise Essentials",
		"omniverse":               "NVIDIA Omniverse",
		"nemo-microservices":      "NeMo Microservices",
		"nvidia-mission-control":  "NVIDIA Mission Control",
		"nvidia-runai-saas":       "NVIDIA Run:ai (SaaS)",
		"nvidia-runai-selfhosted": "NVIDIA Run:ai (Self-Hosted)",
	}
}

// hiddenPrograms lists program codes to suppress even though NGC exposes them
// (e.g. internal, developer-only, or early-access entitlements). Add codes here
// to keep them off the tiles.
func hiddenPrograms() map[string]bool {
	return map[string]bool{
		// Developer / early-access entitlements — not user-facing "Supported" programs.
		"nim-dev":       true,
		"omniverse-dev": true,
	}
}

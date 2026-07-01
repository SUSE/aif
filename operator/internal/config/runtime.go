package config

import "os"

const DefaultExtensionNamespace = "cattle-ui-plugin-system"

func GetExtensionNamespace() string {
	if ns := os.Getenv("EXTENSION_NAMESPACE"); ns != "" {
		return ns
	}
	return DefaultExtensionNamespace
}

const DefaultOperatorNamespace = "aif-operator"

func GetOperatorNamespace() string {
	if ns := os.Getenv("OPERATOR_NAMESPACE"); ns != "" {
		return ns
	}
	return DefaultOperatorNamespace
}

const DefaultOperatorService = "aif-operator"

func GetOperatorService() string {
	if svc := os.Getenv("OPERATOR_SERVICE"); svc != "" {
		return svc
	}
	return DefaultOperatorService
}

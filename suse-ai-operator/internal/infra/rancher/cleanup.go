package rancher

import (
	"context"
	stderrors "errors"

	v1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
	logging "github.com/SUSE/suse-ai-operator/internal/logging"
)

func (m *Manager) Cleanup(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	namespace string,
) error {
	log := logging.FromContext(ctx, "rancher.cleanup").
		WithValues(logging.KeyExtension, ext.Name)

	log.Info("Cleaning up Rancher resources")
	if ext == nil {
		return nil
	}

	var errs []error

	name := ext.Spec.Extension.Name
	if err := m.DeleteUIPlugin(ctx, name, namespace); err != nil {
		errs = append(errs, err)
	}

	if err := m.DeleteClusterRepo(ctx, ClusterRepoName(name)); err != nil {
		errs = append(errs, err)
	}

	if ext.Status.ActiveExtensionName != "" && ext.Status.ActiveExtensionName != name {
		oldName := ext.Status.ActiveExtensionName
		log.Info("Cleaning up old extension resources", "oldName", oldName)
		if err := m.DeleteUIPlugin(ctx, oldName, namespace); err != nil {
			errs = append(errs, err)
		}
		if err := m.DeleteClusterRepo(ctx, ClusterRepoName(oldName)); err != nil {
			errs = append(errs, err)
		}
	}

	if err := stderrors.Join(errs...); err != nil {
		return err
	}

	log.Info("Rancher cleanup completed")
	return nil
}

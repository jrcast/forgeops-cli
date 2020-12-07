package install

import (
	"fmt"

	"github.com/ForgeRock/forgeops-cli/internal/factory"
	"github.com/ForgeRock/forgeops-cli/internal/k8s"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Install Installs the quickstart in the namespace provided
func Install(clientFactory factory.Factory, path string) error {
	errs := []error{}
	k8sCntMgr := k8s.NewK8sClientMgr(clientFactory)
	cfg, err := k8sCntMgr.GetConfigFlags()
	if err != nil {
		return err
	}
	infos, err := k8sCntMgr.GetObjectsFromPath(path)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		return fmt.Errorf("no objects found")
	}
	// Iterate through all objects, applying each one.
	for _, info := range infos {
		if err := k8sCntMgr.ApplyObjectInOtherNamespace(info, *cfg.Namespace); err != nil {
			errs = append(errs, err)
		}
	}
	// If any errors occurred during apply, then return error (or
	// aggregate of errors).
	if len(errs) == 1 {
		return errs[0]
	}
	if len(errs) > 1 {
		return utilerrors.NewAggregate(errs)
	}
	return nil
}

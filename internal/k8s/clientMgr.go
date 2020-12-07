package k8s

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ForgeRock/forgeops-cli/internal/factory"
	"github.com/ForgeRock/forgeops-cli/internal/printer"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
)

// NewK8sClientMgr create a new instance of NewK8sClientMgr
func NewK8sClientMgr(f factory.Factory) ClientMgr {
	return &clientMgr{
		f,
	}
}

// ClientMgr a container for creating kubernetes rest clients
type ClientMgr interface {
	factory.Factory
	GetObjectsFromPath(path string) ([]*resource.Info, error)
	ApplyObject(info *resource.Info) error
	ApplyObjectInOtherNamespace(info *resource.Info, namespace string) error
}

type clientMgr struct {
	factory.Factory
}

// NullSchema always validates bytes.
type NullSchema struct{}

// ValidateBytes never fails for NullSchema.
func (NullSchema) ValidateBytes(data []byte) error { return nil }

// GetObjectsFromPath Obtains objects from filepath or url
func (cmgr clientMgr) GetObjectsFromPath(path string) ([]*resource.Info, error) {
	usage := "contains the configuration to apply"
	cfg, err := cmgr.GetConfigFlags()
	if err != nil {
		return nil, err
	}
	filenames := []string{path}
	kustomize := ""
	recursive := false
	fileNameFlags := &genericclioptions.FileNameFlags{
		Usage:     usage,
		Filenames: &filenames,
		Kustomize: &kustomize,
		Recursive: &recursive,
	}
	fileNameOpts := fileNameFlags.ToOptions()
	builder := cmgr.Builder()
	r := builder.
		Unstructured().
		Schema(NullSchema{}).
		ContinueOnError().
		NamespaceParam(*cfg.Namespace).DefaultNamespace().
		// FilenameParam(len(*cfg.Namespace) > 0, &fileNameOpts).
		FilenameParam(false, &fileNameOpts).
		Flatten().
		Do()
	objects, err := r.Infos()
	return objects, err
}

// ApplyObject Applies changes to the object
func (cmgr clientMgr) ApplyObject(info *resource.Info) error {
	return cmgr.ApplyObjectInOtherNamespace(info, info.Namespace)
}

// ApplyObjectInOtherNamespace Takes the definition of an object and applies it in a different ns
func (cmgr clientMgr) ApplyObjectInOtherNamespace(info *resource.Info, namespace string) error {
	var metadataAccessor = meta.NewAccessor()
	helper := resource.NewHelper(info.Client, info.Mapping).
		WithFieldManager("forgeops")

	isNamespace := strings.ToLower(info.ResourceMapping().GroupVersionKind.Kind) == "namespace"
	if namespace != "" {
		// if the object type is a namespace OR
		// if the object is a namespaced object and the namespace provided is different, then change the namespace
		if isNamespace || (info.Namespaced() && info.Namespace != namespace) {
			if isNamespace {
				info.Name = namespace
				if err := metadataAccessor.SetName(info.Object, namespace); err != nil {
					return err
				}
			} else {
				info.Namespace = namespace
				if err := metadataAccessor.SetNamespace(info.Object, namespace); err != nil {
					return err
				}

			}
		}
	}
	// if the object doesn't exist. Create it. Otherwise, patch it.
	if err := info.Get(); err != nil {
		if apierrors.IsNotFound(err) {
			obj, err := helper.Create(info.Namespace, true, info.Object)
			if err != nil {
				return err
			}
			info.Refresh(obj, true)
			printer.Printf(fmt.Sprintf("%s %q created", info.ResourceMapping().GroupVersionKind.Kind, info.Name))
			return nil
		}
		return err
	}
	// The object exist. Patch/Apply instead
	// Send the full object to be applied on the server side.
	data, err := runtime.Encode(unstructured.UnstructuredJSONScheme, info.Object)
	if err != nil {
		if statusError, ok := err.(apierrors.APIStatus); ok {
			status := statusError.Status()
			status.Message = fmt.Sprintf("error when %s %q: %v", "serverside-apply", info.Source, status.Message)
			return &apierrors.StatusError{ErrStatus: status}
		}
		return fmt.Errorf("error when %s %q: %v", "serverside-apply", info.Source, err)
	}
	options := metav1.PatchOptions{
		Force: func(b bool) *bool { return &b }(false),
	}
	data, err = cmgr.clearManagedFields(data)

	obj, err := helper.Patch(
		info.Namespace,
		info.Name,
		types.ApplyPatchType,
		data,
		&options,
	)
	if err != nil {
		return err
	}
	info.Refresh(obj, true)
	printer.Printf(fmt.Sprintf("%s %q applied", info.ResourceMapping().GroupVersionKind.Kind, info.Name))
	return nil

}

// clearManagedFields removes any entry from metadata.managedFields
// when using the apply operation you cannot have managedFields in the object that is being applied
func (cmgr clientMgr) clearManagedFields(data []byte) ([]byte, error) {
	var err error
	var raw map[string]json.RawMessage
	var metadata metav1.ObjectMeta

	if err := json.Unmarshal(data, &raw); err != nil {
		return []byte{}, err
	}
	if err := json.Unmarshal(raw["metadata"], &metadata); err != nil {
		return []byte{}, err
	}
	metadata.SetManagedFields([]metav1.ManagedFieldsEntry{})

	raw["metadata"], err = json.Marshal(metadata)
	if err != nil {
		return []byte{}, err
	}
	return json.Marshal(raw)
}

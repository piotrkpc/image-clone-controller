package imageclone

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"strings"
)

var _ admission.Handler = &DeploymentImageClone{}
var _ runtime.Object = &appsv1.Deployment{}
var _ runtime.Object = &appsv1.DaemonSet{}

// +kubebuilder:webhook:path=/imageclone-v1-deployment,mutating=true,failurePolicy=fail,groups="",resources=deployments,verbs=create;update,versions=v1,name=mdeployment.kb.io
type DeploymentImageClone struct {
	imageClone
	Client  client.Client
	decoder *admission.Decoder
	Log     logr.Logger
}

func (d *DeploymentImageClone) InjectDecoder(decoder *admission.Decoder) error {
	d.decoder = decoder
	return nil
}

func (d *DeploymentImageClone) Handle(ctx context.Context, request admission.Request) admission.Response {
	dep := &appsv1.Deployment{}
	err := d.decoder.Decode(request, dep)
	if err != nil {
		d.Log.Error(err, "decoding error: ")
		return admission.Errored(http.StatusBadRequest, err)
	}
	for idx, container := range dep.Spec.Template.Spec.Containers {
		d.Log.Info(fmt.Sprintf("testing if the container image matches registry: %s ", container.Image))
		if !matchesRegistry(container.Image, d.backupRegistry) {
			d.Log.Info("using container not from safe backup registry, backing-up the image...", "Image: ", container.Image)
			err := d.backupImage(container.Image)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			d.Log.Info("backup done, performing mutation from :", "Image: ", container.Image)
			err = d.mutateDeploymentsContainer(dep, idx)
			d.Log.Info("mutation done.", "Image: ", dep.Spec.Template.Spec.Containers[idx].Image)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
		}
	}
	depRaw, err := json.Marshal(dep)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(request.Object.Raw, depRaw)
}

func (d *DeploymentImageClone) mutateDeploymentsContainer(dep *appsv1.Deployment, idx int) error {
	container := dep.Spec.Template.Spec.Containers[idx]
	newImageFromBackupRegistry := d.changeRegistryToBackup(container)
	container.Image = newImageFromBackupRegistry
	dep.Spec.Template.Spec.Containers[idx] = container
	return nil
}

// +kubebuilder:webhook:path=/imageclone-v1-daemonset,mutating=true,failurePolicy=fail,groups="",resources=daemonsets,verbs=create;update,versions=v1,name=mdaemonset.kb.io
type DaemonsetImageClone struct {
	imageClone
	Client  client.Client
	decoder *admission.Decoder
	Log     logr.Logger
}

func (ds *DaemonsetImageClone) InjectDecoder(decoder *admission.Decoder) error {
	ds.decoder = decoder
	return nil
}

func (ds *DaemonsetImageClone) Handle(ctx context.Context, request admission.Request) admission.Response {
	dset := &appsv1.DaemonSet{}
	err := ds.decoder.Decode(request, dset)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	for idx, container := range dset.Spec.Template.Spec.Containers {
		if !matchesRegistry(container.Image, ds.backupRegistry) {
			ds.Log.Info("using container not from safe backup registry, backing-up the image...", "Image: ", container.Image)
			err := ds.backupImage(container.Image)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			ds.Log.Info("backup done, performing mutation from :", "Image: ", container.Image)
			err = ds.mutateDaemonSetContainer(dset, idx)

			ds.Log.Info("mutation done.", "Image: ", dset.Spec.Template.Spec.Containers[idx].Image)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
		}
	}
	depRaw, err := json.Marshal(dset)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(request.Object.Raw, depRaw)
}

func (ds *DaemonsetImageClone) mutateDaemonSetContainer(dep *appsv1.DaemonSet, idx int) error {
	container := dep.Spec.Template.Spec.Containers[idx]
	newImageFromBackupRegistry := ds.changeRegistryToBackup(container)
	container.Image = newImageFromBackupRegistry
	dep.Spec.Template.Spec.Containers[idx] = container
	return nil
}

type imageClone struct {
	backupRegistry string
	auth           remote.Option
	imageGet       func(ref name.Reference, options ...remote.Option) (v1.Image, error)
	Log            logr.Logger
}

func (i *imageClone) InjectAuth(auth remote.Option) {
	i.auth = auth
	return
}

func (i *imageClone) InjectImageGetFunc(fn func(ref name.Reference, optrions ...remote.Option) (v1.Image, error)) {
	i.imageGet = fn
}

func (i *imageClone) InjectBackupRegistry(backupRegistry string) {
	i.backupRegistry = backupRegistry
}

func (i *imageClone) changeRegistryToBackup(container corev1.Container) string {
	idx := strings.LastIndex(container.Image, "/")
	newImageFromBackupRegistry := fmt.Sprintf("%s/%s", i.backupRegistry, container.Image[idx+1:])
	return newImageFromBackupRegistry
}

func (i *imageClone) backupImage(image string) error {
	imgRef, err := name.ParseReference(image)
	if err != nil {
		return err
	}
	img, err := i.imageGet(imgRef)
	if err != nil {
		return err
	}
	tag, err := name.NewTag(i.backupRegistry + "/" + image)
	if err != nil {
		return err
	}
	err = remote.Write(tag, img, i.auth)
	if err != nil {
		return err
	}
	return nil
}

func matchesRegistry(containerString string, registryStr string) bool {
	if len(registryStr) == 0 {
		return false
	}
	return strings.HasPrefix(containerString, registryStr)
}

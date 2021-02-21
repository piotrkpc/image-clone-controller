package imageclone

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/fake"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"io/ioutil"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"net/http"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"testing"
)

const MyTestRegistry = "docker.io/piotrkpcbackup"
const TestImageName = "busybox:latest"

var _ http.RoundTripper = &stubTripper{}

type stubTripper struct {
	WasCalled bool
}

func (t *stubTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.WasCalled = true
	return &http.Response{
		StatusCode: 200,
	}, nil
}

func getStubTransportAndTripper() (*stubTripper, remote.Option) {
	stubTripper := stubTripper{}
	stubTransport := remote.WithTransport(&stubTripper)
	return &stubTripper, stubTransport
}

func getFakeImageGet() func(ref name.Reference, options ...remote.Option) (v1.Image, error) {
	return func(ref name.Reference, options ...remote.Option) (v1.Image, error) {
		return &fake.FakeImage{}, nil
	}
}

func TestMatchesRegistry(t *testing.T) {
	type args struct {
		containerString string
		registryStr     string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{name: "given empty image string, return false", args: args{
			containerString: "",
			registryStr:     MyTestRegistry,
		}, want: false},
		{name: "given empty registry return false", args: args{
			containerString: "",
			registryStr:     "",
		}, want: false},
		{name: "given image from different registry return false", args: args{
			containerString: "quay.io/busybox:latest",
			registryStr:     MyTestRegistry,
		}, want: false},
		{name: "given image from same registry return true", args: args{
			containerString: "docker.io/piotrkpcbackup/busybox:latest",
			registryStr:     MyTestRegistry,
		}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesRegistry(tt.args.containerString, tt.args.registryStr); got != tt.want {
				t.Errorf("MatchesRegistry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_imageClone_BackupImage(t *testing.T) {

	stubTripper, stubTransport := getStubTransportAndTripper()
	c := imageClone{
		backupRegistry: MyTestRegistry,
		auth:           stubTransport,
		imageGet:       getFakeImageGet(),
	}
	err := c.backupImage(TestImageName)
	if err != nil {
		t.Fatal(err)
	}
	if !stubTripper.WasCalled {
		t.Error("did not called external service")
	}
}

func Test_deploymentImageClone_Handle(t *testing.T) {
	srcDeploymentBytes, err := ioutil.ReadFile("testSrcDeployment.yml")
	if err != nil {
		t.Fatal(err)
	}
	dstDeploymentBytes, err := ioutil.ReadFile("testDstDeployment.yml")
	if err != nil {
		t.Fatal(err)
	}
	wantedResponseWithPatch := admission.PatchResponseFromRaw(srcDeploymentBytes, dstDeploymentBytes)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID: "test",
		Kind: metav1.GroupVersionKind{
			Group:   appsv1.GroupName,
			Version: appsv1.SchemeGroupVersion.Version,
			Kind:    "Deployment",
		},
		Resource: metav1.GroupVersionResource{
			Group:    metav1.GroupName,
			Version:  "v1",
			Resource: "deployments",
		},
		Namespace: "default",
		Operation: "CREATE",
		Object:    runtime.RawExtension{Raw: srcDeploymentBytes},
	},
	}
	decoder, err := admission.NewDecoder(runtime.NewScheme())
	if err != nil {
		t.Fatal(err)
	}
	tripper, transport := getStubTransportAndTripper()

	d := DeploymentImageClone{
		imageClone: imageClone{
			backupRegistry: MyTestRegistry,
			imageGet:       getFakeImageGet(),
		},
		Log: logr.Discard(),
	}
	err = d.InjectDecoder(decoder)
	if err != nil {
		t.Fatal(err)
	}
	d.InjectAuth(transport)
	if err != nil {
		t.Fatal(err)
	}
	resp := d.Handle(context.TODO(), req)
	if !tripper.WasCalled {
		t.Fatal("we should have called remote service")
	}
	if !reflect.DeepEqual(resp, wantedResponseWithPatch) {
		t.Error("response is different than expected", resp)
	}
}

func Test_daemonSetImageClone_Handle(t *testing.T) {
	srcDaemonSetBytes, err := ioutil.ReadFile("testSrcDaemonSet.yml")
	if err != nil {
		t.Fatal(err)
	}
	dstDaemonSetBytes, err := ioutil.ReadFile("testDstDaemonSet.yml")
	if err != nil {
		t.Fatal(err)
	}
	wantedResponseWithPatch := admission.PatchResponseFromRaw(srcDaemonSetBytes, dstDaemonSetBytes)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID: "test",
		Kind: metav1.GroupVersionKind{
			Group:   appsv1.GroupName,
			Version: appsv1.SchemeGroupVersion.Version,
			Kind:    "DaemonSet",
		},
		Resource: metav1.GroupVersionResource{
			Group:    metav1.GroupName,
			Version:  "v1",
			Resource: "daemonsets",
		},
		Namespace: "default",
		Operation: "CREATE",
		Object:    runtime.RawExtension{Raw: srcDaemonSetBytes},
	},
	}
	decoder, err := admission.NewDecoder(runtime.NewScheme())
	if err != nil {
		t.Fatal(err)
	}
	tripper, transport := getStubTransportAndTripper()

	d := DaemonsetImageClone{
		imageClone: imageClone{
			backupRegistry: MyTestRegistry,
			imageGet:       getFakeImageGet(),
		},
		Log: logr.Discard(),
	}

	err = d.InjectDecoder(decoder)
	if err != nil {
		t.Fatal(err)
	}
	d.InjectAuth(transport)
	if err != nil {
		t.Fatal(err)
	}

	resp := d.Handle(context.TODO(), req)
	if !tripper.WasCalled {
		t.Fatal("we should have called remote service")
	}
	if !reflect.DeepEqual(resp, wantedResponseWithPatch) {
		t.Error("response is different than expected", resp)
	}
}

func Test_imageClone_changeRegistryToBackup(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "given busybox from quay.io, change to backup registry",
			image: "quay.io/busybox:latest",
			want:  MyTestRegistry + "/busybox:latest",
		},
		{name: "given default registry image, change to backup registry",
			image: "busybox:latest", want: MyTestRegistry + "/busybox:latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := &imageClone{
				backupRegistry: MyTestRegistry,
			}
			container := corev1.Container{Image: tt.image}
			if got := i.changeRegistryToBackup(container); got != tt.want {
				t.Errorf("changeRegistryToBackup() = %v, want %v", got, tt.want)
			}
		})
	}
}

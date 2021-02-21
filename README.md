# Image Clone Controller
### Demo

https://asciinema.org/a/GPO97jUeNWnI4eAVd5ZAX2OPt

### This project was scaffolded using `operator-sdk`:

`operator-sdk version: "v1.4.2", commit: "4b083393be65589358b3e0416573df04f4ae8d9b", kubernetes version: "v1.19.4", go version: "go1.15.8", GOOS: "darwin", GOARCH: "amd64"`

Given that the framework only supports creation of webhooks for custom resources, I've created my own manifest for deployment instead of relying on `kubebuilder` generation.

- `./imageclone` - core package with tests and test data
- `./config/webhooks` - manifests for deploying the admission controller.

## How-to deploy
The fastest way to deploy is to use `kind`.

```
kind version
kind v0.10.0 go1.15.7 darwin/amd64
```

`kind create cluster --name image-clone-controller --config kind-config.yaml`

### Prerequisite
The Admission Controller webhooks requires HTTPS endpoint. People are using scripts that generate self-signed certificates, but I think using `cert-manager` is better approach. To install `cert-manager` run:

```
# Install cert-manager
kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.2.0/cert-manager.yaml
```

### Deploy the admission controller
Manifests should deploy everything you need:
```
kubectl apply -f config/webhooks/
```

```
╭─pkopec@Piotrs-MacBook-Pro ~ 
╰─$ k get pods -n image-clone-controller                                                                                                                                                      130 ↵
NAME                                      READY   STATUS    RESTARTS   AGE
image-clone-controller-5df8cdf7dd-vhjxp   1/1     Running   0          14m
╭─pkopec@Piotrs-MacBook-Pro ~ 
╰─$ k get svc -n image-clone-controller 
NAME                     TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
image-clone-controller   ClusterIP   10.96.2.0    <none>        443/TCP   103m
╭─pkopec@Piotrs-MacBook-Pro ~ 
╰─$ k get mutatingwebhookconfigurations.admissionregistration.k8s.io -n image-clone-controller 
NAME                               WEBHOOKS   AGE
cert-manager-webhook               1          4h28m
image-clone-controller             1          3h18m
image-clone-controller-daemonset   1          10m
```
### Tag namespaces to be watches by controller
The controller watches resources that have appropriate tags. You need to assign tags to namespaces before deploying workloads.

```
kubectl label namespaces default image-clone-controller=enabled
```

### Test functionality of the controller.
```kubectl appply -f imageclone/testSrcDeployment.yml```

You can observe the process looking at the logs of the controller:
```
kubectl logs -f -n image-clone-controller image-clone-controller-5df8cdf7dd-4cbr5

2021-02-22T05:48:41.369Z	INFO	mutationWebhook	testing if the container image matches registry: busybox:latest 
2021-02-22T05:48:41.369Z	INFO	mutationWebhook	using container not from safe backup registry, backing-up the image...	{"Image: ": "busybox:latest"}
2021-02-22T05:48:45.283Z	INFO	mutationWebhook	backup done, performing mutation from :	{"Image: ": "busybox:latest"}
2021-02-22T05:48:45.283Z	INFO	mutationWebhook	mutation done.	{"Image: ": "docker.io/piotrkpcbackup/busybox:latest"}
2021-02-22T05:48:45.286Z	DEBUG	controller-runtime.webhook.webhooks	wrote response	{"webhook": "/imageclone-v1-deployment", "code": 200, "reason": "", "UID": "c16df3db-ed35-4aef-80db-597ee3fb40ad", "allowed": true}
```

Verify the image use in resource is from a backup registry:
```
$ kubectl describe deployments.apps busybox-deployment | grep Image
    Image:      docker.io/piotrkpcbackup/busybox:latest
    
$ grep -i image imageclone/testSrcDeployment.yml 
          image: busybox:latest
```
### Development
As I already mentioned the project is scaffolded using `operator-sdk` framework, but no CRDs were added hence some steps in `make` may not work as intended.

#### Unit tests
`make test` - runs tests in the project
```
?       github.com/piotrkpc/image-clone-controller      [no test files]
ok      github.com/piotrkpc/image-clone-controller/imageclone   0.644s  coverage: 80.5% of statements
```

Currently, there are no integration tests. `operator-sdk` usually provides good integration tests suite but given the scenario, entire logic could be tested in unit tests.

### Some design decisions and notes before production
- even though `Deployment` and `Daemonset` API are very similar I've decided to keep them on a separate endpoint. This should enable independent development of the code should the API change with little code duplication.
- currently, the solution is synchronous, which means that we need to wait for the clone to complete.
- currently, the installation process is very basic, on large clusters the controller should scale.
- integration and performance tests are missing and should be developed for production use-case.

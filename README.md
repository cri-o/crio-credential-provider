# CRI-O Credential Provider

This project aims to ship a credential provider built for CRI-O to authenticate
image pulls against registry mirrors by using namespaced Kubernetes Secrets.

![flow-graph](.github/flow.jpg "Flow graph")

## Running the main use case in OpenShift

How to test the feature in OpenShift is outlined in
[test/openshift/README.md](test/openshift/README.md).

## Running the main use case in plain Kubernetes

Build the project and inject the test `registries.conf` into the binary:

```console
export ROOT=$(git rev-parse --show-toplevel)
cd $ROOT
```

```console
make REGISTRIES_CONF=$ROOT/test/registries.conf
```

Run Kubernetes (possibly via [`hack/local-up-cluster.sh`](https://github.com/kubernetes/kubernetes/blob/master/hack/local-up-cluster.sh)):

```console
export FEATURE_GATES=KubeletServiceAccountTokenForCredentialProviders=true
export KUBELET_FLAGS="--image-credential-provider-bin-dir=$ROOT/build --image-credential-provider-config=$ROOT/test/cluster/credential-provider-config.yml"
```

```console
/path/to/hack/local-up-cluster.sh
```

Start CRI-O [with the auth file feature](https://github.com/cri-o/cri-o/pull/9463)
and using the custom `registries.conf`:

```console
sudo ./bin/crio --registries-conf $ROOT/test/registries.conf
```

Run the `localhost:5000` registry which uses basic auth:

```console
./test/registry/start
```

Apply the cluster RBAC and examples:

```console
kubectl apply -f test/cluster/rbac.yml -f test/cluster/secret.yml
```

Finally, run the test workload:

```console
kubectl apply -f test/cluster/pod.yml
```

The credential provider writes an additional file which can be consumed by
CRI-O. The CRI-O logs will state that the mirror is being used and not the
original registry:

```text
INFO[2025-09-11T10:01:26.915456743+02:00] Trying to access "localhost:5000/library/nginx:latest"
INFO[2025-09-11T10:01:26.994748261+02:00] Pulled image: docker.io/library/nginx@sha256:5ff65e8820c7fd8398ca8949e7c4191b93ace149f7ff53a2a7965566bd88ad23  id=057b232a-c168-41ea-9f8e-4a2db672826b name=/runtime.v1.ImageService/PullImage
```

The registry also logs the access:

```console
podman logs registry
```

```text
time="2025-09-11T08:01:26.920319786Z" level=info msg="authorized request" go.version=go1.20.8 http.request.host="localhost:5000" http.request.id=ace363e2-c0de-427b-a833-8a3d5ee5d360 http.request.method=GET http.request.remoteaddr="[2001:db8:4860::1]:33432" http.request.uri="/v2/library/nginx/manifests/latest" http.request.useragent="cri-o/1.34.0 go/go1.25.0 os/linux arch/amd64" vars.name="library/nginx" vars.reference=latest
2001:db8:4860::1 - - [11/Sep/2025:08:01:26 +0000] "GET /v2/library/nginx/manifests/latest HTTP/1.1" 200 1958 "" "cri-o/1.34.0 go/go1.25.0 os/linux arch/amd64"
time="2025-09-11T08:01:26.920579713Z" level=info msg="response completed" go.version=go1.20.8 http.request.host="localhost:5000" http.request.id=ace363e2-c0de-427b-a833-8a3d5ee5d360 http.request.method=GET http.request.remoteaddr="[2001:db8:4860::1]:33432" http.request.uri="/v2/library/nginx/manifests/latest" http.request.useragent="cri-o/1.34.0 go/go1.25.0 os/linux arch/amd64" http.response.contenttype="application/vnd.oci.image.manifest.v1+json" http.response.duration=2.773353ms http.response.status=200 http.response.written=1958
time="2025-09-11T08:01:26.9230305Z" level=info msg="authorized request" go.version=go1.20.8 http.request.host="localhost:5000" http.request.id=c35181da-ad5d-4b85-b6d6-415ed2851e6c http.request.method=GET http.request.remoteaddr="[2001:db8:4860::1]:33432" http.request.uri="/v2/library/nginx/blobs/sha256:41f689c209100e6cadf3ce7fdd02035e90dbd1d586716bf8fc6ea55c365b2d81" http.request.useragent="cri-o/1.34.0 go/go1.25.0 os/linux arch/amd64" vars.digest="sha256:41f689c209100e6cadf3ce7fdd02035e90dbd1d586716bf8fc6ea55c365b2d81" vars.name="library/nginx"
time="2025-09-11T08:01:26.925497207Z" level=info msg="response completed" go.version=go1.20.8 http.request.host="localhost:5000" http.request.id=c35181da-ad5d-4b85-b6d6-415ed2851e6c http.request.method=GET http.request.remoteaddr="[2001:db8:4860::1]:33432" http.request.uri="/v2/library/nginx/blobs/sha256:41f689c209100e6cadf3ce7fdd02035e90dbd1d586716bf8fc6ea55c365b2d81" http.request.useragent="cri-o/1.34.0 go/go1.25.0 os/linux arch/amd64" http.response.contenttype="application/octet-stream" http.response.duration=4.248009ms http.response.status=200 http.response.written=8594
2001:db8:4860::1 - - [11/Sep/2025:08:01:26 +0000] "GET /v2/library/nginx/blobs/sha256:41f689c209100e6cadf3ce7fdd02035e90dbd1d586716bf8fc6ea55c365b2d81 HTTP/1.1" 200 8594 "" "cri-o/1.34.0 go/go1.25.0 os/linux arch/amd64"
```

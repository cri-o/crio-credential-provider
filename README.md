# Running the main use case as test scenario

Build the project and inject the test `registries.conf` into the binary:

```console
export ROOT=$(git rev-parse --show-toplevel)
cd $ROOT
```

```console
go build -ldflags "-X main.registriesConfPath=$ROOT/test/registries.conf" -o build/credential-provider
```

Run Kubernetes (possibly via [`hack/local-up-cluster.sh`](https://github.com/kubernetes/kubernetes/blob/master/hack/local-up-cluster.sh)):

```console
export FEATURE_GATES=KubeletServiceAccountTokenForCredentialProviders=true
export KUBELET_FLAGS="--image-credential-provider-bin-dir=$ROOT/build --image-credential-provider-config=$ROOT/cluster/credential-provider-config.yaml"
```

```console
/path/to/hack/local-up-cluster.sh
```

Start CRI-O using the custom `registries.conf`:

```console
sudo crio --registries-conf $ROOT/test/registries.conf
```

Run the `localhost:5000` registry which uses basic auth:

```console
./test/registry/start
```

Apply the cluster RBAC and examples:

```console
kubectl apply -f cluster/rbac.yml -f cluster/example-secret.yml
```

Run the test workload:

```console
kubectl apply -f cluster/example-pod.yml
```

Right now the whole scenario does not work because of the invalidation of the
credentials in c/image via: https://github.com/containers/image/blob/df7e80d2d19872b61f352a8a182ec934dc0c2346/docker/docker_image_src.go#L139-L145

Referencing discussion: https://github.com/containers/container-libs/issues/333

# Using the CRI-O Credential Provider on OpenShift

This document outlines how to manually test the credential provider using
OpenShift. To do that, run OpenShift with latest CRI-O `main` or any version >=
`v1.35`, which ships with OpenShift >= `4.22`.

I recommend to setup SSH from one master node to the target worker node to be
able to access them if anything goes wrong.

## Prepare the cluster:

- Enable the feature gate `KubeletServiceAccountTokenForCredentialProviders` if
  not already done:

  ```console
  kubectl patch FeatureGate cluster --type merge --patch '{"spec":{"featureSet":"CustomNoUpgrade","customNoUpgrade":{"enabled":["KubeletServiceAccountTokenForCredentialProviders"]}}}'
  ```

  Wait for the Machine Config Operator (MCO) to update the nodes.

- Update the credential provider config:

  ```console
  podman run -it -v$PWD:/w -w/w quay.io/coreos/butane:release machine-config.bu -o machine-config.yml
  kubectl apply -f machine-config.yml
  ```

  Wait for MCO to update the worker nodes.

- Select a node where the workload and test should run:

  ```console
  export NODE_NAME=ip-10-0-56-61.us-west-1.compute.internal
  ```

- Get the credential provider repository on the node:

  ```console
  oc debug "node/$NODE_NAME"
  ```

  ```console
  chroot /host
  cd
  git clone --depth=1 https://github.com/cri-o/crio-credential-provider
  pushd crio-credential-provider
  ```

- Modify registries.conf and restart CRI-O (on the node):

  ```console
  cp test/registries.conf /etc/containers/registries.conf
  systemctl restart crio
  ```

- Start the local registry (on the node):

  ```console
  test/registry/start
  ```

- Build the credential provider binary (on the node):

  ```console
  podman run --privileged -it -w /w -v $PWD:/w golang:1.25 make
  ```

- Replace the existing (ecr) credential provider locally by using an user overlay (on the node):

  ```console
  rpm-ostree usroverlay
  cp build/crio-credential-provider /usr/libexec/kubelet-image-credential-provider-plugins/ecr-credential-provider
  ```

- Apply the required RBAC to the cluster:

  ```console
  sed -i 's;system:node:127.0.0.1;system:node:'"$NODE_NAME"';g' test/cluster/rbac.yml
  kubectl apply -f test/cluster/rbac.yml -f test/cluster/secret.yml
  ```

- Test the credential provider by using a node selector:

  ```console
  kubectl label nodes "$NODE_NAME" app=test
  sed -i "s;spec:;spec:\n  nodeSelector:\n    app: test;g" test/cluster/pod.yml
  kubectl apply -f test/cluster/pod.yml
  ```

- Inspect the credential provider logs using journald:

  ```console
  journalctl -f _COMM=ecr-credential-
  ```

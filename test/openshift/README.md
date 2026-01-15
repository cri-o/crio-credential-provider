# Using the CRI-O Credential Provider on OpenShift

This document outlines how to manually test the credential provider using
OpenShift. To do that, run OpenShift with latest CRI-O `main` or any version >=
`v1.34`, which ships with OpenShift >= `4.21`.

I recommend to setup SSH from one master node to the target worker node to be
able to access them if anything goes wrong.

## Prepare the cluster:

- Enable the feature gate `KubeletServiceAccountTokenForCredentialProviders` if
  not already done:

  ```console
  kubectl patch FeatureGate cluster --type merge --patch '{"spec":{"featureSet":"CustomNoUpgrade","customNoUpgrade":{"enabled":["KubeletServiceAccountTokenForCredentialProviders"]}}}'
  ```

  Wait for the Machine Config Operator (MCO) to update the nodes.

  ```console
  kubectl get mcp -w
  ```

- Update the credential provider config for the worker nodes:

  ```console
  podman run -it -v$PWD:/w -w/w quay.io/coreos/butane:release machine-config.bu -o machine-config.yml
  kubectl apply -f machine-config.yml
  ```

  Wait for MCO to update the worker nodes.

- Select a node where the workload and test should run:

  ```console
  export NODE_NAME=ip-10-0-56-61.us-west-1.compute.internal
  ```

- Verify that the credential provider is part of the OpenShift installation
  (should be the case for OpenShift >= `4.21`):

  ```console
  oc debug "node/$NODE_NAME"
  chroot /host
  ```

  ```console
  /usr/libexec/kubelet-image-credential-provider-plugins/crio-credential-provider --version
  ```

- Start the local registry (on the node):

  ```console
  git clone --depth=1 https://github.com/cri-o/crio-credential-provider ~/crio-credential-provider
  ~/crio-credential-provider/test/registry/start
  ```

- Apply the required RBAC to the cluster (on the host):

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
  journalctl -f _COMM=crio-credential
  ```

  The registry container as well as CRI-O should also log the corresponding
  access.

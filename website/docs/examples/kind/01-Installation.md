---
sidebar_position: 10
---

# Local development with [`ko`][ko] and [`KinD`][kind]
[ko]: https://ko.build
[kind]: https://kind.sigs.k8s.io/

## Installing Kro

1. Create a `KinD` cluster.

   ```sh
   kind create cluster
   ```

2. Create the `kro-system` namespace.

   ```sh
   kubectl create namespace kro-system
   ```

3. Set the `KO_DOCKER_REPO` env var.

   ```sh
   export KO_DOCKER_REPO=kind.local
   ```
   
   > _Note_, if not using the default kind cluster name, set KIND_CLUSTER_NAME

   ```sh
   export KIND_CLUSTER_NAME=my-other-cluster
   ```
4. Apply the Kro CRDs.

   ```sh
   make manifests
   kubectl apply -f ./helm/crds
   ```

5. Render and apply the local helm chart.
 
   ```sh
    helm template kro ./helm \
      --namespace kro-system \
      --set image.pullPolicy=Never \
      --set image.ko=true | ko apply -f -
    ```

## Hello World

1. Create a `NoOp` ResourceGraph using the `ResourceGraphDefinition`.

   ```sh
   kubectl apply -f - <<EOF
   apiVersion: kro.run/v1alpha1
   kind: ResourceGraphDefinition
   metadata:
     name: noop
   spec:
     schema:
       apiVersion: v1alpha1
       kind: NoOp
       spec: {}
       status: {}
     resources: []
   EOF
   ```
   
   Inspect that the `ResourceGraphDefinition` was created, and also the newly created CRD `NoOp`.

   ```sh
   kubectl get ResourceGraphDefinition noop
   kubectl get crds | grep noops
   ```
   
3. Create an instance of the new `NoOp` kind.

   ```sh
   kubectl apply -f - <<EOF
   apiVersion: kro.run/v1alpha1
   kind: NoOp
   metadata:
     name: demo
   EOF
   ```

   And inspect the new instance,

   ```shell
   kubectl get noops -oyaml
   ```

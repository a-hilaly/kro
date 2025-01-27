# Deploying a simple application to Azure with Azure Service Operator (ASO)

This example creates a ResourceGraphDefinition called `azuretodo` and instantiates an instance of it named `my-todo`.

To execute this example you'll need:

1. An Azure Subscription.
2. A Kubernetes cluster with kro and Azure Service Operator (ASO) installed. See [installing ASO](https://azure.github.io/azure-service-operator/#installation). 
   We recommend [kind](https://kind.sigs.k8s.io/) or [AKS](https://azure.microsoft.com/products/kubernetes-service).
3. A namespace to deploy the application into. The example assumes `my-todo`, but that can be overridden in `instance.yaml`. This can be created using `kubectl create namespace my-todo`.

## Accessing the TODO site

The easiest way to access the TODO site is to do a quick port-forward (works in KIND too):
```sh
kubectl port-forward -n my-todo service/my-todo 8080:80
```

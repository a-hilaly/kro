# Deploying an Azure storage container with Azure Service Operator (ASO)

This example creates a ResourceGraphDefinition for a storage account and container, and instantiates an instance of it.

To execute this example you'll need:

1. An Azure Subscription.
2. A Kubernetes cluster with kro and Azure Service Operator (ASO) installed. See [installing ASO](https://azure.github.io/azure-service-operator/#installation).
   We recommend [kind](https://kind.sigs.k8s.io/) or [AKS](https://azure.microsoft.com/products/kubernetes-service).
3. A namespace to deploy the resources into. The example assumes `mystorage`, but that can be overridden in `instance.yaml`. This can be created using `kubectl create namespace mystorage`.

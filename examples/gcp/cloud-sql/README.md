# CloudSQL example

This example creates a ResourceGraphDefinition called `CloudSQL` to deploy Cloud SQL instance in 2 regions as a primary replica pair.

## Create ResourceGraphDefinitions

Apply the RGD to your cluster:

```
kubectl apply -f rgd.yaml
```

Validate the RGD:

```
> kubectl get rgd cloudsql.kro.run
NAME               APIVERSION   KIND       STATE    AGE
cloudsql.kro.run   v1alpha1     CloudSQL   Active   44m
```

## Create an Instance of CloudSQL
Set the env variables used in the instance template:
```
export CLOUDSQL_NAME=demo
export GCP_PROJECT=myproject
export PRIMARY_REGION=us-central1
export REPLICA_REGION=us-west1
```

Run the following command to replace the env variables in `instance-template.yaml` file and create
a new file called instance.yaml. 
```shell
envsubst < "instance-template.yaml" > "instance.yaml"
```

Apply the `instance.yaml` 

```
kubectl apply -f instance.yaml
```

Validate instance status:

```
kubectl get cloudsqls
```

## Validate

Navigate to CloudSQL page in the GCP Console and verify the creation of primary and replica instances.

## Clean up

Remove the instance:

```
kubectl delete cloudsql $CLOUDSQL_NAME
```

Remove the ResourceGraphDefinitions:

```
kubectl delete rgd cloudsql.kro.run
```

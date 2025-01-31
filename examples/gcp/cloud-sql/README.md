# CloudSQL

This example show how you can use KRO to deploy GCP Cloud SQL instance in 2 regions as a primary and replica instances.


## End User: CloudSQL
The administrator needs to install the RGD first.
The end user creates a `CloudSQL` resource that looks like this:

```yaml
apiVersion: kro.run/v1alpha1
kind: CloudSQL
metadata:
  name: demo
  namespace: config-connector
spec:
  name: demo
  project: my-gcp-project
  primaryRegion: us-central1
  replicaRegion: us-west1
```

The status of the applied resource can be checked using:

```
kubectl get cloudsqls
kubectl get cloudsql demo -n config-connector -o yaml
```

Navigate to CloudSQL page in the GCP Console and verify the creation of primary and replica instances.

Once done, the user can delete the `CloudSQL` instance:

```
kubectl delete cloudsql demo -n config-connector
```

## Administrator: ResourceGraphDefinition
The administrator needs to install the RGD in the cluster first before the user can consume it:

```
kubectl apply -f rgd.yaml
```

Validate the RGD is installed correctly:

```
> kubectl get rgd cloudsql.kro.run
NAME               APIVERSION   KIND       STATE    AGE
cloudsql.kro.run   v1alpha1     CloudSQL   Active   44m
```

Once all user created instances are deleted, the administrator can choose to deleted the RGD.
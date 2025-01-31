# Getting Started with GCP

## Prerequisites

1. Kubectl

   1. Install kubectl on [macos](https://kubernetes.io/docs/tasks/tools/install-kubectl-macos/) or [linux](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/) 

2. Gcloud

   1. To install gcloud, please see [here](https://cloud.google.com/sdk/docs/install)

   2. [initialize](https://cloud.google.com/sdk/docs/initializing) the gcloud CLI

## Create a new Google Cloud project

Start by creating a new Google Cloud (GCP) project. When trying out KRO, we suggested that you use a separate project to avoid disrupting any production clusters or services. You may choose to follow your own best practices in setting up the project.

Steps to setup a new GCP project: 

```
# in *nix shell USER should be set. if not set USERNAME explicitly
export USERNAME=${USER?}
export PROJECT_ID=kro-${USERNAME?} 
export REGION=us-central1 # << CHANGE region here 
# Please set the appropriate folder, billing
export GCP_FOLDER=0000000000
export GCP_BILLING=000000-000000-000000
export ADMINUSER=someone@company.com

# Separate Gcloud configuration
gcloud config configurations create kro
gcloud config configurations activate kro
gcloud config set account ${ADMINUSER?}

# Create the project
gcloud projects create ${PROJECT_ID?} --folder=${GCP_FOLDER?}
gcloud auth application-default set-quota-project ${PROJECT_ID?}

# attach billing (THIS IS IMPORTANT)
gcloud beta billing projects link ${PROJECT_ID?} --billing-account ${GCP_BILLING?}

# Set the project ID in the current configuration
gcloud config set project ${PROJECT_ID?}
```

## Enable Google Cloud services

Enable the following required APIs:

```
gcloud services enable \
  container.googleapis.com  \
  cloudresourcemanager.googleapis.com \
  serviceusage.googleapis.com
```

## GKE Cluster with KCC and KRO

1. Create GKE Cluster:
   1. Navigate to the GCP console [Create Standard GKE cluster](https://pantheon.corp.google.com/kubernetes/add) page.
   1.  Set the name of the cluster (to say "kro")
   1. Enable Workload Identity under Cluster-Security settings. 
   1. Click create cluster. This step takes 5-10 mins.
2. Install KCC using these [instructions](https://cloud.google.com/config-connector/docs/how-to/install-manually):
   1. [Install KCC](https://cloud.google.com/config-connector/docs/how-to/install-manually#installing_the_operator)  
   ```
   gcloud storage cp gs://configconnector-operator/latest/release-bundle.tar.gz release-bundle.tar.gz
   tar zxvf release-bundle.tar.gz
   kubectl apply -f operator-system/configconnector-operator.yaml
   ```
   2. [Create SA and bind with KSA](https://cloud.google.com/config-connector/docs/how-to/install-manually#identity) with KCC  
   ```
   gcloud iam service-accounts create kcc-operator
   gcloud projects add-iam-policy-binding ${PROJECT_ID}\
    --member="serviceAccount:kcc-operator@${PROJECT_ID}.iam.gserviceaccount.com" \
    --role="roles/owner"
   gcloud iam service-accounts add-iam-policy-binding kcc-operator@${PROJECT_ID}.iam.gserviceaccount.com \
    --member="serviceAccount:${PROJECT_ID}.svc.id.goog[cnrm-system/cnrm-controller-manager]" \
    --role="roles/iam.workloadIdentityUser"
   gcloud projects add-iam-policy-binding ${PROJECT_ID}\
    --member="serviceAccount:kcc-operator@${PROJECT_ID}.iam.gserviceaccount.com" \
    --role="roles/storage.admin"
   ```
   3. [Configure Config Connector](https://cloud.google.com/config-connector/docs/how-to/install-manually#addon-configuring)  
   ```
   kubectl apply -f - <<EOF
   apiVersion: core.cnrm.cloud.google.com/v1beta1
   kind: ConfigConnector
   metadata:
     name: configconnector.core.cnrm.cloud.google.com
   spec:
     mode: cluster
     googleServiceAccount: "kcc-operator@${PROJECT_ID?}.iam.gserviceaccount.com"
     stateIntoSpec: Absent
   EOF
   ```
   4. [Create a namespace](https://cloud.google.com/config-connector/docs/how-to/install-manually#specify) for KCC objects  
   ```
   kubectl create namespace config-connector
   kubectl annotate namespace config-connector cnrm.cloud.google.com/project-id=${PROJECT_ID?}
   ```
   5. Verify KCC Installation
   ```
   kubectl wait -n cnrm-system --for=condition=Ready pod --all
   ```
3. Setup Kubectl to target the cluster `gcloud container clusters get-credentials kro --region $REGION --project $PROJECT_ID`
4. Install KRO following [instructions here](https://kro.run/docs/getting-started/Installation/)

## Cleanup

If you are operating in a dev environment and want to clean it up, follow these steps:

```
# in *nix shell USER should be set. if not set USERNAME explicitly
export USERNAME=${USER?}
export PROJECT_ID=compositions-${USERNAME?}
export REGION=us-west2
export CONFIG_CONTROLLER_NAME=compositions

# Delete the GCP project if you created one for trying out KRO
gcloud projects delete ${PROJECT_ID?}
# Delete gcloud configuration
gcloud config configurations activate <anything other than 'compositions'>
gcloud config configurations delete compositions
# Delete kubectl context
kubectl config  delete-context \
gke_${PROJECT_ID?}_${REGION?}_krmapihost-${CONFIG_CONTROLLER_NAME?}

# Dont forget to switch to another context 
kubectl config  get-contexts
kubectl config  use-context <context name>
```

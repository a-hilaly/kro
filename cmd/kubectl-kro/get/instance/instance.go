// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package instance

import (
	"context"
	"os"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	amruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"github.com/awslabs/kro/api/v1alpha1"
	kroclient "github.com/awslabs/kro/internal/client"
	"github.com/awslabs/kro/internal/graph"
	"github.com/awslabs/kro/internal/runtime"
)

var (
	optResourceGroupFile      string
	optResourceGroupName      string
	optResourceGroupNamespace string

	optNamespace string
)

func init() {
	Command.PersistentFlags().StringVarP(&optResourceGroupFile, "resourcegroup-file", "f", "", "target resourcegroup file")
	Command.PersistentFlags().StringVarP(&optResourceGroupName, "rg-name", "r", "", "target resourcegroup name")
	Command.PersistentFlags().StringVarP(&optResourceGroupNamespace, "rg-namespace", "N", "default", "target resourcegroup namespace")
	Command.PersistentFlags().StringVarP(&optNamespace, "namespace", "n", "default", "target instance namespace")
}

var Command = &cobra.Command{
	Use:     "instance",
	Aliases: []string{"instances"},
	Args:    cobra.MinimumNArgs(0),
	Short:   "Get information about an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		set, err := kroclient.NewSet(kroclient.Config{})
		if err != nil {
			return nil
		}

		var rg v1alpha1.ResourceGroup
		if optResourceGroupFile != "" {
			b, err := os.ReadFile(optResourceGroupFile)
			if err != nil {
				return err
			}

			err = yaml.UnmarshalStrict(b, &rg)
			if err != nil {
				return err
			}
		} else {
			rgMap, err := set.Dynamic().Resource(schema.GroupVersionResource{
				Group:    v1alpha1.GroupVersion.Group,
				Version:  v1alpha1.GroupVersion.Version,
				Resource: "resourcegroups",
			}).Namespace(optResourceGroupNamespace).Get(context.Background(), optResourceGroupName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			// transform the unstructured object to a typed object
			err = amruntime.DefaultUnstructuredConverter.FromUnstructured(rgMap.Object, &rg)
			if err != nil {
				return err
			}
		}

		err = getInstancesInfo(set, optNamespace, args, &rg)
		if err != nil {
			return err
		}
		return nil
	},
}

func getInstancesInfo(set *kroclient.Set, namespace string, instanceNames []string, rg *v1alpha1.ResourceGroup) error {
	builder, err := graph.NewBuilder(set.RESTConfig())
	if err != nil {
		return err
	}

	processedRG, err := builder.NewResourceGroup(rg)
	if err != nil {
		return err
	}

	// If no instance names provided, list all instances
	if len(instanceNames) == 0 {
		gvr := processedRG.Instance.GetGroupVersionResource()
		list, err := set.Dynamic().Resource(gvr).Namespace(namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		// Extract names from the list
		instanceNames = make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			instanceNames = append(instanceNames, item.GetName())
		}
	}

	// Rest of your existing code to collect and display instances...
	instances := make([]InstanceInfo, 0, len(instanceNames))
	for _, instanceName := range instanceNames {
		info, err := getInstanceInfo(set, namespace, instanceName, processedRG)
		if err != nil {
			return err
		}
		instances = append(instances, info)
	}

	// Print table with all instances
	tw := tablewriter.NewWriter(os.Stdout)
	tw.SetHeader([]string{"NAME", "STATE", "SYNCED", "AGE"})
	tw.SetBorder(false)
	tw.SetCenterSeparator("")
	tw.SetColumnSeparator("")
	tw.SetRowSeparator("")
	tw.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	tw.SetAlignment(tablewriter.ALIGN_LEFT)
	tw.SetHeaderLine(false)

	// Add all instances and their resources
	for _, inst := range instances {
		// Add instance row
		tw.Append([]string{
			inst.Name,
			inst.State,
			inst.Synced,
			inst.Age.String(),
		})

		// Add resource rows
		for i, res := range inst.Resources {
			isLast := i == len(inst.Resources)-1
			prefix := "       ├──"
			if isLast {
				prefix = "       └──"
			}

			tw.Append([]string{
				prefix + " " + res.ID,
				res.State,
				res.Synced,
				res.Age.String(),
			})
		}
	}

	tw.Render()
	return nil
}

func getInstanceInfo(cs *kroclient.Set, namespace, name string, rg *graph.Graph) (InstanceInfo, error) {
	ctx := context.Background()
	info := InstanceInfo{Resources: make([]ResourceInfo, 0)}

	gvr := rg.Instance.GetGroupVersionResource()
	instance, err := cs.Dynamic().Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return info, err
	}

	rt, err := rg.NewGraphRuntime(instance)
	if err != nil {
		return info, err
	}

	// Set instance info
	info.Name = instance.GetName()
	info.Age = time.Since(instance.GetCreationTimestamp().Time).Round(time.Second)
	info.State = "ACTIVE"
	info.Synced = "True"
	if !instance.GetDeletionTimestamp().IsZero() {
		info.State = "DELETING"
	}

	// Collect resource states
	for _, resourceID := range rt.TopologicalOrder() {
		resource, state := rt.GetResource(resourceID)
		resInfo := ResourceInfo{ID: resourceID}

		if state != runtime.ResourceStateResolved {
			resInfo.State = "PENDING"
			resInfo.Synced = "False"
		} else {
			descriptor := rt.ResourceDescriptor(resourceID)
			gvr := descriptor.GetGroupVersionResource()
			var rc dynamic.ResourceInterface
			if descriptor.IsNamespaced() {
				rc = cs.Dynamic().Resource(gvr).Namespace(namespace)
			} else {
				rc = cs.Dynamic().Resource(gvr)
			}

			observed, err := rc.Get(ctx, resource.GetName(), metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					resInfo.State = "PENDING"
					resInfo.Synced = "False"
				} else {
					resInfo.State = "ERROR"
					resInfo.Synced = "False"
				}
			} else {
				resInfo.State = "ACTIVE"
				resInfo.Synced = "True"
				resInfo.Age = time.Since(observed.GetCreationTimestamp().Time).Round(time.Second)
				rt.SetResource(resourceID, observed)
			}
		}

		info.Resources = append(info.Resources, resInfo)
		rt.Synchronize()
	}

	return info, nil
}

// ResourceState represents the state of a resource
type ResourceState struct {
	State  string
	Synced string
	Age    time.Duration
}

// First create a struct to hold all instance and resource info
type InstanceInfo struct {
	Name      string
	State     string
	Synced    string
	Age       time.Duration
	Resources []ResourceInfo
}

type ResourceInfo struct {
	ID     string
	State  string
	Synced string
	Age    time.Duration
}

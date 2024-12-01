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

package rbac

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/awslabs/kro/api/v1alpha1"
	kroclient "github.com/awslabs/kro/internal/client"
	"github.com/awslabs/kro/internal/graph"
)

var (
	optScope             string
	optResourceGroupFile string
	optOutputFormat      string
)

func init() {
	Command.PersistentFlags().StringVarP(&optScope, "scope", "s", "namespace", "whether to generate a ClusterRole or Role")
	Command.PersistentFlags().StringVarP(&optResourceGroupFile, "file", "f", "", "target resourcegroup file")
	Command.PersistentFlags().StringVarP(&optOutputFormat, "output", "o", "yaml", "output format (json|yaml)")
}

var Command = &cobra.Command{
	Use:     "rbac",
	Aliases: []string{},
	Args:    cobra.NoArgs,
	Short:   "Generate recommended RBAC for ResourceGroups",
	RunE: func(cmd *cobra.Command, args []string) error {
		b, err := os.ReadFile(optResourceGroupFile)
		if err != nil {
			return err
		}

		var rg v1alpha1.ResourceGroup
		err = yaml.UnmarshalStrict(b, &rg)
		if err != nil {
			return err
		}

		err = generateRBAC(&rg)
		if err != nil {
			return err
		}
		return nil
	},
}

func generateRBAC(rg *v1alpha1.ResourceGroup) error {
	set, err := kroclient.NewSet(kroclient.Config{})
	if err != nil {
		return nil
	}
	restConfig := set.RESTConfig()

	builder, err := graph.NewBuilder(restConfig)
	if err != nil {
		return err
	}

	processedRG, err := builder.NewResourceGroup(rg)
	if err != nil {
		return err
	}

	gvrs := []schema.GroupVersionResource{}
	for _, id := range processedRG.TopologicalOrder {
		gvrs = append(gvrs, processedRG.Resources[id].GetGroupVersionResource())
	}

	kroDefaultVerbs := []string{
		"get",
		"list",
		"create",
		"update",
		"patch",
		"delete",
	}

	resourcesByGroup := resourcesByGroup(map[string][]string{})
	// group GVRs by api group
	for _, gvr := range gvrs {
		resourcesByGroup.addGVR(gvr)
	}

	policyRules := []rbacv1.PolicyRule{}
	for _, group := range resourcesByGroup.groups() {
		policyRules = append(policyRules, rbacv1.PolicyRule{
			Verbs:     kroDefaultVerbs,
			APIGroups: []string{group},
			Resources: resourcesByGroup[group],
		})
	}

	metadataName := rg.ObjectMeta.Name + "-cluster-role"

	var b []byte
	switch optScope {
	case "cluster":
		clusterRole := newClusterRole(metadataName, policyRules)
		b, err = marshalObject(clusterRole, optOutputFormat)
		if err != nil {
			return err
		}
	case "namespace":
		role := newRole(rg.ObjectMeta.Namespace, metadataName, policyRules)
		b, err = marshalObject(role, optOutputFormat)
		if err != nil {
			return err
		}
	}

	fmt.Println(string(b))
	return nil
}

var (
	kroGenLabels = map[string]string{
		"kro.run/version": "dev",
	}
)

func newClusterRole(metadataName string, policyRules []rbacv1.PolicyRule) rbacv1.ClusterRole {
	return rbacv1.ClusterRole{
		TypeMeta: v1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:        metadataName,
			Annotations: kroGenLabels,
		},
		Rules: policyRules,
	}
}
func newRole(namespace string, metadataName string, policyRules []rbacv1.PolicyRule) rbacv1.Role {
	return rbacv1.Role{
		TypeMeta: v1.TypeMeta{
			Kind:       "Role",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:        metadataName,
			Namespace:   namespace,
			Annotations: kroGenLabels,
		},
		Rules: policyRules,
	}
}

type resourcesByGroup map[string][]string

func (rbg resourcesByGroup) addGVR(gvr schema.GroupVersionResource) {
	resources, exist := rbg[gvr.Group]
	if !exist {
		resources = []string{gvr.Resource}
		rbg[gvr.Group] = resources
		return
	}

	found := false
	for _, resource := range resources {
		if resource == gvr.Resource {
			found = true
			break
		}
	}

	if !found {
		resources = append(resources, gvr.Resource)
		rbg[gvr.Group] = resources
	}
}

func (rbg resourcesByGroup) groups() []string {
	groups := []string{}
	for group := range rbg {
		groups = append(groups, group)
	}

	sort.Strings(groups)
	return groups
}

func marshalObject(object interface{}, _ string) ([]byte, error) {
	var b []byte
	var err error
	switch optOutputFormat {
	case "json":
		b, err = json.Marshal(object)
		if err != nil {
			return nil, err
		}
	case "yaml":
		b, err = yaml.Marshal(object)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported output type: %s", optOutputFormat)
	}

	return b, nil
}

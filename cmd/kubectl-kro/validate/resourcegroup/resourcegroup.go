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

package resourcegroup

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/awslabs/kro/api/v1alpha1"
	kroclient "github.com/awslabs/kro/internal/client"
	"github.com/awslabs/kro/internal/graph"
)

var (
	optResourceGroupFile string
)

func init() {
	Command.PersistentFlags().StringVarP(&optResourceGroupFile, "file", "f", "", "target resourcegroup file")
}

var Command = &cobra.Command{
	Use:     "rg",
	Aliases: []string{"resourcegroup"},
	Args:    cobra.NoArgs,
	Short:   "Validates a ResourceGroups",
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

		err = validateResourceGroup(&rg)
		if err != nil {
			fmt.Printf("❌ %s is not a valid ResourceGroup.\n", rg.Name)
			return err
		}

		fmt.Printf("✅ %s is valid ResourceGroup.\n", rg.Name)
		return nil
	},
}

func validateResourceGroup(rg *v1alpha1.ResourceGroup) error {
	set, err := kroclient.NewSet(kroclient.Config{})
	if err != nil {
		return nil
	}
	restConfig := set.RESTConfig()

	builder, err := graph.NewBuilder(restConfig)
	if err != nil {
		return err
	}

	_, err = builder.NewResourceGroup(rg)
	if err != nil {
		return err
	}
	return nil
}

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

package crd

import (
	"encoding/json"
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
	optOutputFormat string
)

func init() {
	Command.PersistentFlags().StringVarP(&optResourceGroupFile, "file", "f", "", "target resourcegroup file")
	Command.PersistentFlags().StringVarP(&optOutputFormat, "output", "o", "yaml", "output format (json|yaml)")
}

var Command = &cobra.Command{
	Use:     "crd",
	Aliases: []string{},
	Args:    cobra.NoArgs,
	Short:   "Generate the CRD from a given resourcegroup, following it's simple schema",
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
		
		err = generateCRD(&rg)
		if err != nil {
			return err
		}
		return nil
	},
}

func generateCRD(rg *v1alpha1.ResourceGroup) error {
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

	crd := processedRG.Instance.GetCRD()
	crd.Annotations = kroGenAnnotations

	b, err := marshalObject(crd, optOutputFormat)
	if err != nil {
		return err
	}

	fmt.Println(string(b))
	return nil
}

var (
	kroGenAnnotations = map[string]string{
		"kro.run/version": "dev",
	}
)

func marshalObject(object interface{}, format string) ([]byte, error) {
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

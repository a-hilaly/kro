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

package root

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/awslabs/kro/cmd/kubectl-kro/generate"
	"github.com/awslabs/kro/cmd/kubectl-kro/get"
	"github.com/awslabs/kro/cmd/kubectl-kro/install"
	packagecmd "github.com/awslabs/kro/cmd/kubectl-kro/package"
	"github.com/awslabs/kro/cmd/kubectl-kro/publish"
	"github.com/awslabs/kro/cmd/kubectl-kro/registry"
	"github.com/awslabs/kro/cmd/kubectl-kro/validate"
)

func init() {
	// rootCmd.PersistentFlags().StringVar(&ackConfigPath, "config-file", defaultConfigPath, "ackdev configuration file path")

	rootCmd.AddCommand(generate.Command)
	rootCmd.AddCommand(validate.Command)
	rootCmd.AddCommand(get.Command)
	rootCmd.AddCommand(packagecmd.Command)
	rootCmd.AddCommand(registry.Command)
	rootCmd.AddCommand(publish.Command)
	rootCmd.AddCommand(install.Command)
}

var rootCmd = &cobra.Command{
	Use:           "kubectl-kro",
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "A tool to manage ACK repositories, CRDs, development tools and testing",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

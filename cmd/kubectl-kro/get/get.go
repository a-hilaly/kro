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

package get

import (
	"github.com/spf13/cobra"

	"github.com/awslabs/kro/cmd/kubectl-kro/get/instance"
)

func init() {
	Command.AddCommand(instance.Command)
}

var Command = &cobra.Command{
	Use:     "get",
	Aliases: []string{},
	Args:    cobra.NoArgs,
	Short:   "Get kro related resources",
}

// Copyright 2021 Silvio Böhler
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"fmt"
	"os"

	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/check"
	"github.com/sboehler/knut/lib/model/registry"

	"github.com/spf13/cobra"
)

// CreateCheckCommand creates the command.
func CreateCheckCommand() *cobra.Command {

	var r checkRunner

	// Cmd is the balance command.
	c := &cobra.Command{
		Use:   "check",
		Short: "check the journal",
		Long:  `Check the journal.`,
		Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		Run:   r.run,
	}
	r.setupFlags(c)
	return c
}

type checkRunner struct {
	write bool
}

func (r *checkRunner) run(cmd *cobra.Command, args []string) {

	if err := r.execute(cmd, args); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%#v\n", err)
		os.Exit(1)
	}
}

func (r *checkRunner) setupFlags(c *cobra.Command) {
	c.Flags().BoolVar(&r.write, "write", false, "write")
}

func (r checkRunner) execute(cmd *cobra.Command, args []string) error {
	reg := registry.New()

	j, err := journal.FromPath(cmd.Context(), reg, args[0])
	if err != nil {
		return err
	}
	_, err = j.Process(
		check.Check(),
	)
	if err != nil {
		return err
	}
	return nil
}

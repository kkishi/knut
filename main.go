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

package main

import (
	"fmt"
	"os"

	"github.com/sboehler/knut/cmd"

	// enable importers here
	_ "github.com/sboehler/knut/cmd/importer/cumulus"
	_ "github.com/sboehler/knut/cmd/importer/interactivebrokers"
	_ "github.com/sboehler/knut/cmd/importer/postfinance"
	_ "github.com/sboehler/knut/cmd/importer/revolut"
	_ "github.com/sboehler/knut/cmd/importer/revolut2"
	_ "github.com/sboehler/knut/cmd/importer/supercard"
	_ "github.com/sboehler/knut/cmd/importer/swisscard"
	_ "github.com/sboehler/knut/cmd/importer/swisscard2"
	_ "github.com/sboehler/knut/cmd/importer/swissquote"
	_ "github.com/sboehler/knut/cmd/importer/ubsaccount"
	_ "github.com/sboehler/knut/cmd/importer/ubscard"
	_ "github.com/sboehler/knut/cmd/importer/viac"
	_ "github.com/sboehler/knut/cmd/importer/wise"
)

var version = "development"

func main() {
	c := cmd.CreateCmd(version)
	if err := c.Execute(); err != nil {
		fmt.Fprintln(c.ErrOrStderr(), err)
		os.Exit(1)
	}
}

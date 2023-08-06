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

package prices

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/printer"
	"github.com/sboehler/knut/lib/model"
	"github.com/sboehler/knut/lib/model/price"
	"github.com/sboehler/knut/lib/model/registry"
	"github.com/sboehler/knut/lib/quotes/yahoo"
	"github.com/sboehler/knut/lib/syntax"
	"github.com/sboehler/knut/lib/syntax/parser"
	"github.com/shopspring/decimal"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/multierr"

	"github.com/cheggaaa/pb/v3"
	"github.com/natefinch/atomic"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// CreateCmd creates the command.
func CreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch",
		Short: "Fetch quotes from Yahoo! Finance",
		Long:  `Fetch quotes from Yahoo! Finance based on the supplied configuration in yaml format. See doc/prices.yaml for an example.`,

		Args: cobra.ExactValidArgs(1),

		Run: run,
	}
}

func run(cmd *cobra.Command, args []string) {
	if err := execute2(cmd, args); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		os.Exit(1)
	}
}

const concurrency = 5

func execute2(cmd *cobra.Command, args []string) error {
	ctx := registry.New()
	configs, err := readConfig(args[0])
	if err != nil {
		return err
	}
	p := pool.New().WithMaxGoroutines(concurrency).WithErrors()
	bar := pb.StartNew(len(configs))

	for _, cfg := range configs {
		cfg := cfg
		p.Go(func() error {
			defer bar.Increment()
			return fetch(ctx, args[0], cfg)
		})
	}
	return multierr.Combine(p.Wait())
}

func fetch(jctx *registry.Registry, f string, cfg config) error {
	absPath := filepath.Join(filepath.Dir(f), cfg.File)
	l, err := readFile(jctx, absPath)
	if err != nil {
		return err
	}
	if err := fetchPrices(jctx, cfg, time.Now().AddDate(-1, 0, 0), time.Now(), l); err != nil {
		return err
	}
	if err := writeFile(jctx, l, absPath); err != nil {
		return err
	}
	return nil
}

func readConfig(path string) ([]config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.SetStrict(true)
	var t []config
	if err := dec.Decode(&t); err != nil {
		return nil, err
	}
	return t, nil
}

func readFile(ctx *registry.Registry, filepath string) (res map[time.Time]*model.Price, err error) {
	text, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	p := parser.New(string(text), filepath)
	if err := p.Advance(); err != nil {
		return nil, err
	}
	f, err := p.ParseFile()
	if err != nil {
		return nil, err
	}
	prices := make(map[time.Time]*model.Price)
	for _, d := range f.Directives {
		if p, ok := d.Directive.(syntax.Price); ok {
			m, err := price.Create(ctx, &p)
			if err != nil {
				return nil, err
			}
			prices[m.Date] = m
		} else {
			return nil, fmt.Errorf("unexpected directive in prices file: %v", d)
		}
	}
	return prices, nil
}

func fetchPrices(ctx *registry.Registry, cfg config, t0, t1 time.Time, results map[time.Time]*model.Price) error {
	var (
		c                 = yahoo.New()
		quotes            []yahoo.Quote
		commodity, target *model.Commodity
		err               error
	)
	if quotes, err = c.Fetch(cfg.Symbol, t0, t1); err != nil {
		return err
	}
	if commodity, err = ctx.GetCommodity(cfg.Commodity); err != nil {
		return err
	}
	if target, err = ctx.GetCommodity(cfg.TargetCommodity); err != nil {
		return err
	}
	for _, i := range quotes {
		results[i.Date] = &model.Price{
			Date:      i.Date,
			Commodity: commodity,
			Target:    target,
			Price:     decimal.NewFromFloat(i.Close),
		}
	}
	return nil
}

func writeFile(ctx *registry.Registry, prices map[time.Time]*model.Price, filepath string) error {
	j := journal.New(ctx)
	for _, price := range prices {
		j.AddPrice(price)
	}
	r, w := io.Pipe()
	go func() {
		defer w.Close()
		_, err := printer.NewPrinter().PrintJournal(w, j)
		if err != nil {
			panic(err)
		}
	}()
	return atomic.WriteFile(filepath, r)
}

type config struct {
	Symbol          string `yaml:"symbol"`
	File            string `yaml:"file"`
	Commodity       string `yaml:"commodity"`
	TargetCommodity string `yaml:"target_commodity"`
}

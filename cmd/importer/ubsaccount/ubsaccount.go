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

package ubsaccount

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dimchansky/utfbom"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/sboehler/knut/cmd/flags"
	"github.com/sboehler/knut/cmd/importer"
	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/model"
	"github.com/sboehler/knut/lib/model/posting"
	"github.com/sboehler/knut/lib/model/registry"
	"github.com/sboehler/knut/lib/model/transaction"
)

// CreateCmd creates the cobra command.
func CreateCmd() *cobra.Command {

	var r runner

	cmd := &cobra.Command{
		Use:   "ch.ubs.account",
		Short: "Import UBS CSV account statements",

		Args: cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),

		Run: r.run,
	}
	r.setupFlags(cmd)
	return cmd
}

type runner struct {
	accountFlag flags.AccountFlag
}

func (r *runner) setupFlags(cmd *cobra.Command) {
	cmd.Flags().VarP(&r.accountFlag, "account", "a", "account name")
	cmd.MarkFlagRequired("account")
}

func (r *runner) run(cmd *cobra.Command, args []string) {
	if err := r.runE(cmd, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (r *runner) runE(cmd *cobra.Command, args []string) error {
	var (
		reader *bufio.Reader
		reg    = registry.New()
		err    error
	)
	if reader, err = flags.OpenFile(args[0]); err != nil {
		return err
	}
	p := Parser{
		registry: reg,
		reader:   csv.NewReader(utfbom.SkipOnly(reader)),
		builder:  journal.New(),
	}
	if p.account, err = r.accountFlag.Value(reg.Accounts()); err != nil {
		return err
	}
	if err = p.parse(); err != nil {
		return err
	}
	out := bufio.NewWriter(cmd.OutOrStdout())
	defer out.Flush()
	return journal.Print(out, p.builder.Build())
}

func init() {
	importer.RegisterImporter(CreateCmd)
}

// Parser is a parser for account statements
type Parser struct {
	registry *model.Registry
	reader   *csv.Reader
	account  *model.Account
	builder  *journal.Builder

	currency *model.Commodity
}

func (p *Parser) parse() error {
	p.reader.LazyQuotes = true
	p.reader.TrimLeadingSpace = true
	p.reader.Comma = ';'
	p.reader.FieldsPerRecord = -1

	kv, err := p.readKeyValues()
	if err != nil {
		return err
	}
	if s, ok := kv["Valued in:"]; ok {
		if p.currency, err = p.registry.Commodities().Get(s); err != nil {
			return err
		}
	} else {
		p.currency = p.registry.Commodities().MustGet("CHF")
	}
	for {
		ok, err := p.readBookingLine()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
	}
	p.maybeAssertBalance(kv["Closing balance:"], kv["Until:"])
	return nil
}

func (p *Parser) maybeAssertBalance(balance, date string) {
	b, err := decimal.NewFromString(balance)
	if err != nil {
		return
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return
	}
	p.builder.Add(&model.Assertion{
		Date: d,
		Balances: []model.Balance{
			{
				Account:   p.account,
				Commodity: p.currency,
				Quantity:  b,
			},
		},
	})
}

func (p *Parser) readKeyValues() (map[string]string, error) {
	res := make(map[string]string)
	for {
		rec, err := p.reader.Read()
		if err != nil {
			return nil, err
		}
		if len(rec) != 3 {
			return res, nil
		}
		res[rec[0]] = rec[1]
	}
}

type bookingField int

const (
	bfTradeDate bookingField = iota
	bfTradeTime
	bfBookingDate
	bfValueDate
	bfCurrency
	bfDebit
	bfCredit
	bfIndividualAmount
	bfBalance
	bfTransactionNo
	bfDescription1
	bfDescription2
	bfDescription3
	bfFootnotes
	bfNumColumns
)

func (p *Parser) readBookingLine() (bool, error) {
	rec, err := p.reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	if len(rec) != 15 {
		return false, fmt.Errorf("unexpected number of columns: %d", len(rec))
	}
	date, err := time.Parse("2006-01-02", rec[bfTradeDate])
	if err != nil {
		return false, err
	}
	quantity, err := parseAmount(rec[bfDebit], rec[bfCredit])
	if err != nil {
		return false, err
	}
	desc := strings.TrimSpace(rec[bfDescription1])
	desc = desc[1 : len(desc)-1] // TODO: Check that desc looked like "text".
	p.builder.Add(transaction.Builder{
		Date:        date,
		Description: strings.TrimSpace(desc),
		Postings: posting.Builder{
			Credit:    p.registry.Accounts().TBDAccount(),
			Debit:     p.account,
			Commodity: p.currency,
			Quantity:  quantity,
		}.Build(),
	}.Build())
	return true, nil
}

func parseAmount(debit, credit string) (decimal.Decimal, error) {
	switch {
	case len(debit) > 0 && len(credit) == 0:
		return decimal.NewFromString(debit)
	case len(debit) == 0 && len(credit) > 0:
		return decimal.NewFromString(credit)
	default:
		return decimal.Zero, fmt.Errorf("invalid amount fields %q %q", debit, credit)
	}
}

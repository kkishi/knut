package process

import (
	"context"
	"fmt"
	"time"

	"github.com/sboehler/knut/lib/common/amounts"
	"github.com/sboehler/knut/lib/common/cpr"
	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/ast"
	"github.com/sboehler/knut/lib/journal/val"
	"golang.org/x/sync/errgroup"
)

// Valuator produces valuated days.
type Valuator struct {
	Context   journal.Context
	Valuation *journal.Commodity

	values            amounts.Amounts
	amounts           amounts.Amounts
	normalized        journal.NormalizedPrices
	date              time.Time
	newPrices, newTrx bool
}

// ProcessStream computes prices.
func (pr *Valuator) ProcessStream(ctx context.Context, inCh <-chan *val.Day) (chan *val.Day, chan error) {
	errCh := make(chan error)
	resCh := make(chan *val.Day, 100)

	values := make(amounts.Amounts)

	go func() {
		defer close(resCh)
		defer close(errCh)

		for {
			day, ok, err := cpr.Pop(ctx, inCh)
			if !ok || err != nil {
				return
			}
			day.Values = values

			for _, t := range day.Day.Transactions {
				if errors := pr.valuateAndBookTransaction(ctx, day, t); cpr.Push(ctx, errCh, errors...) != nil {
					return
				}
			}

			pr.computeValuationTransactions(day)
			values = values.Clone()

			if cpr.Push(ctx, resCh, day) != nil {
				return
			}
		}
	}()

	return resCh, errCh
}

// Process2 computes prices.
func (pr *Valuator) Process2(ctx context.Context, g *errgroup.Group, inCh <-chan *ast.Day) <-chan *ast.Day {
	resCh := make(chan *ast.Day, 100)

	g.Go(func() error {
		defer close(resCh)

		values := make(amounts.Amounts)
		for {
			day, ok, err := cpr.Pop(ctx, inCh)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			day.Value = values

			for _, t := range day.Transactions {
				for i, posting := range t.Postings {
					if pr.Valuation != nil && pr.Valuation != posting.Commodity {
						var err error
						if posting.Amount, err = day.Normalized.Valuate(posting.Commodity, posting.Amount); err != nil {
							return err
						}
					}
					values.Book(posting.Credit, posting.Debit, posting.Amount, posting.Commodity)
					t.Postings[i] = posting
				}
			}

			pr.processValuationTransactions2(day)
			values = values.Clone()

			if err := cpr.Push(ctx, resCh, day); err != nil {
				return err
			}
		}
		return nil
	})

	return resCh
}

func (pr *Valuator) processValuationTransactions2(d *ast.Day) error {
	for pos, va := range d.Amounts {
		if pos.Commodity == pr.Valuation {
			continue
		}
		at := pos.Account.Type()
		if at != journal.ASSETS && at != journal.LIABILITIES {
			continue
		}
		value, err := d.Normalized.Valuate(pos.Commodity, va)
		if err != nil {
			return fmt.Errorf("no valuation found for commodity %s", pos.Commodity)
		}
		diff := value.Sub(d.Value[pos])
		if diff.IsZero() {
			continue
		}
		if !diff.IsZero() {
			credit := pr.Context.ValuationAccountFor(pos.Account)
			t := &ast.Transaction{
				Date:        d.Date,
				Description: fmt.Sprintf("Adjust value of %v in account %v", pos.Commodity, pos.Account),
				Postings: []ast.Posting{
					ast.NewPostingWithTargets(credit, pos.Account, pos.Commodity, diff, []*journal.Commodity{pos.Commodity}),
				},
			}
			d.Value.Book(credit, pos.Account, diff, pos.Commodity)
			d.Transactions = append(d.Transactions, t)
		}
	}
	return nil

}

func (pr Valuator) valuateAndBookTransaction(ctx context.Context, day *val.Day, t *ast.Transaction) []error {
	var errors []error
	tx := t.Clone()
	for i, posting := range t.Postings {
		if pr.Valuation != nil && pr.Valuation != posting.Commodity {
			var err error
			if posting.Amount, err = day.Prices.Valuate(posting.Commodity, posting.Amount); err != nil {
				errors = append(errors, err)
			}
		}
		day.Values.Book(posting.Credit, posting.Debit, posting.Amount, posting.Commodity)
		tx.Postings[i] = posting
	}
	day.Transactions = append(day.Transactions, tx)
	return errors
}

// computeValuationTransactions checks whether the valuation for the positions
// corresponds to the amounts. If not, the difference is due to a valuation
// change of the previous amount, and a transaction is created to adjust the
// valuation.
func (pr Valuator) computeValuationTransactions(day *val.Day) {
	if pr.Valuation == nil {
		return
	}
	for pos, va := range day.Day.Amounts {
		if pos.Commodity == pr.Valuation {
			continue
		}
		var at = pos.Account.Type()
		if at != journal.ASSETS && at != journal.LIABILITIES {
			continue
		}
		value, err := day.Prices.Valuate(pos.Commodity, va)
		if err != nil {
			panic(fmt.Sprintf("no valuation found for commodity %s", pos.Commodity))
		}
		diff := value.Sub(day.Values[pos])
		if diff.IsZero() {
			continue
		}
		if !diff.IsZero() {
			credit := pr.Context.ValuationAccountFor(pos.Account)
			day.Transactions = append(day.Transactions, &ast.Transaction{
				Date:        day.Date,
				Description: fmt.Sprintf("Adjust value of %v in account %v", pos.Commodity, pos.Account),
				Postings: []ast.Posting{
					ast.NewPostingWithTargets(credit, pos.Account, pos.Commodity, diff, []*journal.Commodity{pos.Commodity}),
				},
			})
			day.Values.Book(credit, pos.Account, diff, pos.Commodity)
		}
	}
}

// Process valuates the transactions and inserts valuation transactions.
func (pr *Valuator) Process(ctx context.Context, d ast.Dated, next func(ast.Dated) bool) error {
	if pr.values == nil {
		pr.values = make(amounts.Amounts)
	}

	if pr.date != d.Date {
		if pr.newPrices {
			if err := pr.processValuationTransactions(next); err != nil {
				return err
			}
		}
		if pr.newPrices || pr.newTrx {
			next(ast.Dated{Date: pr.date, Elem: pr.values.Clone()})
		}
		pr.newPrices = false
		pr.newTrx = false
		pr.date = d.Date
	}
	switch dd := d.Elem.(type) {

	case journal.NormalizedPrices:
		pr.newPrices = true
		pr.normalized = dd

	case *ast.Transaction:
		pr.newTrx = true
		// valuate transaction
		for i, posting := range dd.Postings {
			if pr.Valuation != nil && pr.Valuation != posting.Commodity {
				var err error
				if posting.Amount, err = pr.normalized.Valuate(posting.Commodity, posting.Amount); err != nil {
					return err
				}
			}
			pr.values.Book(posting.Credit, posting.Debit, posting.Amount, posting.Commodity)
			dd.Postings[i] = posting
		}
		next(d)

	case amounts.Amounts:
		pr.amounts = dd

	default:
		next(d)
	}
	return nil
}

// Finalize implements Finalize.
func (pr *Valuator) Finalize(ctx context.Context, next func(ast.Dated) bool) error {
	if pr.newPrices {
		if err := pr.processValuationTransactions(next); err != nil {
			return err
		}
	}
	if pr.newPrices || pr.newTrx {
		next(ast.Dated{Date: pr.date, Elem: pr.values.Clone()})
	}
	return nil
}

func (pr *Valuator) processValuationTransactions(next func(ast.Dated) bool) error {
	for pos, va := range pr.amounts {
		if pos.Commodity == pr.Valuation {
			continue
		}
		at := pos.Account.Type()
		if at != journal.ASSETS && at != journal.LIABILITIES {
			continue
		}
		value, err := pr.normalized.Valuate(pos.Commodity, va)
		if err != nil {
			return fmt.Errorf("no valuation found for commodity %s", pos.Commodity)
		}
		diff := value.Sub(pr.values[pos])
		if diff.IsZero() {
			continue
		}
		if !diff.IsZero() {
			credit := pr.Context.ValuationAccountFor(pos.Account)
			t := &ast.Transaction{
				Date:        pr.date,
				Description: fmt.Sprintf("Adjust value of %v in account %v", pos.Commodity, pos.Account),
				Postings: []ast.Posting{
					ast.NewPostingWithTargets(credit, pos.Account, pos.Commodity, diff, []*journal.Commodity{pos.Commodity}),
				},
			}
			pr.values.Book(credit, pos.Account, diff, pos.Commodity)
			if !next(ast.Dated{Date: pr.date, Elem: t}) {
				return nil
			}
		}
	}
	return nil

}

package process

import (
	"context"
	"fmt"
	"time"

	"github.com/sboehler/knut/lib/common/cpr"
	"github.com/sboehler/knut/lib/journal"
	"github.com/sboehler/knut/lib/journal/ast"
	"github.com/sboehler/knut/lib/journal/ast/parser"
	"golang.org/x/sync/errgroup"
)

// ASTBuilder builds an abstract syntax tree.
type ASTBuilder struct {
	Context journal.Context

	Journal string
	Expand  bool
	Filter  journal.Filter

	ast *ast.AST
}

// Source2 is a source of days.
func (pr *ASTBuilder) Source2(ctx context.Context, g *errgroup.Group) <-chan *ast.Day {
	pr.ast = &ast.AST{
		Context: pr.Context,
		Days:    make(map[time.Time]*ast.Day),
	}
	p := parser.RecursiveParser{
		Context: pr.Context,
		File:    pr.Journal,
	}
	resCh := make(chan *ast.Day)

	g.Go(func() error {
		defer close(resCh)

		ch, errCh := p.Parse(ctx)
		for {
			d, ok, err := cpr.Get(ch, errCh)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			switch t := d.(type) {

			case *ast.Open:
				pr.ast.AddOpen(t)

			case *ast.Price:
				pr.ast.AddPrice(t)

			case *ast.Transaction:
				var filtered []ast.Posting
				for _, p := range t.Postings {
					if p.Matches(pr.Filter) {
						filtered = append(filtered, p)
					}
				}
				if len(filtered) == 0 {
					break
				}
				if len(filtered) < len(t.Postings) {
					t.Postings = filtered
				}
				if len(t.AddOns) > 0 {
					for _, addOn := range t.AddOns {
						switch acc := addOn.(type) {
						case *ast.Accrual:
							for _, ts := range acc.Expand(t) {
								pr.ast.AddTransaction(ts)
							}
						default:
							panic(fmt.Sprintf("unknown addon: %#v", acc))
						}
					}
				} else {
					pr.ast.AddTransaction(t)
				}

			case *ast.Assertion:
				if !pr.Filter.MatchAccount(t.Account) {
					break
				}
				if !pr.Filter.MatchCommodity(t.Commodity) {
					break
				}
				pr.ast.AddAssertion(t)

			case *ast.Value:
				if !pr.Filter.MatchAccount(t.Account) {
					break
				}
				if !pr.Filter.MatchCommodity(t.Commodity) {
					break
				}
				pr.ast.AddValue(t)

			case *ast.Close:
				if !pr.Filter.MatchAccount(t.Account) {
					break
				}
				pr.ast.AddClose(t)

			default:
				return fmt.Errorf("unknown: %#v", t)
			}
		}
		for _, d := range pr.ast.SortedDays() {
			if err := cpr.Push(ctx, resCh, d); err != nil {
				return err
			}
		}
		return nil
	})
	return resCh
}

// BuildAST reads directives from the given channel and
// builds a Ledger if successful.
func (pr *ASTBuilder) BuildAST(ctx context.Context, inCh <-chan ast.Directive) (<-chan *ast.AST, <-chan error) {
	resCh := make(chan *ast.AST)
	errCh := make(chan error)
	go func() {
		defer close(resCh)
		defer close(errCh)
		res := &ast.AST{
			Context: pr.Context,
			Days:    make(map[time.Time]*ast.Day),
		}
		for {
			d, ok, err := cpr.Pop(ctx, inCh)
			if err != nil {
				return
			}
			if !ok {
				cpr.Push(ctx, resCh, res)
				return
			}
			switch t := d.(type) {
			case *ast.Open:
				res.AddOpen(t)
			case *ast.Price:
				res.AddPrice(t)
			case *ast.Transaction:
				res.AddTransaction(t)
			case *ast.Assertion:
				res.AddAssertion(t)
			case *ast.Value:
				res.AddValue(t)
			case *ast.Close:
				res.AddClose(t)
			default:
				if cpr.Push(ctx, errCh, fmt.Errorf("unknown: %#v", t)) != nil {
					return
				}
			}
		}
	}()
	return resCh, errCh
}

var _ ast.Processor = (*Sorter)(nil)

// Sorter sorts the directives.
type Sorter struct {
	Context journal.Context
	AST     *ast.AST
}

// Process implements Processor.
func (srt *Sorter) Process(ctx context.Context, d ast.Dated, next func(ast.Dated) bool) error {
	if srt.AST == nil {
		srt.AST = &ast.AST{
			Context: srt.Context,
			Days:    make(map[time.Time]*ast.Day),
		}
	}

	switch t := d.Elem.(type) {
	case *ast.Open:
		srt.AST.AddOpen(t)
	case *ast.Price:
		srt.AST.AddPrice(t)
	case *ast.Transaction:
		srt.AST.AddTransaction(t)
	case *ast.Assertion:
		srt.AST.AddAssertion(t)
	case *ast.Value:
		srt.AST.AddValue(t)
	case *ast.Close:
		srt.AST.AddClose(t)
	default:
		return fmt.Errorf("unknown: %#v", t)
	}
	return nil
}

// Finalize starts sending sorted directives.
func (srt *Sorter) Finalize(ctx context.Context, next func(ast.Dated) bool) error {

	for _, d := range srt.AST.SortedDays() {
		for _, o := range d.Openings {
			if !next(ast.Dated{Date: o.Date, Elem: o}) {
				return nil
			}
		}
		for _, o := range d.Prices {
			if !next(ast.Dated{Date: o.Date, Elem: o}) {
				return nil
			}
		}
		for _, o := range d.Transactions {
			if !next(ast.Dated{Date: o.Date, Elem: o}) {
				return nil
			}
		}
		for _, o := range d.Values {
			if !next(ast.Dated{Date: o.Date, Elem: o}) {
				return nil
			}
		}
		for _, o := range d.Assertions {
			if !next(ast.Dated{Date: o.Date, Elem: o}) {
				return nil
			}
		}
		for _, o := range d.Closings {
			if !next(ast.Dated{Date: o.Date, Elem: o}) {
				return nil
			}
		}
	}
	return nil
}

// ASTExpander expands and filters the given AST.
type ASTExpander struct {
	Expand bool
	Filter journal.Filter
}

// ExpandAndFilterAST processes the given AST.
func (pr *ASTExpander) ExpandAndFilterAST(ctx context.Context, inCh <-chan *ast.AST) (<-chan *ast.AST, <-chan error) {
	resCh := make(chan *ast.AST)
	errCh := make(chan error)
	go func() {
		defer close(resCh)
		defer close(errCh)

		for {
			d, ok, err := cpr.Pop(ctx, inCh)
			if !ok || err != nil {
				return
			}
			r := pr.process(d)
			if cpr.Push(ctx, resCh, r) != nil {
				return
			}
		}
	}()
	return resCh, errCh
}

// process processes the AST and returns a new copy.
func (pr *ASTExpander) process(a *ast.AST) *ast.AST {
	res := &ast.AST{
		Context: a.Context,
		Days:    make(map[time.Time]*ast.Day),
	}
	for d, astDay := range a.Days {
		day := res.Day(d)

		day.Openings = astDay.Openings
		day.Prices = astDay.Prices

		for _, val := range astDay.Values {
			if pr.Filter.MatchAccount(val.Account) && pr.Filter.MatchCommodity(val.Commodity) {
				day.Values = append(day.Values, val)
			}
		}

		for _, trx := range astDay.Transactions {
			pr.expandTransaction(res, trx)
		}

		for _, a := range astDay.Assertions {
			if pr.Filter.MatchAccount(a.Account) && pr.Filter.MatchCommodity(a.Commodity) {
				day.Assertions = append(day.Assertions, a)
			}
		}

		for _, c := range astDay.Closings {
			if pr.Filter.MatchAccount(c.Account) {
				day.Closings = append(day.Closings, c)
			}
		}
	}
	return res
}

// ProcessTransaction adds a transaction directive.
func (pr *ASTExpander) expandTransaction(a *ast.AST, t *ast.Transaction) {
	if pr.Expand && len(t.AddOns) > 0 {
		for _, addOn := range t.AddOns {
			switch acc := addOn.(type) {
			case *ast.Accrual:
				for _, ts := range acc.Expand(t) {
					pr.expandTransaction(a, ts)
				}
			default:
				panic(fmt.Sprintf("unknown addon: %#v", acc))
			}
		}
		return
	}
	var filtered []ast.Posting
	for _, p := range t.Postings {
		if p.Matches(pr.Filter) {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) > 0 {
		if len(filtered) < len(t.Postings) {
			t = t.Clone()
			t.Postings = filtered
		}
		a.AddTransaction(t)
	}
}

// Expander expands value directives.
type Expander struct{}

// Process expands transactions.
func (exp *Expander) Process(ctx context.Context, d ast.Dated, next func(ast.Dated) bool) error {
	if t, ok := d.Elem.(*ast.Transaction); ok {
		for _, addOn := range t.AddOns {
			switch acc := addOn.(type) {
			case *ast.Accrual:
				for _, ts := range acc.Expand(t) {
					if !next(ast.Dated{
						Date: ts.Date,
						Elem: ts,
					}) {
						return nil
					}
				}
				return nil
			default:
				panic(fmt.Sprintf("unknown addon: %#v", acc))
			}
		}
	}
	next(d)
	return nil
}

// PostingFilter filters postings and certain directives
// which are not applicable without the filtered postings.
type PostingFilter struct {
	Filter journal.Filter
}

// Process expands transactions.
func (pf *PostingFilter) Process(ctx context.Context, d ast.Dated, next func(ast.Dated) bool) error {
	switch t := d.Elem.(type) {

	case *ast.Transaction:
		var filtered []ast.Posting
		for _, p := range t.Postings {
			if p.Matches(pf.Filter) {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			break
		}
		if len(filtered) < len(t.Postings) {
			t.Postings = filtered
		}
		next(d)

	case *ast.Value:
		if !pf.Filter.MatchAccount(t.Account) {
			break
		}
		if !pf.Filter.MatchCommodity(t.Commodity) {
			break
		}
		next(d)

	case *ast.Assertion:
		if !pf.Filter.MatchAccount(t.Account) {
			break
		}
		if !pf.Filter.MatchCommodity(t.Commodity) {
			break
		}
		next(d)

	case *ast.Close:
		if !pf.Filter.MatchAccount(t.Account) {
			break
		}
		next(d)

	default:
		next(d)
	}
	return nil
}

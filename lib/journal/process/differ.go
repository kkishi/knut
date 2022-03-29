package process

import (
	"context"

	"github.com/sboehler/knut/lib/common/amounts"
	"github.com/sboehler/knut/lib/common/cpr"
	"github.com/sboehler/knut/lib/journal/ast"
	"github.com/sboehler/knut/lib/journal/val"
	"golang.org/x/sync/errgroup"
)

// Differ filters the incoming days according to the dates
// specified.
type Differ struct {
	Diff bool

	prev amounts.Amounts
}

// ProcessStream does the filtering.
func (pf Differ) ProcessStream(ctx context.Context, inCh <-chan *val.Day) (<-chan *val.Day, <-chan error) {
	errCh := make(chan error)
	if !pf.Diff {
		close(errCh)
		return inCh, errCh
	}
	resCh := make(chan *val.Day, 100)

	go func() {
		defer close(resCh)
		defer close(errCh)

		var prev amounts.Amounts
		for {
			d, ok, err := cpr.Pop(ctx, inCh)
			if !ok || err != nil {
				return
			}
			res := &val.Day{
				Date:   d.Date,
				Values: d.Values.Clone().Minus(prev),
			}
			if cpr.Push(ctx, resCh, res) != nil {
				return
			}
			prev = d.Values
		}
	}()
	return resCh, errCh
}

// Process2 does the diffing.
func (pf Differ) Process2(ctx context.Context, g *errgroup.Group, inCh <-chan *ast.Day) <-chan *ast.Day {
	if !pf.Diff {
		return inCh
	}
	resCh := make(chan *ast.Day, 100)

	g.Go(func() error {
		defer close(resCh)

		var prev amounts.Amounts
		for {
			d, ok, err := cpr.Pop(ctx, inCh)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			diff := d.Value.Clone().Minus(prev)

			prev = d.Value
			d.Value = diff
			if err := cpr.Push(ctx, resCh, d); err != nil {
				return err
			}
		}
		return nil
	})
	return resCh
}

var _ ast.Processor = (*Differ)(nil)

// Process diffs the amounts.
func (pf *Differ) Process(ctx context.Context, d ast.Dated, next func(ast.Dated) bool) error {
	if v, ok := d.Elem.(amounts.Amounts); ok {
		res := ast.Dated{
			Date: d.Date,
			Elem: v.Clone().Minus(pf.prev),
		}
		pf.prev = v
		next(res)
	}
	return nil
}

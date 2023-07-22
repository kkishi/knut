package parser

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sboehler/knut/lib/syntax"
)

func TestParseCommodity(t *testing.T) {
	for _, test := range []struct {
		text    string
		want    syntax.Commodity
		wantErr bool
	}{
		{
			text: "foobar",
			want: syntax.Commodity{Start: 0, End: 6},
		},
		{
			text:    "",
			want:    syntax.Commodity{Start: 0, End: 0},
			wantErr: true,
		},
		{
			text:    "(foobar)",
			want:    syntax.Commodity{Start: 0, End: 0},
			wantErr: true,
		},
	} {
		t.Run(test.text, func(t *testing.T) {
			p := setupParser(t, test.text)

			got, err := p.parseCommodity()

			if (err != nil) != test.wantErr || !cmp.Equal(got, test.want, cmpopts.IgnoreFields(syntax.Commodity{}, "Text")) {
				t.Fatalf("p.parseCommodity() = %#v, %#v, want %#v, error presence %t", got, err, test.want, test.wantErr)
			}
		})
	}
}

func setupParser(t *testing.T, text string) *Parser {
	t.Helper()
	parser := New(text, "")
	if err := parser.Advance(); err != nil {
		t.Fatalf("s.Advance() = %v, want nil", err)
	}
	return parser
}

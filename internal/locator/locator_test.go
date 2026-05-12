package locator

import (
	"errors"
	"strings"
	"testing"
)

const sample = `function bar() {
    return 1;
}

export function baz(x: number): number {
    return x + 1;
}

export const qux = (s: string) => s.trim();

class Widget {
    render() {
        return "<div/>";
    }
}
`

func TestLocate(t *testing.T) {
	src := []byte(sample)

	cases := []struct {
		name           string
		symbol         string
		wantKind       Kind
		wantStartLine  uint32
		sliceContains  string
		exportedPrefix bool
	}{
		{"plain function", "bar", KindFunction, 1, "function bar()", false},
		{"exported function", "baz", KindFunction, 5, "export function baz", true},
		{"arrow const", "qux", KindArrowConst, 9, "qux = (s: string) =>", true},
		{"class method", "render", KindMethod, 12, "render()", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sym, err := Locate(src, tc.symbol)
			if err != nil {
				t.Fatalf("Locate(%q) returned error: %v", tc.symbol, err)
			}
			if sym.Name != tc.symbol {
				t.Errorf("Name = %q, want %q", sym.Name, tc.symbol)
			}
			if sym.Kind != tc.wantKind {
				t.Errorf("Kind = %v, want %v", sym.Kind, tc.wantKind)
			}
			if sym.StartLine != tc.wantStartLine {
				t.Errorf("StartLine = %d, want %d", sym.StartLine, tc.wantStartLine)
			}
			if sym.EndByte <= sym.StartByte {
				t.Errorf("EndByte (%d) must be > StartByte (%d)", sym.EndByte, sym.StartByte)
			}
			slice := string(src[sym.StartByte:sym.EndByte])
			if !strings.Contains(slice, tc.sliceContains) {
				t.Errorf("range slice missing %q; got:\n%s", tc.sliceContains, slice)
			}
			if tc.exportedPrefix && !strings.HasPrefix(slice, "export ") {
				t.Errorf("expected range to begin with `export `; got prefix %q", slice[:min(len(slice), 16)])
			}
		})
	}
}

func TestLocateNotFoundSuggestsClosest(t *testing.T) {
	src := []byte(`function bar() {}
export function baz() {}
`)
	_, err := Locate(src, "barz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected *NotFoundError, got %T (%v)", err, err)
	}
	if nfe.Name != "barz" {
		t.Errorf("Name = %q, want barz", nfe.Name)
	}
	if len(nfe.Suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}
	if nfe.Suggestions[0] != "bar" {
		t.Errorf("top suggestion = %q, want bar (all: %v)", nfe.Suggestions[0], nfe.Suggestions)
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error message missing suggestion hint: %q", err.Error())
	}
}

func TestLocateNotFoundNoCloseMatch(t *testing.T) {
	src := []byte(`function alpha() {}
function beta() {}
`)
	_, err := Locate(src, "zzzzzzz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected *NotFoundError, got %T", err)
	}
	if len(nfe.Suggestions) != 0 {
		t.Errorf("expected no suggestions for distant target, got %v", nfe.Suggestions)
	}
}

func TestLocateFirstWinsAcrossClasses(t *testing.T) {
	src := []byte(`class A {
    same() { return 1; }
}
class B {
    same() { return 2; }
}
`)
	sym, err := Locate(src, "same")
	if err != nil {
		t.Fatal(err)
	}
	if sym.StartLine != 2 {
		t.Errorf("expected first occurrence at line 2, got %d", sym.StartLine)
	}
}

func TestLocateExportedConstRangeCoversExport(t *testing.T) {
	src := []byte(`export const greeter = (n: string) => "hi " + n;
`)
	sym, err := Locate(src, "greeter")
	if err != nil {
		t.Fatal(err)
	}
	slice := string(src[sym.StartByte:sym.EndByte])
	if !strings.HasPrefix(slice, "export const greeter") {
		t.Errorf("expected slice to start with `export const greeter`, got %q", slice)
	}
}

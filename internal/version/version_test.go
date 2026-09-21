package version

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in      string
		want    Version
		wantErr bool
	}{
		{"0.0.1", Version{0, 0, 1}, false},
		{"1.2.3", Version{1, 2, 3}, false},
		{"v1.2.3", Version{1, 2, 3}, false},
		{"  2.0.0\n", Version{2, 0, 0}, false},
		{"1.2", Version{}, true},
		{"1.2.3.4", Version{}, true},
		{"a.b.c", Version{}, true},
		{"1.-2.3", Version{}, true},
		{"", Version{}, true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) : erreur attendue", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) : erreur inattendue %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %v, attendu %v", c.in, got, c.want)
		}
	}
}

func TestBumps(t *testing.T) {
	base := Version{1, 2, 3}
	if got := base.BumpMajor(); got != (Version{2, 0, 0}) {
		t.Errorf("BumpMajor = %v", got)
	}
	if got := base.BumpMinor(); got != (Version{1, 3, 0}) {
		t.Errorf("BumpMinor = %v", got)
	}
	if got := base.BumpFix(); got != (Version{1, 2, 4}) {
		t.Errorf("BumpFix = %v", got)
	}
	if got := base.Tag(); got != "v1.2.3" {
		t.Errorf("Tag = %q", got)
	}
}

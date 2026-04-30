package controllers

import (
	"reflect"
	"sort"
	"testing"
)

// === BuildModelCodeRangeWhere ===

func TestBuildModelCodeRangeWhere_BothEmpty(t *testing.T) {
	frag, args := BuildModelCodeRangeWhere("p.model_code", "", "")
	if frag != "" {
		t.Errorf("expected empty frag, got %q", frag)
	}
	if args != nil {
		t.Errorf("expected nil args, got %v", args)
	}
}

func TestBuildModelCodeRangeWhere_OnlyFrom(t *testing.T) {
	frag, args := BuildModelCodeRangeWhere("p.model_code", "GB1000", "")
	want := "UPPER(p.model_code) >= UPPER(?)"
	if frag != want {
		t.Errorf("frag\n got: %q\nwant: %q", frag, want)
	}
	if !reflect.DeepEqual(args, []interface{}{"GB1000"}) {
		t.Errorf("args got %v, want [GB1000]", args)
	}
}

func TestBuildModelCodeRangeWhere_OnlyTo(t *testing.T) {
	frag, args := BuildModelCodeRangeWhere("p.model_code", "", "GB9999")
	want := "UPPER(p.model_code) <= UPPER(?)"
	if frag != want {
		t.Errorf("frag\n got: %q\nwant: %q", frag, want)
	}
	if !reflect.DeepEqual(args, []interface{}{"GB9999"}) {
		t.Errorf("args got %v, want [GB9999]", args)
	}
}

func TestBuildModelCodeRangeWhere_Both(t *testing.T) {
	frag, args := BuildModelCodeRangeWhere("p.model_code", "GB1000", "GB9999")
	want := "UPPER(p.model_code) >= UPPER(?) AND UPPER(p.model_code) <= UPPER(?)"
	if frag != want {
		t.Errorf("frag\n got: %q\nwant: %q", frag, want)
	}
	if !reflect.DeepEqual(args, []interface{}{"GB1000", "GB9999"}) {
		t.Errorf("args got %v", args)
	}
}

func TestBuildModelCodeRangeWhere_TrimsWhitespace(t *testing.T) {
	frag, args := BuildModelCodeRangeWhere("p.model_code", "  GB1000 ", "  ")
	want := "UPPER(p.model_code) >= UPPER(?)"
	if frag != want {
		t.Errorf("frag\n got: %q\nwant: %q", frag, want)
	}
	if !reflect.DeepEqual(args, []interface{}{"GB1000"}) {
		t.Errorf("args got %v, want [GB1000]", args)
	}
}

// === MatchModelCodeRange ===

func TestMatchModelCodeRange_BothEmpty_NoFilter(t *testing.T) {
	if !MatchModelCodeRange("anything", "", "") {
		t.Error("empty range should match anything")
	}
}

func TestMatchModelCodeRange_CaseInsensitive(t *testing.T) {
	if !MatchModelCodeRange("gb1234", "GB1000", "GB9999") {
		t.Error("lowercase should be treated equal to upper")
	}
	if !MatchModelCodeRange("GB1234", "gb1000", "gb9999") {
		t.Error("range bounds should also be case-insensitive")
	}
}

func TestMatchModelCodeRange_BoundaryInclusive(t *testing.T) {
	if !MatchModelCodeRange("GB1000", "GB1000", "GB9999") {
		t.Error("from bound should be inclusive")
	}
	if !MatchModelCodeRange("GB9999", "GB1000", "GB9999") {
		t.Error("to bound should be inclusive")
	}
}

// 此測試保護「移除 zzz sentinel」的修法:
// 若 to=="" 仍走「to:='zzz'」路徑,UPPER(col) <= 'ZZZ' 會把 ZZZA 排除。
func TestMatchModelCodeRange_OnlyFrom_DoesNotExcludeBeyondZZZ(t *testing.T) {
	if !MatchModelCodeRange("ZZZA", "GB", "") {
		t.Error("ZZZA must be included when only from is set (open upper bound)")
	}
	if !MatchModelCodeRange("Z9999", "GB", "") {
		t.Error("Z9999 must be included when only from is set")
	}
}

func TestMatchModelCodeRange_OnlyTo(t *testing.T) {
	if !MatchModelCodeRange("AAA", "", "GB9999") {
		t.Error("anything before to should match")
	}
	if MatchModelCodeRange("HZ0001", "", "GB9999") {
		t.Error("after to should be excluded")
	}
}

func TestMatchModelCodeRange_OutsideRange(t *testing.T) {
	if MatchModelCodeRange("AA0001", "GB1000", "GB9999") {
		t.Error("before from should be excluded")
	}
	if MatchModelCodeRange("HZ0001", "GB1000", "GB9999") {
		t.Error("after to should be excluded")
	}
}

// === ModelCodeNaturalLess ===

// 自然排序:相同前綴內,數字段須以數值大小比較,不是字典序
func TestModelCodeNaturalLess_NumericSegments(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"GB8030-01", "GB8030-15", true},   // 1 < 15
		{"GB8030-15", "GB8030-49", true},   // 15 < 49
		{"GB8030-9", "GB8030-10", true},    // 9 < 10 (字典序會反過來)
		{"GB8019-15", "GB8030-01", true},   // 8019 < 8030
		{"GB8030-01", "GB8210-01", true},   // 8030 < 8210
		{"GB8210-01", "N1843W-15", true},   // 字母 G < N
		{"GB8030-01", "GB8030-01", false},  // 相等
	}
	for _, c := range cases {
		if got := ModelCodeNaturalLess(c.a, c.b); got != c.want {
			t.Errorf("Less(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestModelCodeNaturalLess_SortIntegration(t *testing.T) {
	codes := []string{
		"N1843W-15",
		"GB8030-01",
		"GB8019-15",
		"GB8030-15",
		"GB8210-01",
		"GB8030-9",
		"GB8030-49",
	}
	// 9 與 01 純數值比較:9 > 1,所以 GB8030-9 排在 GB8030-01 之後;
	// 9 < 15,所以排在 GB8030-15 之前。
	want := []string{
		"GB8019-15",
		"GB8030-01",
		"GB8030-9",
		"GB8030-15",
		"GB8030-49",
		"GB8210-01",
		"N1843W-15",
	}
	sort.SliceStable(codes, func(i, j int) bool {
		return ModelCodeNaturalLess(codes[i], codes[j])
	})
	if !reflect.DeepEqual(codes, want) {
		t.Errorf("sort order:\n got: %v\nwant: %v", codes, want)
	}
}

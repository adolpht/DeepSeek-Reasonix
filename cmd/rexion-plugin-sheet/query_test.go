package main

import "testing"

// TestParseSelectItems covers field/aggregate/star parsing.
func TestParseSelectItems(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []selectItem
	}{
		{"star", []string{"*"}, []selectItem{{raw: "*", kind: "star"}}},
		{"plain field", []string{"region"}, []selectItem{{raw: "region", kind: "field", field: "region", alias: "region"}}},
		{"field with alias", []string{"region AS r"}, []selectItem{{kind: "field", field: "region", alias: "r"}}},
		{
			"aggregate",
			[]string{"SUM(amount) as total"},
			[]selectItem{{kind: "agg", fn: "SUM", field: "amount", alias: "total"}},
		},
		{
			"aggregate no alias",
			[]string{"COUNT(*)"},
			[]selectItem{{kind: "agg", fn: "COUNT", field: "*", alias: "*"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSelectItems(c.in)
			if err != nil {
				t.Fatalf("parseSelectItems: %v", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("len = %d, want %d", len(got), len(c.want))
			}
			for i := range got {
				if got[i].kind != c.want[i].kind || got[i].field != c.want[i].field ||
					got[i].fn != c.want[i].fn || got[i].alias != c.want[i].alias {
					t.Errorf("item %d = %+v, want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestParseWhere covers comparison, AND/OR, parentheses, and IN.
func TestParseWhere(t *testing.T) {
	row := map[string]string{"region": "华东", "amount": "1500", "status": "active"}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"eq true", "region='华东'", true},
		{"eq false", "region='华北'", false},
		{"ne", "region!='华北'", true},
		{"gt", "amount>1000", true},
		{"gte eq", "amount>=1500", true},
		{"lt", "amount<1000", false},
		{"and true", "region='华东' AND amount>1000", true},
		{"and false", "region='华东' AND amount<1000", false},
		{"or true", "region='华北' OR amount>1000", true},
		{"or false", "region='华北' OR amount<1000", false},
		{"parens", "(region='华北' OR amount>1000) AND status='active'", true},
		{"in true", "region IN ('华东','华北')", true},
		{"in false", "region IN ('华南','华北')", false},
		{"double quotes", "region=\"华东\"", true},
		{"numeric compare", "amount=1500", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := parseWhere(c.expr)
			if err != nil {
				t.Fatalf("parseWhere(%q): %v", c.expr, err)
			}
			if got := expr.match(row); got != c.want {
				t.Errorf("parseWhere(%q).match = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}

// TestParseWhereErrors ensures malformed input is rejected, not silently accepted.
func TestParseWhereErrors(t *testing.T) {
	bad := []string{
		"",                // empty
		"region",          // no operator
		"region=",         // no value
		"region='华东' AND", // dangling AND
		"region IN 'a'",   // IN without paren
		"region IN (",     // unclosed IN
		"(region='华东'",    // unclosed paren
	}
	for _, b := range bad {
		if _, err := parseWhere(b); err == nil {
			t.Errorf("parseWhere(%q) expected error, got nil", b)
		}
	}
}

// TestAggregateRows verifies SUM/COUNT/AVG/MIN/MAX over groups.
func TestAggregateRows(t *testing.T) {
	records := []map[string]string{
		{"region": "A", "amt": "100"},
		{"region": "A", "amt": "200"},
		{"region": "B", "amt": "300"},
	}
	header, rows := aggregateRows(records, []selectItem{
		{kind: "agg", fn: "SUM", field: "amt", alias: "sum_amt"},
		{kind: "agg", fn: "COUNT", field: "*", alias: "cnt"},
		{kind: "agg", fn: "AVG", field: "amt", alias: "avg_amt"},
		{kind: "agg", fn: "MIN", field: "amt", alias: "min_amt"},
		{kind: "agg", fn: "MAX", field: "amt", alias: "max_amt"},
	}, []string{"region"})

	if len(header) != 6 { // 1 group_by + 5 aggs
		t.Fatalf("header len = %d, want 6: %v", len(header), header)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
	// Group A: sum=300, count=2, avg=150, min=100, max=200
	a := rows[0]
	if a[1] != "300" || a[2] != "2" || a[3] != "150" || a[4] != "100" || a[5] != "200" {
		t.Errorf("group A aggregates wrong: %v", a)
	}
}

// TestFormatNumber checks int vs float rendering.
func TestFormatNumber(t *testing.T) {
	if got := formatNumber(42.0); got != "42" {
		t.Errorf("formatNumber(42) = %q", got)
	}
	if got := formatNumber(3.14); got != "3.14" {
		t.Errorf("formatNumber(3.14) = %q", got)
	}
}

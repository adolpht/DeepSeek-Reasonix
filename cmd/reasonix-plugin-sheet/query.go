package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// querySheetTool runs a SQL-like query against a sheet without loading the full
// table into the model context. Only the aggregated/filtered result is returned.
//
// Supported subset:
//
//	select: ["field", ...] or ["SUM(amount) as total", "region", ...] or ["*"]
//	where:  field op value [AND|OR field op value] with parentheses and IN(...)
//	        ops: = != > >= < <=
//	group_by: ["field", ...]
//	order_by: "field" or "field DESC" or "field ASC"
//	limit:  int
//
// Aggregates: SUM COUNT AVG MIN MAX (only meaningful with group_by).
var querySheetTool = toolDef{
	name: "query_sheet",
	description: "Run a SQL-like query against an xlsx/csv without loading the full table into context. " +
		"Supports WHERE, GROUP BY, ORDER BY, LIMIT and aggregates SUM/COUNT/AVG/MIN/MAX. " +
		"Returns a Markdown table of the result set.",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "description": "Absolute path to the file"},
			"sheet":    map[string]any{"type": "string", "description": "Sheet name (xlsx only)"},
			"select":   map[string]any{"type": "array", "description": "Fields or aggregates, e.g. [\"region\",\"SUM(amount) as total\"] or [\"*\"]"},
			"where":    map[string]any{"type": "string", "description": "Filter expression, e.g. \"region='华东' AND amount>1000\""},
			"group_by": map[string]any{"type": "array", "description": "Group fields, e.g. [\"region\"]"},
			"order_by": map[string]any{"type": "string", "description": "Sort spec, e.g. \"total DESC\""},
			"limit":    map[string]any{"type": "integer", "description": "Row cap on the result (default 100)"},
		},
		"required": []string{"path", "select"},
	},
	run: runQuerySheet,
}

// selectItem is one projected column: either a raw field or an aggregate.
type selectItem struct {
	raw   string // original spec text, for output header
	kind  string // "field" | "agg"
	field string // source field (for "field") or aggregate arg (for "agg")
	fn    string // SUM/COUNT/AVG/MIN/MAX
	alias string // output column name
}

func runQuerySheet(args map[string]any) (any, error) {
	path, err := argString(args, "path")
	if err != nil {
		return nil, err
	}
	sheet := argStringDefault(args, "sheet", "")

	selSpec, err := toStringSlice(args["select"])
	if err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}
	if len(selSpec) == 0 {
		return nil, fmt.Errorf("select must be non-empty")
	}
	selects, err := parseSelectItems(selSpec)
	if err != nil {
		return nil, err
	}

	var where whereExpr
	if w := argStringDefault(args, "where", ""); w != "" {
		where, err = parseWhere(w)
		if err != nil {
			return nil, fmt.Errorf("where: %w", err)
		}
	}

	groupBy, err := toStringSlice(args["group_by"])
	if err != nil {
		return nil, fmt.Errorf("group_by: %w", err)
	}

	orderBy := argStringDefault(args, "order_by", "")
	limit := argIntDefault(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}

	// Read full sheet into memory (no truncation — query path must see all rows
	// to aggregate correctly). For very large sheets a future streaming impl is
	// noted in the roadmap; P1 caps memory by relying on max_rows in read_sheet.
	header, records, err := loadRecords(path, sheet)
	if err != nil {
		return nil, err
	}
	_ = header

	// Apply WHERE.
	if where != nil {
		filtered := records[:0]
		for _, r := range records {
			if where.match(r) {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}

	// Determine output.
	var outHeader []string
	var outRows [][]string

	if len(groupBy) > 0 {
		outHeader, outRows = aggregateRows(records, selects, groupBy)
	} else if hasAggregates(selects) {
		// Whole-table aggregates (no group by).
		outHeader, outRows = aggregateRows(records, selects, nil)
	} else {
		// Plain projection.
		outHeader, outRows = projectRows(records, selects)
	}

	// ORDER BY.
	if orderBy != "" {
		applyOrderBy(outHeader, outRows, orderBy)
	}

	// LIMIT.
	if len(outRows) > limit {
		outRows = outRows[:limit]
	}

	// Build Markdown.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("(matched %d rows, returned %d)\n\n", len(records), len(outRows)))
	if len(outHeader) == 0 {
		return b.String() + "(no result)\n", nil
	}
	b.WriteString("| ")
	for _, h := range outHeader {
		b.WriteString(h + " | ")
	}
	b.WriteString("\n|")
	for range outHeader {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, row := range outRows {
		b.WriteString("| ")
		for i := range outHeader {
			v := ""
			if i < len(row) {
				v = row[i]
			}
			b.WriteString(strings.ReplaceAll(v, "|", "\\|") + " | ")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func toStringSlice(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array, got %T", v)
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("array elements must be strings, got %T", x)
		}
		out = append(out, s)
	}
	return out, nil
}

func parseSelectItems(specs []string) ([]selectItem, error) {
	items := make([]selectItem, 0, len(specs))
	for _, s := range specs {
		s = strings.TrimSpace(s)
		if s == "*" {
			items = append(items, selectItem{raw: "*", kind: "star"})
			continue
		}
		// Aggregate? e.g. SUM(amount) as total
		if agg := parseAggregate(s); agg != nil {
			items = append(items, *agg)
			continue
		}
		// Plain field (with optional alias).
		field, alias := splitAlias(s)
		if field == "" {
			return nil, fmt.Errorf("invalid select item %q", s)
		}
		items = append(items, selectItem{raw: s, kind: "field", field: field, alias: alias})
	}
	return items, nil
}

func parseAggregate(s string) *selectItem {
	upper := strings.ToUpper(s)
	for _, fn := range []string{"SUM", "COUNT", "AVG", "MIN", "MAX"} {
		prefix := fn + "("
		if strings.HasPrefix(upper, prefix) && strings.Contains(s, ")") {
			open := strings.Index(s, "(")
			close := strings.Index(s, ")")
			if open < 0 || close < 0 || close < open {
				return nil
			}
			arg := strings.TrimSpace(s[open+1 : close])
			rest := strings.TrimSpace(s[close+1:])
			alias := arg
			if strings.HasPrefix(strings.ToUpper(rest), "AS ") {
				alias = strings.TrimSpace(rest[3:])
			}
			return &selectItem{raw: s, kind: "agg", fn: fn, field: arg, alias: alias}
		}
	}
	return nil
}

func splitAlias(s string) (field, alias string) {
	if i := strings.Index(strings.ToUpper(s), " AS "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+4:])
	}
	return s, s
}

func hasAggregates(items []selectItem) bool {
	for _, it := range items {
		if it.kind == "agg" {
			return true
		}
	}
	return false
}

// loadRecords reads the full sheet into []map[string]string (header from first row).
func loadRecords(path, sheet string) ([]string, []map[string]string, error) {
	rows, _, _, err := readRows(path, sheet, "", 1, math.MaxInt32)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("file has no rows")
	}
	header := rows[0]
	// Normalize header cells: trim, dedupe.
	seen := map[string]int{}
	for i, h := range header {
		h = strings.TrimSpace(h)
		if h == "" {
			h = fmt.Sprintf("col%d", i+1)
		}
		if n, dup := seen[h]; dup {
			seen[h] = n + 1
			h = fmt.Sprintf("%s_%d", h, n+1)
		} else {
			seen[h] = 1
		}
		header[i] = h
	}
	records := make([]map[string]string, 0, len(rows)-1)
	for _, r := range rows[1:] {
		m := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(r) {
				m[h] = r[i]
			}
		}
		records = append(records, m)
	}
	return header, records, nil
}

// projectRows handles plain SELECT (no aggregates).
func projectRows(records []map[string]string, items []selectItem) ([]string, [][]string) {
	// Resolve header. Star expands to all keys (use first record's order if available).
	var header []string
	var fieldOrder []string
	for _, it := range items {
		if it.kind == "star" {
			// Need a sample record to get keys; if none, header stays as-is.
			if len(records) > 0 {
				for k := range records[0] {
					fieldOrder = append(fieldOrder, k)
					header = append(header, k)
				}
			}
			continue
		}
		header = append(header, it.alias)
		fieldOrder = append(fieldOrder, it.field)
	}
	out := make([][]string, 0, len(records))
	for _, r := range records {
		row := make([]string, len(fieldOrder))
		for i, f := range fieldOrder {
			row[i] = r[f]
		}
		out = append(out, row)
	}
	return header, out
}

// aggregateRows handles SELECT with aggregates, optionally grouped.
func aggregateRows(records []map[string]string, items []selectItem, groupBy []string) ([]string, [][]string) {
	// Build groups.
	type group struct {
		key  string
		rows []map[string]string
	}
	groups := map[string]*group{}
	var order []string
	for _, r := range records {
		key := groupKey(r, groupBy)
		g, ok := groups[key]
		if !ok {
			g = &group{key: key, rows: []map[string]string{r}}
			groups[key] = g
			order = append(order, key)
		} else {
			g.rows = append(g.rows, r)
		}
	}

	// If no group_by and aggregates present, single group over all records.
	if len(groupBy) == 0 {
		order = []string{""}
		groups = map[string]*group{"": {key: "", rows: records}}
	}

	// Build header.
	var header []string
	for _, gb := range groupBy {
		header = append(header, gb)
	}
	for _, it := range items {
		if it.kind == "agg" {
			header = append(header, it.alias)
		} else if it.kind == "field" {
			// non-aggregate field in an aggregate query: take first row's value per group
			header = append(header, it.alias)
		}
	}

	out := make([][]string, 0, len(order))
	for _, k := range order {
		g := groups[k]
		row := make([]string, 0, len(header))
		// group_by values: split key back (order matters).
		keyParts := strings.Split(k, "\x00")
		for i := range groupBy {
			if i < len(keyParts) {
				row = append(row, keyParts[i])
			} else {
				row = append(row, "")
			}
		}
		// aggregates / fields
		for _, it := range items {
			if it.kind == "agg" {
				row = append(row, computeAggregate(g.rows, it))
			} else if it.kind == "field" {
				v := ""
				if len(g.rows) > 0 {
					v = g.rows[0][it.field]
				}
				row = append(row, v)
			}
		}
		out = append(out, row)
	}
	return header, out
}

func groupKey(r map[string]string, groupBy []string) string {
	parts := make([]string, len(groupBy))
	for i, f := range groupBy {
		parts[i] = r[f]
	}
	return strings.Join(parts, "\x00")
}

func computeAggregate(rows []map[string]string, it selectItem) string {
	if it.fn == "COUNT" {
		if it.field == "*" {
			return strconv.Itoa(len(rows))
		}
		n := 0
		for _, r := range rows {
			if r[it.field] != "" {
				n++
			}
		}
		return strconv.Itoa(n)
	}
	// numeric aggregates
	var vals []float64
	for _, r := range rows {
		v := strings.TrimSpace(r[it.field])
		if v == "" {
			continue
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			continue
		}
		vals = append(vals, f)
	}
	if len(vals) == 0 {
		return ""
	}
	var result float64
	switch it.fn {
	case "SUM":
		for _, v := range vals {
			result += v
		}
	case "AVG":
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		result = sum / float64(len(vals))
	case "MIN":
		result = vals[0]
		for _, v := range vals {
			if v < result {
				result = v
			}
		}
	case "MAX":
		result = vals[0]
		for _, v := range vals {
			if v > result {
				result = v
			}
		}
	default:
		return ""
	}
	return formatNumber(result)
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func applyOrderBy(header []string, rows [][]string, spec string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return
	}
	desc := false
	if strings.HasSuffix(strings.ToUpper(spec), " DESC") {
		desc = true
		spec = strings.TrimSpace(spec[:len(spec)-5])
	} else if strings.HasSuffix(strings.ToUpper(spec), " ASC") {
		spec = strings.TrimSpace(spec[:len(spec)-4])
	}
	col := -1
	for i, h := range header {
		if h == spec {
			col = i
			break
		}
	}
	if col < 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i][col], rows[j][col]
		af, ae := strconv.ParseFloat(a, 64)
		bf, be := strconv.ParseFloat(b, 64)
		if ae == nil && be == nil {
			if desc {
				return af > bf
			}
			return af < bf
		}
		if desc {
			return a > b
		}
		return a < b
	})
}

// --- where expression parser (recursive descent) ---
//
// Grammar:
//   expr   := orExpr
//   orExpr := andExpr (OR andExpr)*
//   andExpr:= term (AND term)*
//   term   := '(' expr ')' | comparison | inList
//   comparison := IDENT OP value
//   inList := IDENT IN '(' value (',' value)* ')'
//   value  := STRING | NUMBER

type whereExpr interface {
	match(row map[string]string) bool
}

type andNode struct{ l, r whereExpr }
type orNode struct{ l, r whereExpr }

func (a andNode) match(r map[string]string) bool { return a.l.match(r) && a.r.match(r) }
func (o orNode) match(r map[string]string) bool  { return o.l.match(r) || o.r.match(r) }

type cmpNode struct {
	field string
	op    string
	value string
}

func (c cmpNode) match(row map[string]string) bool {
	cell := row[c.field]
	switch c.op {
	case "=":
		return cell == c.value
	case "!=":
		return cell != c.value
	case ">":
		return numCompare(cell, c.value) > 0
	case ">=":
		return numCompare(cell, c.value) >= 0
	case "<":
		return numCompare(cell, c.value) < 0
	case "<=":
		return numCompare(cell, c.value) <= 0
	}
	return false
}

type inNode struct {
	field string
	vals  []string
}

func (n inNode) match(row map[string]string) bool {
	cell := row[n.field]
	for _, v := range n.vals {
		if cell == v {
			return true
		}
	}
	return false
}

func numCompare(a, b string) int {
	af, ae := strconv.ParseFloat(strings.TrimSpace(a), 64)
	bf, be := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if ae == nil && be == nil {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	// fall back to string compare
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

type whereLexer struct {
	s   string
	pos int
}

type whereToken struct {
	kind string // "ident" "string" "number" "op" "lparen" "rparen" "comma" "eof"
	val  string
}

func parseWhere(s string) (whereExpr, error) {
	lx := &whereLexer{s: s}
	p := &whereParser{lx: lx}
	expr, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	tok := p.next()
	if tok.kind != "eof" {
		return nil, fmt.Errorf("unexpected token %q after expression", tok.val)
	}
	return expr, nil
}

func (l *whereLexer) next() whereToken {
	for l.pos < len(l.s) && (l.s[l.pos] == ' ' || l.s[l.pos] == '\t') {
		l.pos++
	}
	if l.pos >= len(l.s) {
		return whereToken{kind: "eof"}
	}
	c := l.s[l.pos]
	switch c {
	case '(':
		l.pos++
		return whereToken{kind: "lparen", val: "("}
	case ')':
		l.pos++
		return whereToken{kind: "rparen", val: ")"}
	case ',':
		l.pos++
		return whereToken{kind: "comma", val: ","}
	case '\'':
		l.pos++
		start := l.pos
		for l.pos < len(l.s) && l.s[l.pos] != '\'' {
			l.pos++
		}
		val := l.s[start:l.pos]
		if l.pos < len(l.s) {
			l.pos++ // closing quote
		}
		return whereToken{kind: "string", val: val}
	case '"':
		l.pos++
		start := l.pos
		for l.pos < len(l.s) && l.s[l.pos] != '"' {
			l.pos++
		}
		val := l.s[start:l.pos]
		if l.pos < len(l.s) {
			l.pos++
		}
		return whereToken{kind: "string", val: val}
	case '=', '!':
		if l.pos+1 < len(l.s) && l.s[l.pos+1] == '=' {
			op := l.s[l.pos : l.pos+2]
			l.pos += 2
			return whereToken{kind: "op", val: op}
		}
		l.pos++
		return whereToken{kind: "op", val: string(c)}
	case '>', '<':
		if l.pos+1 < len(l.s) && l.s[l.pos+1] == '=' {
			op := l.s[l.pos : l.pos+2]
			l.pos += 2
			return whereToken{kind: "op", val: op}
		}
		l.pos++
		return whereToken{kind: "op", val: string(c)}
	}
	// identifier / number / keyword
	start := l.pos
	for l.pos < len(l.s) {
		ch := l.s[l.pos]
		if ch == ' ' || ch == '\t' || ch == '(' || ch == ')' || ch == ',' || ch == '=' || ch == '!' || ch == '>' || ch == '<' || ch == '\'' || ch == '"' {
			break
		}
		l.pos++
	}
	val := l.s[start:l.pos]
	if _, err := strconv.ParseFloat(val, 64); err == nil {
		return whereToken{kind: "number", val: val}
	}
	return whereToken{kind: "ident", val: val}
}

type whereParser struct {
	lx   *whereLexer
	peek whereToken
	has  bool
}

func (p *whereParser) next() whereToken {
	if p.has {
		t := p.peek
		p.has = false
		return t
	}
	return p.lx.next()
}

func (p *whereParser) peekTok() whereToken {
	if !p.has {
		p.peek = p.lx.next()
		p.has = true
	}
	return p.peek
}

func (p *whereParser) parseOr() (whereExpr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peekTok()
		if t.kind == "ident" && strings.EqualFold(t.val, "OR") {
			p.next()
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			left = orNode{l: left, r: right}
			continue
		}
		break
	}
	return left, nil
}

func (p *whereParser) parseAnd() (whereExpr, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peekTok()
		if t.kind == "ident" && strings.EqualFold(t.val, "AND") {
			p.next()
			right, err := p.parseTerm()
			if err != nil {
				return nil, err
			}
			left = andNode{l: left, r: right}
			continue
		}
		break
	}
	return left, nil
}

func (p *whereParser) parseTerm() (whereExpr, error) {
	t := p.peekTok()
	if t.kind == "lparen" {
		p.next()
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if nt := p.next(); nt.kind != "rparen" {
			return nil, fmt.Errorf("expected ')', got %q", nt.val)
		}
		return e, nil
	}
	if t.kind != "ident" {
		return nil, fmt.Errorf("expected field name, got %q", t.val)
	}
	p.next()
	field := t.val
	nt := p.peekTok()
	// IN (...)
	if nt.kind == "ident" && strings.EqualFold(nt.val, "IN") {
		p.next()
		if lt := p.next(); lt.kind != "lparen" {
			return nil, fmt.Errorf("expected '(' after IN, got %q", lt.val)
		}
		var vals []string
		for {
			vt := p.next()
			if vt.kind != "string" && vt.kind != "number" {
				return nil, fmt.Errorf("expected value in IN list, got %q", vt.val)
			}
			vals = append(vals, vt.val)
			ct := p.next()
			if ct.kind == "rparen" {
				break
			}
			if ct.kind != "comma" {
				return nil, fmt.Errorf("expected ',' or ')' in IN list, got %q", ct.val)
			}
		}
		return inNode{field: field, vals: vals}, nil
	}
	// comparison
	if nt.kind != "op" {
		return nil, fmt.Errorf("expected operator after %q, got %q", field, nt.val)
	}
	p.next()
	vt := p.next()
	if vt.kind != "string" && vt.kind != "number" {
		return nil, fmt.Errorf("expected value, got %q", vt.val)
	}
	return cmpNode{field: field, op: nt.val, value: vt.val}, nil
}

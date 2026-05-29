// Package convention computes per-package AST statistics that describe
// the package's prevailing idioms — error handling style, logging
// library, naming patterns, concurrency primitives. CKV exposes these
// raw stats so the agent (or a SKILL) can read what conventions a
// package follows before proposing edits.
//
// CKV deliberately does no interpretation here: stats are numbers and
// counts. Summarization belongs to the agent's SKILL, where the LLM
// can phrase them in context-appropriate prose.
package convention

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// Stats is the per-package statistic bundle. The map shape uses
// `any` so adding a new metric in v2 does not break consumers (the
// agent's SKILL filters keys it understands and ignores the rest).
type Stats struct {
	// Errors counts the error-construction style:
	//   "fmt.Errorf_wrap"   — fmt.Errorf with a %w verb
	//   "fmt.Errorf_plain"  — fmt.Errorf without %w
	//   "errors.New"        — errors.New
	//   "pkg/errors.Wrap"   — github.com/pkg/errors.Wrap (legacy)
	Errors map[string]int

	// Loggers counts well-known logger call sites:
	//   "log15", "zap", "slog", "stdlib_log"
	Loggers map[string]int

	// Receivers counts struct receiver short-name length distribution.
	// Keys are the short names ("s", "srv", "store", ...).
	Receivers map[string]int

	// NewConstructors counts the number of top-level `func New*` and
	// `func MustNew*` constructors (idiom for builder vs zero-value).
	NewConstructors int

	// TestFiles counts *_test.go files in the package.
	TestFiles int

	// TableDriven counts `for _, tc := range cases` / `for _, tt`
	// patterns — the conventional table-driven test idiom.
	TableDriven int

	// TestifyUses counts imports of stretchr/testify.
	TestifyUses int

	// Mutexes counts sync.Mutex / sync.RWMutex declarations.
	Mutexes int

	// Channels counts make(chan ...) call sites.
	Channels int

	// ErrGroupUses counts uses of golang.org/x/sync/errgroup.
	ErrGroupUses int

	// FileCount is the total number of files contributing to these
	// stats; useful for normalizing other counters.
	FileCount int
}

// ToMap returns Stats as a generic map. Suitable for JSON round-trip
// and for the ConventionStats column on the chunk.
func (s Stats) ToMap() map[string]any {
	out := map[string]any{
		"new_constructors": s.NewConstructors,
		"test_files":       s.TestFiles,
		"table_driven":     s.TableDriven,
		"testify_uses":     s.TestifyUses,
		"mutexes":          s.Mutexes,
		"channels":         s.Channels,
		"errgroup_uses":    s.ErrGroupUses,
		"file_count":       s.FileCount,
	}
	if len(s.Errors) > 0 {
		out["errors"] = stringIntMap(s.Errors)
	}
	if len(s.Loggers) > 0 {
		out["loggers"] = stringIntMap(s.Loggers)
	}
	if len(s.Receivers) > 0 {
		out["receivers"] = stringIntMap(s.Receivers)
	}
	return out
}

// Summary is a short, deterministic prose summary suitable as embed
// text for a ChunkConvention chunk. Deterministic order matters
// because chunk_id is content-hashed.
func (s Stats) Summary(pkg string) string {
	var b strings.Builder
	b.WriteString("package: ")
	b.WriteString(pkg)
	b.WriteString(". conventions summary.\n")
	if total := sumValues(s.Errors); total > 0 {
		writeTopShare(&b, "errors", s.Errors, total)
	}
	if total := sumValues(s.Loggers); total > 0 {
		writeTopShare(&b, "logger", s.Loggers, total)
	}
	if s.NewConstructors > 0 {
		b.WriteString("constructors: ")
		b.WriteString(itoa(s.NewConstructors))
		b.WriteString(" New*\n")
	}
	if s.Mutexes > 0 || s.Channels > 0 || s.ErrGroupUses > 0 {
		b.WriteString("concurrency: mutex=")
		b.WriteString(itoa(s.Mutexes))
		b.WriteString(" chan=")
		b.WriteString(itoa(s.Channels))
		b.WriteString(" errgroup=")
		b.WriteString(itoa(s.ErrGroupUses))
		b.WriteString("\n")
	}
	if s.TestFiles > 0 {
		b.WriteString("tests: ")
		b.WriteString(itoa(s.TestFiles))
		b.WriteString(" files, table_driven=")
		b.WriteString(itoa(s.TableDriven))
		b.WriteString(", testify=")
		b.WriteString(itoa(s.TestifyUses))
		b.WriteString("\n")
	}
	return b.String()
}

// Aggregator accumulates per-file stats then resolves into a per-package map.
type Aggregator struct {
	byPackage map[string]*Stats
}

// NewAggregator creates an empty accumulator. Safe to use without
// initialization via the zero value as long as ObservePackage is called
// before Result.
func NewAggregator() *Aggregator {
	return &Aggregator{byPackage: map[string]*Stats{}}
}

// ObserveFile parses src and folds its stats into the aggregator. file
// is the repo-relative path (used to derive the package key as
// filepath.Dir and to detect *_test.go).
func (a *Aggregator) ObserveFile(file string, src []byte) error {
	pkg := packageKey(file)
	st := a.byPackage[pkg]
	if st == nil {
		st = &Stats{
			Errors:    map[string]int{},
			Loggers:   map[string]int{},
			Receivers: map[string]int{},
		}
		a.byPackage[pkg] = st
	}
	st.FileCount++
	if strings.HasSuffix(file, "_test.go") {
		st.TestFiles++
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return err
	}

	// Imports: testify / errgroup detection.
	for _, imp := range f.Imports {
		v := unquote(imp.Path.Value)
		switch {
		case strings.Contains(v, "stretchr/testify"):
			st.TestifyUses++
		case strings.Contains(v, "golang.org/x/sync/errgroup"):
			st.ErrGroupUses++
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Recv == nil && strings.HasPrefix(x.Name.Name, "New") {
				st.NewConstructors++
			}
			if x.Recv != nil && len(x.Recv.List) > 0 {
				for _, r := range x.Recv.List {
					for _, n := range r.Names {
						st.Receivers[n.Name]++
					}
				}
			}
		case *ast.RangeStmt:
			// table-driven idiom: `for _, tc := range cases`
			if v, ok := x.Value.(*ast.Ident); ok {
				name := v.Name
				if name == "tc" || name == "tt" {
					st.TableDriven++
				}
			}
		case *ast.CallExpr:
			name := callableName(x.Fun)
			switch name {
			case "fmt.Errorf":
				if hasWrapVerb(x.Args) {
					st.Errors["fmt.Errorf_wrap"]++
				} else {
					st.Errors["fmt.Errorf_plain"]++
				}
			case "errors.New":
				st.Errors["errors.New"]++
			case "errors.Wrap":
				st.Errors["pkg/errors.Wrap"]++
			case "make":
				if isChanType(x.Args) {
					st.Channels++
				}
			case "log15.New", "log15.Info", "log15.Warn", "log15.Error":
				st.Loggers["log15"]++
			case "zap.L", "zap.S", "zap.NewProduction":
				st.Loggers["zap"]++
			case "slog.Info", "slog.Warn", "slog.Error", "slog.Debug":
				st.Loggers["slog"]++
			case "log.Printf", "log.Print", "log.Println", "log.Fatal":
				st.Loggers["stdlib_log"]++
			}
		case *ast.SelectorExpr:
			// Any reference to sync.Mutex / sync.RWMutex — struct field,
			// var, parameter, embedded type. Selector-based check catches
			// all those without a separate per-node walk.
			if isMutexType(x) {
				st.Mutexes++
			}
		}
		return true
	})

	return nil
}

// Result returns the per-package stats. Keys are package paths
// (filepath.Dir of the first observed file in that package). The map
// is fresh; subsequent ObserveFile calls do not mutate it.
func (a *Aggregator) Result() map[string]Stats {
	out := make(map[string]Stats, len(a.byPackage))
	for k, v := range a.byPackage {
		out[k] = *v
	}
	return out
}

// packageKey returns the repo-relative directory that contains file.
// Empty / "." files (top-level main.go) map to "" — caller decides
// what to do.
func packageKey(file string) string {
	i := strings.LastIndex(file, "/")
	if i < 0 {
		return ""
	}
	return file[:i]
}

func callableName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok {
			return pkg.Name + "." + x.Sel.Name
		}
		return x.Sel.Name
	}
	return ""
}

func hasWrapVerb(args []ast.Expr) bool {
	if len(args) == 0 {
		return false
	}
	lit, ok := args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return strings.Contains(lit.Value, "%w")
}

func isChanType(args []ast.Expr) bool {
	if len(args) == 0 {
		return false
	}
	_, ok := args[0].(*ast.ChanType)
	return ok
}

func isMutexType(e ast.Expr) bool {
	if e == nil {
		return false
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if pkg.Name != "sync" {
		return false
	}
	return sel.Sel.Name == "Mutex" || sel.Sel.Name == "RWMutex"
}

func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '`' && s[len(s)-1] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}

func sumValues(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

func writeTopShare(b *strings.Builder, label string, m map[string]int, total int) {
	type kv struct {
		k string
		v int
	}
	rows := make([]kv, 0, len(m))
	for k, v := range m {
		rows = append(rows, kv{k, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].v != rows[j].v {
			return rows[i].v > rows[j].v
		}
		return rows[i].k < rows[j].k
	})
	b.WriteString(label)
	b.WriteString(":")
	for i, r := range rows {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(" ")
		b.WriteString(r.k)
		b.WriteString("=")
		b.WriteString(itoa(r.v))
	}
	b.WriteString(" (total=")
	b.WriteString(itoa(total))
	b.WriteString(")\n")
}

// stringIntMap is the canonical representation of a count map when
// embedded in the generic stats JSON. The wrapper exists only so
// the keys serialize in deterministic alpha order.
type stringIntMap map[string]int

// itoa is a tiny strconv.Itoa stand-in to keep the imports list
// minimal for the summary builder; performance is not the concern.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

package web

import (
	"errors"
	"net/url"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// The column chain is the workspace's whole navigation state, carried in the
// path: /tasks/{root}/w/{kind}/{ref}/{kind}/{ref}... Opening something from a
// column appends a pair on the right; a rail entry links to a prefix of the
// same path, so stepping back is truncation and every state is a real URL.
//
// A chain of depth 0 is /tasks/{root}, the board with that task's panel open
// - the state the rail ends at. There is deliberately no /tasks/{root}/w/
// form: ServeMux does not clean a trailing slash and only matches an empty
// tail with one, so a second spelling of a state /tasks/{root} already names
// would be two canonical forms. ParseChain accepts it and canonicalises;
// Path never emits it.

// ColumnKind is a column type's URL tag. It is one path segment and is
// matched against the kind registry (kinds.go), never interpreted here.
type ColumnKind string

const (
	// KindTask is a task column: its ref is a task id.
	KindTask ColumnKind = "t"
	// KindFile is a file column: its ref is a file attached to the task
	// the chain was on when the column was opened.
	KindFile ColumnKind = "f"
	// KindDesc is a task's own description in the working area. Its ref is
	// the task id, which the chain already implies - but a pair without a
	// ref is not a chain, and spelling the id out makes the URL and the
	// rail rung say which task's text is open.
	//
	// It is a kind rather than a reserved file ref because a reserved ref
	// would shadow a real file: only "{id}.md" and "*.phase" are reserved
	// for writes, so a task may legitimately have a file called
	// "description.md" (internal/task/name.go, internal/task/phase.go).
	KindDesc ColumnKind = "d"
	// KindGraph is the backlog relations graph. Its ref is the task to
	// highlight, which is why it is a real id and never a sentinel: the
	// one state with no task to highlight is the graph opened from the
	// board, and that is a route of its own rather than a chain (a chain
	// always has a root, see ParseChain).
	KindGraph ColumnKind = "g"
)

// maxChainDepth caps the columns in one chain. The cap is about cost, not URL
// length: a depth-N render takes the service lock up to N+2 times, while even
// a worst-case depth-10 URL over this repository's own backlog measures 270
// characters. Ten is the depth the subtask requires to work; the rest is
// headroom.
const maxChainDepth = 16

// Column is one entry of the chain: a kind and its reference, both decoded.
type Column struct {
	Kind ColumnKind
	Ref  string
}

// Chain is a root task plus the columns opened from it, in order. The zero
// Chain is the bare board, which renders as "/".
type Chain struct {
	Root    string
	Columns []Column
}

// ErrBadChain is every way a path fails to name a chain: a malformed pair, an
// empty or illegal segment, an unknown kind, or more columns than the cap. A
// handler turns it into one 404 - the chain is part of the URL, so a bad
// chain is a URL that names nothing, not a request that went wrong.
var ErrBadChain = errors.New("not a column chain")

// ParseChain reads a chain out of a request path.
//
// It takes the ESCAPED path (r.URL.EscapedPath()), not r.PathValue: PathValue
// hands back a decoded value, in which "%2F" has become a real separator, so
// splitting it would let one encoded slash in a file name forge a segment
// boundary. Splitting the escaped form and unescaping each segment on its own
// keeps segment identity exact.
//
// Segments are addressed by index rather than by searching for "/w/": a task
// whose id is literally "w" makes the first "/w/" the wrong one.
func ParseChain(escapedPath string) (Chain, error) {
	segs := strings.Split(escapedPath, "/")
	// "" / "tasks" / <root> [ / "w" / tail... ]
	if len(segs) < 3 || segs[0] != "" || segs[1] != "tasks" {
		return Chain{}, ErrBadChain
	}
	// /tasks/{root} is the depth-0 chain, so the model is total over every
	// URL a chain can render and the round trip closes. /tasks/{root}/panel
	// is not a chain: only "w" opens a tail.
	if len(segs) > 3 && segs[3] != "w" {
		return Chain{}, ErrBadChain
	}
	root, err := unescapeSegment(segs[2])
	if err != nil {
		return Chain{}, err
	}
	if err := task.ValidateNameSegment("task id", root); err != nil {
		return Chain{}, ErrBadChain
	}

	var tail []string
	if len(segs) > 4 {
		tail = segs[4:]
	}
	// A trailing slash leaves one empty segment: /tasks/42/w/ is the
	// depth-0 chain, which Path spells /tasks/42.
	if len(tail) == 1 && tail[0] == "" {
		tail = nil
	}
	if len(tail)%2 != 0 {
		return Chain{}, ErrBadChain
	}
	if len(tail)/2 > maxChainDepth {
		return Chain{}, ErrBadChain
	}

	chain := Chain{Root: root}
	for i := 0; i < len(tail); i += 2 {
		kind, err := unescapeSegment(tail[i])
		if err != nil {
			return Chain{}, err
		}
		if _, ok := columnKinds[ColumnKind(kind)]; !ok {
			return Chain{}, ErrBadChain
		}
		ref, err := unescapeSegment(tail[i+1])
		if err != nil {
			return Chain{}, err
		}
		// Every ref names something on disk - a task directory or a file
		// inside one - so all of them obey the one shape rule, which is
		// also what rejects a decoded separator and a dots-only name.
		if err := task.ValidateNameSegment("reference", ref); err != nil {
			return Chain{}, ErrBadChain
		}
		chain.Columns = append(chain.Columns, Column{Kind: ColumnKind(kind), Ref: ref})
	}
	return chain, nil
}

// unescapeSegment decodes one path segment. An undecodable escape is a bad
// chain, not a server error.
func unescapeSegment(seg string) (string, error) {
	s, err := url.PathUnescape(seg)
	if err != nil {
		return "", ErrBadChain
	}
	return s, nil
}

// Path is the chain's canonical URL, with every segment percent-encoded here
// so a template emits a finished string. Depth 0 is /tasks/{root}, and an
// empty root is the bare board.
func (c Chain) Path() string {
	if c.Root == "" {
		return "/"
	}
	var b strings.Builder
	b.WriteString("/tasks/")
	b.WriteString(url.PathEscape(c.Root))
	if len(c.Columns) > 0 {
		b.WriteString("/w")
		for _, col := range c.Columns {
			b.WriteString("/")
			b.WriteString(url.PathEscape(string(col.Kind)))
			b.WriteString("/")
			b.WriteString(url.PathEscape(col.Ref))
		}
	}
	return b.String()
}

// Fragment is the URL of the same state as an htmx fragment. Page and
// fragment routes are paired explicitly in this package (/board to /{$},
// /tasks/{id}/panel to /tasks/{id}), and "/strip" + Path continues that.
func (c Chain) Fragment() string {
	return stripPrefix + c.Path()
}

// stripPrefix is the fragment route's prefix. A chain's page path appended to
// it is the fragment path, and stripping it is how the fragment handler gets
// back to the page path it must parse.
const stripPrefix = "/strip"

// Append returns the chain with one more column on the right. It copies, so a
// chain handed to a template is never mutated behind it.
func (c Chain) Append(kind ColumnKind, ref string) Chain {
	cols := make([]Column, len(c.Columns), len(c.Columns)+1)
	copy(cols, c.Columns)
	return Chain{Root: c.Root, Columns: append(cols, Column{Kind: kind, Ref: ref})}
}

// TruncateTo returns the chain with its first n columns, which is what a rail
// entry links to: n == 0 is the board with the root's panel open.
func (c Chain) TruncateTo(n int) Chain {
	if n < 0 {
		n = 0
	}
	if n > len(c.Columns) {
		n = len(c.Columns)
	}
	cols := make([]Column, n)
	copy(cols, c.Columns[:n])
	return Chain{Root: c.Root, Columns: cols}
}

// TaskAt is the task column i belongs to: the nearest task column before it,
// else the root. It is how a file column knows whose file its ref names.
func (c Chain) TaskAt(i int) string {
	for j := i - 1; j >= 0; j-- {
		if c.Columns[j].Kind == KindTask {
			return c.Columns[j].Ref
		}
	}
	return c.Root
}

// Depth is the number of columns past the board.
func (c Chain) Depth() int { return len(c.Columns) }

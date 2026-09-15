package store

import (
	"strconv"
	"strings"
	"time"
)

// QueryNode is a node in the boolean AST of a parsed change query. Each node
// compiles to a SQL WHERE fragment plus its bound arguments. The shared query
// runs over `changes ch JOIN accounts a ON a.id = ch.owner_id`.
type QueryNode interface {
	compile() (string, []any)
}

type leafNode struct {
	sql  string
	args []any
}

func (n leafNode) compile() (string, []any) { return "(" + n.sql + ")", n.args }

func leaf(sql string, args ...any) QueryNode { return leafNode{sql: sql, args: args} }

type andNode []QueryNode

func (n andNode) compile() (string, []any) {
	if len(n) == 0 {
		return "1=1", nil
	}
	parts := make([]string, 0, len(n))
	args := []any{}
	for _, c := range n {
		s, a := c.compile()
		parts = append(parts, s)
		args = append(args, a...)
	}
	return "(" + strings.Join(parts, " AND ") + ")", args
}

type orNode []QueryNode

func (n orNode) compile() (string, []any) {
	if len(n) == 0 {
		return "1=0", nil
	}
	parts := make([]string, 0, len(n))
	args := []any{}
	for _, c := range n {
		s, a := c.compile()
		parts = append(parts, s)
		args = append(args, a...)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

type notNode struct{ child QueryNode }

func (n notNode) compile() (string, []any) {
	s, a := n.child.compile()
	return "NOT (" + s + ")", a
}

// ---------- tokenizer ----------

type tokKind int

const (
	tokTerm tokKind = iota
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
)

type token struct {
	kind    tokKind
	text    string // tokTerm: key:value or free text, quotes stripped
	negated bool   // tokTerm: leading '-'
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func tokenize(q string) []token {
	var toks []token
	i, n := 0, len(q)
	for i < n {
		c := q[i]
		switch {
		case isSpace(c):
			i++
		case c == '(':
			toks = append(toks, token{kind: tokLParen})
			i++
		case c == ')':
			toks = append(toks, token{kind: tokRParen})
			i++
		default:
			negated := false
			if c == '-' {
				negated = true
				i++
			}
			var sb strings.Builder
			quoted := false
			for i < n {
				ch := q[i]
				if ch == '"' {
					quoted = true
					i++
					for i < n && q[i] != '"' {
						sb.WriteByte(q[i])
						i++
					}
					if i < n {
						i++ // closing quote
					}
					continue
				}
				if isSpace(ch) || ch == '(' || ch == ')' {
					break
				}
				sb.WriteByte(ch)
				i++
			}
			text := sb.String()
			if text == "" {
				continue
			}
			if !quoted && !negated {
				switch strings.ToUpper(text) {
				case "AND":
					toks = append(toks, token{kind: tokAnd})
					continue
				case "OR":
					toks = append(toks, token{kind: tokOr})
					continue
				case "NOT":
					toks = append(toks, token{kind: tokNot})
					continue
				}
			}
			toks = append(toks, token{kind: tokTerm, text: text, negated: negated})
		}
	}
	return toks
}

// ---------- parser ----------

type parser struct {
	toks []token
	pos  int
	self *Account
}

func (p *parser) peek() (token, bool) {
	if p.pos < len(p.toks) {
		return p.toks[p.pos], true
	}
	return token{}, false
}

func (p *parser) next() (token, bool) {
	t, ok := p.peek()
	if ok {
		p.pos++
	}
	return t, ok
}

// ParseQuery parses a Gerrit-style boolean query string into a QueryNode.
// self (may be nil) resolves owner:self / reviewer:self / is:starred / is:watched.
// An empty query returns a node that matches everything.
func ParseQuery(q string, self *Account) QueryNode {
	p := &parser{toks: tokenize(q), self: self}
	node := p.parseOr()
	if node == nil {
		return andNode{}
	}
	return node
}

func (p *parser) parseOr() QueryNode {
	left := p.parseAnd()
	if left == nil {
		return nil
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tokOr {
			return left
		}
		p.next()
		right := p.parseAnd()
		if right == nil {
			return left
		}
		if or, isOr := left.(orNode); isOr {
			left = append(or, right)
		} else {
			left = orNode{left, right}
		}
	}
}

func (p *parser) parseAnd() QueryNode {
	left := p.parseNot()
	if left == nil {
		return nil
	}
	for {
		t, ok := p.peek()
		if !ok {
			return left
		}
		switch t.kind {
		case tokAnd:
			p.next()
			right := p.parseNot()
			if right == nil {
				return left
			}
			left = andAppend(left, right)
		case tokTerm, tokLParen, tokNot:
			// implicit AND between adjacent operands
			right := p.parseNot()
			if right == nil {
				return left
			}
			left = andAppend(left, right)
		default:
			return left
		}
	}
}

func andAppend(left, right QueryNode) QueryNode {
	if and, isAnd := left.(andNode); isAnd {
		return append(and, right)
	}
	return andNode{left, right}
}

func (p *parser) parseNot() QueryNode {
	t, ok := p.peek()
	if !ok {
		return nil
	}
	if t.kind == tokNot {
		p.next()
		child := p.parseNot()
		if child == nil {
			return nil
		}
		return notNode{child: child}
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() QueryNode {
	t, ok := p.next()
	if !ok {
		return nil
	}
	switch t.kind {
	case tokLParen:
		inner := p.parseOr()
		if inner == nil {
			inner = andNode{}
		}
		if close, ok := p.peek(); ok && close.kind == tokRParen {
			p.next()
		}
		return inner
	case tokTerm:
		node := condFor(t.text, p.self)
		if t.negated {
			return notNode{child: node}
		}
		return node
	default:
		return nil
	}
}

// ---------- operators ----------

func condFor(term string, self *Account) QueryNode {
	key, val, hasColon := strings.Cut(term, ":")
	if !hasColon {
		return condText(term)
	}
	var selfName string
	var selfID int64
	if self != nil {
		selfName = self.Username
		selfID = self.ID
	}
	resolve := func(v string) string {
		if strings.EqualFold(v, "self") && selfName != "" {
			return selfName
		}
		return v
	}
	switch strings.ToLower(key) {
	case "status":
		switch strings.ToLower(val) {
		case "open", "new", "pending":
			return leaf(`ch.status=?`, "NEW")
		case "merged":
			return leaf(`ch.status=?`, "MERGED")
		case "closed":
			return leaf(`ch.status IN (?,?)`, "MERGED", "ABANDONED")
		case "abandoned":
			return leaf(`ch.status=?`, "ABANDONED")
		}
		return leaf(`ch.status=?`, strings.ToUpper(val))
	case "project":
		return leaf(`ch.project=?`, val)
	case "branch":
		return leaf(`ch.branch=?`, val)
	case "topic":
		return leaf(`ch.topic=?`, val)
	case "owner":
		if strings.EqualFold(val, "self") && selfID > 0 {
			return leaf(`ch.owner_id=?`, selfID)
		}
		u := resolve(val)
		return leaf(`(a.username=? OR a.full_name=?)`, u, u)
	case "reviewer":
		if strings.EqualFold(val, "self") && selfID > 0 {
			return leaf(`EXISTS (SELECT 1 FROM reviewers rv WHERE rv.change_number=ch.number AND rv.account_id=?)`, selfID)
		}
		u := resolve(val)
		return leaf(`EXISTS (SELECT 1 FROM reviewers rv JOIN accounts ra ON ra.id=rv.account_id
		            WHERE rv.change_number=ch.number AND (ra.username=? OR ra.full_name=?))`, u, u)
	case "change":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			return leaf(`ch.number=?`, n)
		}
		return leaf(`ch.change_id=?`, val)
	case "is":
		switch strings.ToLower(val) {
		case "wip":
			return leaf(`ch.work_in_progress=1`)
		case "open":
			return leaf(`ch.status=?`, "NEW")
		case "merged":
			return leaf(`ch.status=?`, "MERGED")
		case "abandoned":
			return leaf(`ch.status=?`, "ABANDONED")
		case "private":
			return leaf(`ch.private=1`)
		case "starred":
			if selfID > 0 {
				return leaf(`EXISTS (SELECT 1 FROM starred st WHERE st.change_number=ch.number AND st.account_id=?)`, selfID)
			}
			return leaf(`1=0`)
		case "watched":
			if selfID > 0 {
				return leaf(`EXISTS (SELECT 1 FROM watched_projects wp WHERE wp.project=ch.project AND wp.account_id=?)`, selfID)
			}
			return leaf(`1=0`)
		}
		return condText(term)
	case "has":
		if strings.EqualFold(val, "vote") {
			return leaf(`EXISTS (SELECT 1 FROM votes v WHERE v.change_number=ch.number)`)
		}
		return condText(term)
	case "before", "until":
		if t := parseQueryDate(val); t != nil {
			return leaf(`ch.updated < ?`, t.UTC().Format(time.RFC3339Nano))
		}
		return condText(term)
	case "after", "since":
		if t := parseQueryDate(val); t != nil {
			return leaf(`ch.updated > ?`, t.UTC().Format(time.RFC3339Nano))
		}
		return condText(term)
	case "hashtag":
		return leaf(`EXISTS (SELECT 1 FROM change_hashtags ht WHERE ht.change_number=ch.number AND ht.hashtag=?)`,
			NormalizeHashtag(val))
	case "assignee":
		if strings.EqualFold(val, "self") && selfID > 0 {
			return leaf(`ch.assignee_id=?`, selfID)
		}
		if strings.EqualFold(val, "none") {
			return leaf(`ch.assignee_id=0`)
		}
		u := resolve(val)
		return leaf(`EXISTS (SELECT 1 FROM accounts aa WHERE aa.id=ch.assignee_id AND (aa.username=? OR aa.full_name=?))`, u, u)
	default:
		return condText(term)
	}
}

func condText(t string) QueryNode {
	like := "%" + t + "%"
	return leaf(`(ch.subject LIKE ? OR ch.project LIKE ? OR ch.change_id LIKE ? OR CAST(ch.number AS TEXT) LIKE ?)`,
		like, like, like, like)
}

func parseQueryDate(s string) *time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

// SearchChangesParsed runs a parsed boolean query and returns matching changes
// plus the total count (ignoring limit/offset) for pagination.
func (d *DB) SearchChangesParsed(root QueryNode, limit, offset int) ([]*Change, int, error) {
	whereSQL, args := root.compile()
	fromSQL := `FROM changes ch JOIN accounts a ON a.id = ch.owner_id `
	full := ""
	if whereSQL != "" {
		full = "WHERE " + whereSQL + " "
	}

	var total int
	if err := d.db.QueryRow(`SELECT COUNT(*) `+fromSQL+full, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.db.Query(`SELECT `+changeCols+` `+fromSQL+full+
		`ORDER BY ch.updated DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Change
	for rows.Next() {
		c, err := scanChange(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

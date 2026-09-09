// Package testdriver provides a dependency-free, in-memory database/sql driver
// used by unit tests to exercise GORM/sql-backed repositories offline (no
// MariaDB/Postgres required). It is a manual stub: queries are answered from a
// configurable result set, so tests stay fully offline.
package testdriver

import (
	"database/sql"
	"database/sql/driver"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Row is a single fake result row: ordered column values.
type Row struct {
	Cols []string
	Vals []driver.Value
}

// NewRow builds a fake Row from a column list and positional values.
func NewRow(cols []string, vals ...interface{}) Row {
	dv := make([]driver.Value, len(vals))
	for i, v := range vals {
		dv[i] = v
	}
	return Row{Cols: cols, Vals: dv}
}

// FakeDB is the configurable behaviour for the fake driver.
type FakeDB struct {
	mu         sync.Mutex
	DefaultRow []Row            // rows returned when no Responses match
	Responses  map[string][]Row // keyed by a substring of the (lower-cased) SQL
	ExecErr    error            // error returned by Exec
	QueryErr   error            // error returned by Query
	RowsAff    int64            // rows affected for Exec
	CountValue int64            // value returned for count(*) queries

	execCalls  int
	queryCalls int
}

// SetSelectRows sets the default SELECT rows returned when no Responses entry matches.
func (f *FakeDB) SetSelectRows(rows []Row) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DefaultRow = rows
}

// SetResponse configures rows returned when the SQL contains the given
// substring (case-insensitive). Useful because different queries project
// different column sets.
func (f *FakeDB) SetResponse(substr string, rows []Row) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Responses == nil {
		f.Responses = map[string][]Row{}
	}
	f.Responses[substr] = rows
}

// ExecCallCount returns how many Exec calls were made.
func (f *FakeDB) ExecCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.execCalls
}

// QueryCallCount returns how many Query calls were made.
func (f *FakeDB) QueryCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queryCalls
}

func (f *FakeDB) matchRows(query string) []Row {
	lower := strings.ToLower(query)
	for k, v := range f.Responses {
		if strings.Contains(lower, strings.ToLower(k)) {
			return v
		}
	}
	return f.DefaultRow
}

type conn struct{ db *FakeDB }

func (c *conn) Prepare(query string) (driver.Stmt, error) { return &stmt{c: c, q: query}, nil }
func (c *conn) Close() error                              { return nil }
func (c *conn) Begin() (driver.Tx, error)                 { return &tx{}, nil }

func (c *conn) Query(query string, args []driver.Value) (driver.Rows, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	c.db.queryCalls++
	if c.db.QueryErr != nil {
		return nil, c.db.QueryErr
	}
	q := strings.ToLower(query)
	if strings.Contains(q, "count(") {
		return &rows{cols: []string{"count(*)"}, data: [][]driver.Value{{c.db.CountValue}}}, nil
	}
	matched := c.db.matchRows(query)
	if len(matched) == 0 {
		return &rows{cols: []string{"id"}, data: [][]driver.Value{{int64(0)}}}, nil
	}
	cols := matched[0].Cols
	data := make([][]driver.Value, 0, len(matched))
	for _, r := range matched {
		data = append(data, r.Vals)
	}
	return &rows{cols: cols, data: data}, nil
}

func (c *conn) Exec(query string, args []driver.Value) (driver.Result, error) {
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	c.db.execCalls++
	if c.db.ExecErr != nil {
		return nil, c.db.ExecErr
	}
	aff := c.db.RowsAff
	if aff == 0 {
		aff = 1
	}
	return &result{rowsAff: aff}, nil
}

type stmt struct {
	c *conn
	q string
}

func (s *stmt) Close() error                                    { return nil }
func (s *stmt) NumInput() int                                   { return -1 }
func (s *stmt) Exec(args []driver.Value) (driver.Result, error) { return s.c.Exec(s.q, args) }
func (s *stmt) Query(args []driver.Value) (driver.Rows, error)  { return s.c.Query(s.q, args) }

type tx struct{}

func (t *tx) Commit() error   { return nil }
func (t *tx) Rollback() error { return nil }

type result struct{ rowsAff int64 }

func (r *result) LastInsertId() (int64, error) { return 1, nil }
func (r *result) RowsAffected() (int64, error) { return r.rowsAff, nil }

type rows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *rows) Columns() []string { return r.cols }
func (r *rows) Close() error      { return nil }
func (r *rows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

var (
	registered = map[string]*FakeDB{}
	mu         sync.Mutex
	counter    int
)

// Open registers a new fake driver instance and returns a *sql.DB connected to
// it. The returned FakeDB can be configured from the test.
func Open() (*sql.DB, *FakeDB) {
	mu.Lock()
	counter++
	name := "fakedb_" + strconv.Itoa(counter)
	db := &FakeDB{}
	registered[name] = db
	mu.Unlock()

	sql.Register(name, &driver_{db: db})
	sqldb, err := sql.Open(name, "fake")
	if err != nil {
		panic(err)
	}
	return sqldb, db
}

type driver_ struct{ db *FakeDB }

func (d *driver_) Open(name string) (driver.Conn, error) { return &conn{db: d.db}, nil }

var selectColsRe = regexp.MustCompile(`(?i)select\s+(.+?)\s+from`)

// ColumnsFromSelect returns the column list a SELECT statement projects.
func ColumnsFromSelect(query string) []string {
	m := selectColsRe.FindStringSubmatch(query)
	if m == nil {
		return nil
	}
	parts := strings.Split(m[1], ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "`")
		if p == "*" {
			continue
		}
		if i := strings.Index(p, " as "); i >= 0 {
			p = strings.TrimSpace(p[:i])
		}
		out = append(out, p)
	}
	return out
}

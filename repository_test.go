package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

var errTestWrite = errors.New("database write failed")

type workWriteTestDB struct {
	step        int
	failAt      int
	activeCount int64
	committed   bool
	rolledBack  bool
	lastQuery   string
	lastArgs    []driver.NamedValue
}

func (d *workWriteTestDB) next() error {
	d.step++
	if d.step == d.failAt {
		return errTestWrite
	}
	return nil
}

func (d *workWriteTestDB) Connect(context.Context) (driver.Conn, error) { return d, nil }
func (d *workWriteTestDB) Driver() driver.Driver                        { return d }
func (d *workWriteTestDB) Open(string) (driver.Conn, error)             { return d, nil }
func (d *workWriteTestDB) Close() error                                 { return nil }
func (d *workWriteTestDB) Begin() (driver.Tx, error)                    { return d, nil }
func (d *workWriteTestDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (d *workWriteTestDB) Commit() error {
	if err := d.next(); err != nil {
		return err
	}
	d.committed = true
	return nil
}
func (d *workWriteTestDB) Rollback() error {
	d.rolledBack = true
	return nil
}
func (d *workWriteTestDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	d.lastQuery, d.lastArgs = query, args
	if err := d.next(); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}
func (d *workWriteTestDB) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := d.next(); err != nil {
		return nil, err
	}
	value := int64(42)
	if strings.Contains(query, "COUNT(*)") {
		value = d.activeCount
	}
	return &workWriteTestRows{value: value}, nil
}

type workWriteTestRows struct {
	value int64
	done  bool
}

func (r *workWriteTestRows) Columns() []string { return []string{"value"} }
func (r *workWriteTestRows) Close() error      { return nil }
func (r *workWriteTestRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	values[0] = r.value
	r.done = true
	return nil
}

func testWriteRepo(t *testing.T, state *workWriteTestDB) RealRepo {
	t.Helper()
	db := sql.OpenDB(state)
	t.Cleanup(func() { _ = db.Close() })
	return RealRepo{DB: db}
}

func TestCreateWorkPropagatesDatabaseErrors(t *testing.T) {
	for failAt := 1; failAt <= 6; failAt++ {
		t.Run(fmt.Sprintf("step-%d", failAt), func(t *testing.T) {
			state := &workWriteTestDB{failAt: failAt}
			repo := testWriteRepo(t, state)
			err := repo.CreateWork("user-123", reportRequestBody{Date: "2026-10-29"})
			if !errors.Is(err, errTestWrite) {
				t.Fatalf("got %v, want database error", err)
			}
			if state.committed || (failAt < 6 && !state.rolledBack) {
				t.Fatalf("committed=%v, rolled back=%v", state.committed, state.rolledBack)
			}
		})
	}
}

func TestCreateWorkSavesEndTime(t *testing.T) {
	for _, end := range []string{"", "2026-10-29 13:15:00"} {
		t.Run(end, func(t *testing.T) {
			state := &workWriteTestDB{}
			repo := testWriteRepo(t, state)
			err := repo.CreateWork("user-123", reportRequestBody{
				Date:  "2026-10-29",
				Works: Works{Task_name: "昼礼", Start_time: "2026-10-29 13:00:00", End_time: end},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !state.committed || state.step != 6 || !strings.Contains(state.lastQuery, "INSERT INTO workentries") {
				t.Fatalf("work was not inserted and committed: %+v", state)
			}
			var want driver.Value
			if end != "" {
				want = end
			}
			if state.lastArgs[5].Value != want {
				t.Fatalf("saved end time %v, want %v", state.lastArgs[5].Value, want)
			}
		})
	}
}

func TestCreateWorkRejectsExistingActiveWork(t *testing.T) {
	state := &workWriteTestDB{activeCount: 1}
	repo := testWriteRepo(t, state)
	if err := repo.CreateWork("user-123", reportRequestBody{}); err == nil {
		t.Fatal("expected active work error")
	}
	if state.step != 4 || state.committed || !state.rolledBack {
		t.Fatalf("unexpected transaction state: %+v", state)
	}
}

func TestUpdateWorkPreservesNullableEndTime(t *testing.T) {
	for _, end := range []string{"", "13:15"} {
		t.Run(end, func(t *testing.T) {
			state := &workWriteTestDB{}
			repo := testWriteRepo(t, state)
			if err := repo.UpdateWork("user-123", Works{End_time: end}, "42"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(state.lastQuery, "ON w.report_id = d.report_id") {
				t.Fatal("work update must join the owning daily report")
			}
			var want driver.Value
			if end != "" {
				want = end
			}
			if state.lastArgs[4].Value != want {
				t.Fatalf("saved end time %v, want %v", state.lastArgs[4].Value, want)
			}
		})
	}
}

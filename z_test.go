// Copyright 2017 Tamás Gulácsi
//
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package goracle_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	goracle "gopkg.in/goracle.v2"
)

var testDb *sql.DB

var testLoggersMu sync.RWMutex
var testLoggers []*testing.T

func init() {
	var err error
	if testDb, err = sql.Open(
		"goracle",
		os.Getenv("GORACLE_DRV_TEST_USERNAME")+"/"+
			os.Getenv("GORACLE_DRV_TEST_PASSWORD")+"@"+
			os.Getenv("GORACLE_DRV_TEST_DB"),
	); err != nil {
		fmt.Println("ERROR")
		panic(err)
	}

	goracle.Log = func(keyvals ...interface{}) error {
		buf := bufPool.Get().(*bytes.Buffer)
		defer bufPool.Put(buf)
		buf.Reset()
		if len(keyvals)%2 != 0 {
			keyvals = append(append(make([]interface{}, 0, len(keyvals)+1), "msg"), keyvals...)
		}
		for i := 0; i < len(keyvals); i += 2 {
			fmt.Fprintf(buf, "%s=%#v ", keyvals[i], keyvals[i+1])
		}
		testLoggersMu.RLock()
		defer testLoggersMu.RUnlock()
		for _, f := range testLoggers {
			f.Log(buf.String())
		}
		return nil
	}
}

var bufPool = sync.Pool{New: func() interface{} { return bytes.NewBuffer(make([]byte, 0, 1024)) }}

func enableLogging(t *testing.T) func() {
	testLoggersMu.Lock()
	testLoggers = append(testLoggers, t)
	testLoggersMu.Unlock()
	return func() {
		testLoggersMu.Lock()
		defer testLoggersMu.Unlock()
		for i, f := range testLoggers {
			if f == t {
				testLoggers[i] = testLoggers[0]
				testLoggers = testLoggers[1:]
				break
			}
		}
	}
}

func TestDbmsOutput(t *testing.T) {
	defer enableLogging(t)()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := goracle.EnableDbmsOutput(ctx, testDb); err != nil {
		t.Fatal(err)
	}

	txt := `árvíztűrő tükörfúrógép`
	qry := "BEGIN DBMS_OUTPUT.PUT_LINE('" + txt + "'); END;"
	if _, err := testDb.ExecContext(ctx, qry); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := goracle.ReadDbmsOutput(ctx, &buf, testDb); err != nil {
		t.Error(err)
	}
	t.Log(buf.String())
	if buf.String() != txt {
		t.Errorf("got %q, wanted %q", buf.String(), txt)
	}
}

func TestInOutArray(t *testing.T) {
	defer enableLogging(t)()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	qry := `CREATE OR REPLACE PACKAGE test_pkg AS
TYPE int_tab_typ IS TABLE OF PLS_INTEGER INDEX BY PLS_INTEGER;
TYPE num_tab_typ IS TABLE OF NUMBER INDEX BY PLS_INTEGER;
TYPE vc_tab_typ IS TABLE OF VARCHAR2(100) INDEX BY PLS_INTEGER;
TYPE dt_tab_typ IS TABLE OF DATE INDEX BY PLS_INTEGER;
TYPE lob_tab_typ IS TABLE OF CLOB INDEX BY PLS_INTEGER;

PROCEDURE inout_int(p_int IN OUT int_tab_typ);
PROCEDURE inout_num(p_num IN OUT num_tab_typ);
PROCEDURE inout_vc(p_vc IN OUT vc_tab_typ);
PROCEDURE inout_dt(p_dt IN OUT dt_tab_typ);
PROCEDURE p2(p_int IN OUT int_tab_typ, p_num IN OUT num_tab_typ, p_vc IN OUT vc_tab_typ, p_dt IN OUT dt_tab_typ);
END test_pkg;
`
	if _, err := testDb.ExecContext(ctx, qry); err != nil {
		t.Fatal(err, qry)
	}
	defer testDb.Exec("DROP PACKAGE test_pkg")

	qry = `CREATE OR REPLACE PACKAGE BODY test_pkg AS
PROCEDURE inout_int(p_int IN OUT int_tab_typ) IS
  v_idx PLS_INTEGER;
BEGIN
  DBMS_OUTPUT.PUT_LINE('p_int.COUNT='||p_int.COUNT||' FIRST='||p_int.FIRST||' LAST='||p_int.LAST);
  v_idx := p_int.FIRST;
  WHILE v_idx IS NOT NULL LOOP
    p_int(v_idx) := NVL(p_int(v_idx) * 2, 1);
	v_idx := p_int.NEXT(v_idx);
  END LOOP;
  p_int(NVL(p_int.LAST, 0)+1) := p_int.COUNT;
END;

PROCEDURE inout_num(p_num IN OUT num_tab_typ) IS
  v_idx PLS_INTEGER;
BEGIN
  DBMS_OUTPUT.PUT_LINE('p_num.COUNT='||p_num.COUNT||' FIRST='||p_num.FIRST||' LAST='||p_num.LAST);
  v_idx := p_num.FIRST;
  WHILE v_idx IS NOT NULL LOOP
    p_num(v_idx) := NVL(p_num(v_idx) / 2, 0.5);
	v_idx := p_num.NEXT(v_idx);
  END LOOP;
  p_num(NVL(p_num.LAST, 0)+1) := p_num.COUNT;
END;

PROCEDURE inout_vc(p_vc IN OUT vc_tab_typ) IS
  v_idx PLS_INTEGER;
BEGIN
  DBMS_OUTPUT.PUT_LINE('p_vc.COUNT='||p_vc.COUNT||' FIRST='||p_vc.FIRST||' LAST='||p_vc.LAST);
  v_idx := p_vc.FIRST;
  WHILE v_idx IS NOT NULL LOOP
    p_vc(v_idx) := NVL(p_vc(v_idx) ||' +', '-');
	v_idx := p_vc.NEXT(v_idx);
  END LOOP;
  p_vc(NVL(p_vc.LAST, 0)+1) := p_vc.COUNT;
END;

PROCEDURE inout_dt(p_dt IN OUT dt_tab_typ) IS
  v_idx PLS_INTEGER;
BEGIN
  DBMS_OUTPUT.PUT_LINE('p_dt.COUNT='||p_dt.COUNT||' FIRST='||p_dt.FIRST||' LAST='||p_dt.LAST);
  v_idx := p_dt.FIRST;
  WHILE v_idx IS NOT NULL LOOP
    p_dt(v_idx) := NVL(p_dt(v_idx) + 1, SYSDATE);
	v_idx := p_dt.NEXT(v_idx);
  END LOOP;
  p_dt(NVL(p_dt.LAST, 0)+1) := TRUNC(SYSDATE);
END;

PROCEDURE p2(
	p_int IN OUT int_tab_typ,
	p_num IN OUT num_tab_typ,
	p_vc IN OUT vc_tab_typ,
	p_dt IN OUT dt_tab_typ
--, p_lob IN OUT lob_tab_typ
) IS
BEGIN
  inout_int(p_int);
  inout_num(p_num);
  inout_vc(p_vc);
  inout_dt(p_dt);
  --p_lob := NULL;
END p2;
END test_pkg;
`
	if _, err := testDb.ExecContext(ctx, qry); err != nil {
		t.Fatal(err, qry)
	}
	compileErrors, err := goracle.GetCompileErrors(testDb, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(compileErrors) != 0 {
		t.Fatalf("compile errors: %v", compileErrors)
	}

	intgr := []int32{3, 1, 4}
	intgrWant := []int32{3 * 2, 1 * 2, 4 * 2, 3}
	num := []string{"3.14", "-2.48"}
	numWant := []string{"1.57", "-1.24", "2"}
	vc := []string{"string", "bring"}
	vcWant := []string{"string +", "bring +", "2"}
	dt := []time.Time{time.Date(2017, 6, 18, 7, 5, 51, 0, time.Local), time.Time{}}
	dtWant := []time.Time{dt[0].Add(24 * time.Hour), time.Now().Truncate(24 * time.Hour)}

	t.Logf("vc=%#v", vc)
	goracle.EnableDbmsOutput(ctx, testDb)
	if _, err := testDb.ExecContext(ctx, "BEGIN test_pkg.inout_vc(:1); END;",
		goracle.PlSQLArrays,
		sql.Out{Dest: &vc, In: true},
	); err != nil {
		t.Fatalf("%+v", err)
	}
	t.Logf("vc=%#v", vc)
	if d := cmp.Diff(vc, []string{"string +", "bring +", "2"}); d != "" {
		t.Errorf("vc: %s", d)
		var buf bytes.Buffer
		if err := goracle.ReadDbmsOutput(ctx, &buf, testDb); err != nil {
			t.Error(err)
		}
		t.Log("OUTPUT:", buf.String())
		return
	}
	//lob := []goracle.Lob{goracle.Lob{IsClob: true, Reader: strings.NewReader("abcdef")}}
	if _, err := testDb.ExecContext(ctx, "BEGIN test_pkg.inout_num(:1); END;",
		goracle.PlSQLArrays,
		sql.Out{Dest: &num, In: true},
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("num=%#v", intgr)
	if d := cmp.Diff(num, numWant); d != "" {
		t.Errorf("num: %s", d)
	}
	if _, err := testDb.ExecContext(ctx, "BEGIN test_pkg.inout_int(:1); END;",
		goracle.PlSQLArrays,
		sql.Out{Dest: &intgr, In: true},
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("int=%#v", intgr)
	if d := cmp.Diff(intgr, intgrWant); d != "" {
		t.Errorf("int: %s", d)
	}

	if _, err := testDb.ExecContext(ctx,
		"BEGIN test_pkg.p2(:1, :2, :3, :4); END;",
		goracle.PlSQLArrays,
		sql.Out{Dest: &intgr, In: true},
		sql.Out{Dest: &num, In: true},
		sql.Out{Dest: &vc, In: true},
		sql.Out{Dest: &dt, In: true},
		//sql.Out{Dest: &lob, In: true},
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("int=%#v num=%#v vc=%#v dt=%#v", intgr, num, vc, dt)
	if d := cmp.Diff(intgr, intgrWant); d != "" {
		t.Errorf("int: %s", d)
	}
	if d := cmp.Diff(num, numWant); d != "" {
		t.Errorf("num: %s", d)
	}
	if d := cmp.Diff(vc, vcWant); d != "" {
		t.Errorf("vc: %s", d)
	}
	if d := cmp.Diff(dt, dtWant); d != "" {
		t.Errorf("dt: %s", d)
	}
}

func TestOutParam(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	qry := `CREATE OR REPLACE PROCEDURE
test_p1(p_int IN OUT INTEGER, p_num IN OUT NUMBER, p_vc IN OUT VARCHAR2, p_dt IN OUT DATE, p_lob IN OUT CLOB)
IS
BEGIN
  p_int := NVL(p_int * 2, 1);
  p_num := NVL(p_num / 2, 0.5);
  p_vc := NVL(p_vc ||' +', '-');
  p_dt := NVL(p_dt + 1, SYSDATE);
  p_lob := NULL;
END;`
	if _, err := testDb.ExecContext(ctx, qry); err != nil {
		t.Fatal(err, qry)
	}
	defer testDb.Exec("DROP PROCEDURE test_p1")
	stmt, err := testDb.PrepareContext(ctx, "BEGIN test_p1(:1, :2, :3, :4, :5); END;")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	var intgr int = 3
	var num string = "3.14"
	var vc string = "string"
	var dt time.Time = time.Date(2017, 6, 18, 7, 5, 51, 0, time.Local)
	var lob goracle.Lob = goracle.Lob{IsClob: true, Reader: strings.NewReader("abcdef")}
	if _, err := stmt.ExecContext(ctx,
		sql.Out{Dest: &intgr, In: true},
		sql.Out{Dest: &num, In: true},
		sql.Out{Dest: &vc, In: true},
		sql.Out{Dest: &dt, In: true},
		sql.Out{Dest: &lob, In: true},
	); err != nil {
		t.Fatal(err)
	}
	t.Logf("int=%#v num=%#v vc=%#v dt=%#v", intgr, num, vc, dt)
	if intgr != 6 {
		t.Errorf("int: got %d, wanted %d", intgr, 6)
	}
	if num != "1.57" {
		t.Errorf("num: got %q, wanted %q", num, "1.57")
	}
	if vc != "string +" {
		t.Errorf("vc: got %q, wanted %q", vc, "string +")
	}
}

func TestSelectRefCursor(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const num = 1000
	rows, err := testDb.QueryContext(ctx, "SELECT CURSOR(SELECT object_name, object_type, object_id, created FROM all_objects WHERE ROWNUM < 1000) FROM DUAL")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var intf interface{}
		if err := rows.Scan(&intf); err != nil {
			t.Error(err)
			continue
		}
		t.Logf("%T", intf)
		sub := intf.(driver.RowsColumnTypeScanType)
		cols := sub.Columns()
		t.Log("Columns", cols)
		dests := make([]driver.Value, len(cols))
		for {
			if err := sub.Next(dests); err != nil {
				if err == io.EOF {
					break
				}
				t.Error(err)
				break
			}
			//fmt.Println(dests)
			t.Log(dests)
		}
		sub.Close()
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const num = 1000
	rows, err := testDb.QueryContext(ctx, "SELECT object_name, object_type, object_id, created FROM all_objects WHERE ROWNUM < NVL(:alpha, 2) ORDER BY object_id", sql.Named("alpha", num))
	//rows, err := testDb.QueryContext(ctx, "SELECT object_name, object_type, object_id, created FROM all_objects WHERE ROWNUM < 1000 ORDER BY object_id")
	if err != nil {
		t.Fatalf("%+v", err)
	}
	n, oldOid := 0, int64(0)
	for rows.Next() {
		var tbl, typ string
		var oid int64
		var created time.Time
		if err := rows.Scan(&tbl, &typ, &oid, &created); err != nil {
			t.Fatal(err)
		}
		t.Log(tbl, typ, oid, created)
		if tbl == "" {
			t.Fatal("empty tbl")
		}
		n++
		if oldOid > oid {
			t.Errorf("got oid=%d, wanted sth < %d.", oid, oldOid)
		}
		oldOid = oid
	}
	if n != num-1 {
		t.Errorf("got %d rows, wanted %d", n, num-1)
	}
}

func TestExecuteMany(t *testing.T) {
	t.Parallel()
	testDb.Exec("DROP TABLE test_em")
	testDb.Exec("CREATE TABLE test_em (f_id INTEGER, f_int INTEGER, f_num NUMBER, f_num_6 NUMBER(6), F_num_5_2 NUMBER(5,2), f_vc VARCHAR2(30), F_dt DATE)")
	defer testDb.Exec("DROP TABLE test_em")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const num = 1000
	ints := make([]int, num)
	nums := make([]string, num)
	int32s := make([]int32, num)
	floats := make([]float64, num)
	strs := make([]string, num)
	dates := make([]time.Time, num)
	now := time.Now()
	ids := make([]int, num)
	for i := range nums {
		ids[i] = i
		ints[i] = i << 1
		nums[i] = strconv.Itoa(i)
		int32s[i] = int32(i)
		floats[i] = float64(i) / float64(3.14)
		strs[i] = fmt.Sprintf("%x", i)
		dates[i] = now.Add(-time.Duration(i) * time.Hour)
	}
	for i, tc := range []struct {
		Name  string
		Value interface{}
	}{
		{"f_int", ints},
		{"f_num", nums},
		{"f_num_6", int32s},
		{"f_num_5_2", floats},
		{"f_vc", strs},
		{"f_dt", dates},
	} {
		res, err := testDb.ExecContext(ctx,
			"INSERT INTO test_em ("+tc.Name+") VALUES (:1)",
			tc.Value)
		if err != nil {
			t.Fatalf("%d. INSERT INTO test_em (%q) VALUES (%+v): %#v", i, tc.Name, tc.Value, err)
		}
		ra, err := res.RowsAffected()
		if err != nil {
			t.Error(err)
		} else if ra != num {
			t.Errorf("%d. %q: wanted %d rows, got %d", i, tc.Name, num, ra)
		}
	}

	testDb.ExecContext(ctx, "TRUNCATE TABLE test_em")

	res, err := testDb.ExecContext(ctx,
		`INSERT INTO test_em
		  (f_id, f_int, f_num, f_num_6, F_num_5_2, F_vc, F_dt)
		  VALUES
		  (:1, :2, :3, :4, :5, :6, :7)`,
		ids, ints, nums, int32s, floats, strs, dates)
	if err != nil {
		t.Fatalf("%#v", err)
	}
	ra, err := res.RowsAffected()
	if err != nil {
		t.Error(err)
	} else if ra != num {
		t.Errorf("wanted %d rows, got %d", num, ra)
	}

	rows, err := testDb.QueryContext(ctx, "SELECT * FROM test_em ORDER BY F_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id, Int int
		var num, vc string
		var num6 int32
		var num52 float64
		var dt time.Time
		if err := rows.Scan(&id, &Int, &num, &num6, &num52, &vc, &dt); err != nil {
			t.Fatal(err)
		}
		if id != i {
			t.Fatalf("ID got %d, wanted %d.", id, i)
		}
		if Int != ints[i] {
			t.Errorf("%d. INT got %d, wanted %d.", i, Int, ints[i])
		}
		if num != nums[i] {
			t.Errorf("%d. NUM got %q, wanted %q.", i, num, nums[i])
		}
		if num6 != int32s[i] {
			t.Errorf("%d. NUM_6 got %v, wanted %v.", i, num6, int32s[i])
		}
		rounded := float64(int64(floats[i]/0.005+0.5)) * 0.005
		if math.Abs(num52-rounded) > 0.05 {
			t.Errorf("%d. NUM_5_2 got %v, wanted %v.", i, num52, rounded)
		}
		if vc != strs[i] {
			t.Errorf("%d. VC got %q, wanted %q.", i, vc, strs[i])
		}
		if dt != dates[i].Truncate(time.Second) {
			t.Errorf("%d. got DT %v, wanted %v.", i, dt, dates[i])
		}
		i++
	}
}
func TestReadWriteLob(t *testing.T) {
	t.Parallel()
	testDb.Exec("DROP TABLE test_lob")
	testDb.Exec("CREATE TABLE test_lob (f_id NUMBER(6), f_blob BLOB, f_clob CLOB)")
	defer testDb.Exec("DROP TABLE test_lob")

	stmt, err := testDb.Prepare("INSERT INTO test_lob (F_id, f_blob, F_clob) VALUES (:1, :2, :3)")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	for tN, tC := range []struct {
		Bytes  []byte
		String string
	}{
		{[]byte{0, 1, 2, 3, 4, 5}, "12345"},
	} {

		if _, err := stmt.Exec(tN*2, tC.Bytes, tC.String); err != nil {
			t.Errorf("%d/1. (%v, %q): %v", tN, tC.Bytes, tC.String, err)
			continue
		}
		if _, err := stmt.Exec(tN*2+1,
			goracle.Lob{Reader: bytes.NewReader(tC.Bytes)},
			goracle.Lob{Reader: strings.NewReader(tC.String), IsClob: true},
		); err != nil {
			t.Errorf("%d/2. (%v, %q): %v", tN, tC.Bytes, tC.String, err)
		}

		rows, err := testDb.Query("SELECT F_id, F_blob, F_clob FROM test_lob WHERE F_id IN (:1, :2)", 2*tN, 2*tN+1)
		if err != nil {
			t.Errorf("%d/3. %v", tN, err)
			continue
		}
		for rows.Next() {
			var id, blob, clob interface{}
			if err := rows.Scan(&id, &blob, &clob); err != nil {
				rows.Close()
				t.Errorf("%d/3. scan: %v", tN, err)
				continue
			}
			t.Logf("%d. blob=%+v clob=%+v", id, blob, clob)
			if clob, ok := clob.(*goracle.Lob); !ok {
				t.Errorf("%d. %T is not LOB", id, blob)
			} else {
				got, err := ioutil.ReadAll(clob)
				if err != nil {
					t.Errorf("%d. %v", id, err)
				} else if got := string(got); got != tC.String {
					t.Errorf("%d. got %q for CLOB, wanted %q", id, got, tC.String)
				}
			}
			if blob, ok := blob.(*goracle.Lob); !ok {
				t.Errorf("%d. %T is not LOB", id, blob)
			} else {
				got, err := ioutil.ReadAll(blob)
				if err != nil {
					t.Errorf("%d. %v", id, err)
				} else if !bytes.Equal(got, tC.Bytes) {
					t.Errorf("%d. got %v for BLOB, wanted %v", id, got, tC.Bytes)
				}
			}
		}
		rows.Close()
	}
}

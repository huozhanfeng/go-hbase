package hbase

import (
	"runtime"
	"strconv"
	"sync"

	. "github.com/pingcap/check"
)

type ScanTestSuit struct {
	cli       HBaseClient
	tableName string
}

var _ = Suite(&ScanTestSuit{})

func (s *ScanTestSuit) SetUpSuite(c *C) {
	var (
		ok  bool
		err error
	)

	s.cli, err = NewClient(getTestZkHosts(), "/hbase")
	c.Assert(err, IsNil)

	s.tableName = "scan_test"
	table := NewTableNameWithDefaultNS(s.tableName)
	tblDesc := NewTableDesciptor(table)
	cf := newColumnFamilyDescriptor("cf", 3)
	tblDesc.AddColumnDesc(cf)
	s.cli.CreateTable(tblDesc, nil)

	for i := 1; i <= 10000; i++ {
		p := NewPut([]byte(strconv.Itoa(i)))
		p.AddValue([]byte("cf"), []byte("q"), []byte(strconv.Itoa(i)))
		ok, err = s.cli.Put("scan_test", p)
		c.Assert(ok, IsTrue)
		c.Assert(err, IsNil)
	}
}

func (s *ScanTestSuit) TearDownSuite(c *C) {
	err := s.cli.DisableTable(NewTableNameWithDefaultNS(s.tableName))
	c.Assert(err, IsNil)

	err = s.cli.DropTable(NewTableNameWithDefaultNS(s.tableName))
	c.Assert(err, IsNil)
}

func (s *ScanTestSuit) TestNextKey(c *C) {
	inputs := []struct {
		data   []byte
		result []byte
	}{
		{nil, []byte{0}},
		{[]byte{}, []byte{0}},
		{[]byte{0}, []byte{1}},
		{[]byte{1, 2, 3}, []byte{1, 2, 4}},
		{[]byte{1, 255}, []byte{2, 0}},
		{[]byte{255}, []byte{0, 0}},
		{[]byte{255, 255}, []byte{0, 0, 0}},
	}

	for _, input := range inputs {
		result := nextKey(input.data)
		c.Assert(result, BytesEquals, input.result, Commentf("%v - %v - %v", input.data, input.result, result))
	}
}

func (s *ScanTestSuit) TestScan(c *C) {
	runtime.GOMAXPROCS(runtime.NumCPU())
	wg := sync.WaitGroup{}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scan := NewScan([]byte("scan_test"), 100, s.cli)
			defer scan.Close()
			// [1, 5)
			scan.StartRow = []byte("1")
			scan.StopRow = []byte("5")

			cnt := 0
			for {
				r := scan.Next()
				if r == nil || scan.Closed() {
					break
				}
				cnt++
			}
			c.Assert(cnt, Equals, 4445)
		}()
	}

	wg.Wait()
}

func (s *ScanTestSuit) TestScanWithVersion(c *C) {
	// Test version put.
	p := NewPut([]byte("99999"))
	p.AddValue([]byte("cf"), []byte("q"), []byte("1002"))
	p.AddTimestamp(1002)
	ok, err := s.cli.Put("scan_test", p)
	c.Assert(ok, IsTrue)
	c.Assert(err, IsNil)

	p = NewPut([]byte("99999"))
	p.AddValue([]byte("cf"), []byte("q"), []byte("1003"))
	p.AddTimestamp(1003)
	ok, err = s.cli.Put("scan_test", p)
	c.Assert(ok, IsTrue)
	c.Assert(err, IsNil)

	p = NewPut([]byte("99999"))
	p.AddValue([]byte("cf"), []byte("q"), []byte("1001"))
	p.AddTimestamp(1001)
	ok, err = s.cli.Put("scan_test", p)
	c.Assert(ok, IsTrue)
	c.Assert(err, IsNil)

	// Test version get.
	g := NewGet([]byte("99999"))
	g.AddColumn([]byte("cf"), []byte("q"))
	g.AddTimeRange(1001, 1004)
	r, err := s.cli.Get("scan_test", g)
	c.Assert(r.Columns["cf:q"].Values, HasLen, 1)
	c.Assert(string(r.SortedColumns[0].Value), Equals, "1003")
	c.Assert(err, IsNil)

	g = NewGet([]byte("99999"))
	g.AddColumn([]byte("cf"), []byte("q"))
	g.AddTimeRange(1001, 1003)
	r, err = s.cli.Get("scan_test", g)
	c.Assert(r.Columns["cf:q"].Values, HasLen, 1)
	c.Assert(string(r.SortedColumns[0].Value), Equals, "1002")
	c.Assert(err, IsNil)

	g = NewGet([]byte("99999"))
	g.AddColumn([]byte("cf"), []byte("q"))
	g.AddTimeRange(999, 1001)
	r, err = s.cli.Get("scan_test", g)
	c.Assert(r, IsNil)
	c.Assert(err, IsNil)

	g = NewGet([]byte("99999"))
	g.AddColumn([]byte("cf"), []byte("q"))
	r, err = s.cli.Get("scan_test", g)
	c.Assert(err, IsNil)
	c.Assert(r.Columns["cf:q"].Values, HasLen, 1)
	c.Assert(string(r.SortedColumns[0].Value), Equals, "1003")
	c.Assert(r.SortedColumns[0].Ts, Equals, uint64(1003))

	g = NewGet([]byte("99999"))
	g.AddColumn([]byte("cf"), []byte("q"))
	g.Versions = 2
	r, err = s.cli.Get("scan_test", g)
	c.Assert(r.Columns["cf:q"].Values, HasLen, 2)
	value, ok := r.Columns["cf:q"].Values[1003]
	c.Assert(ok, IsTrue)
	c.Assert(string(value), Equals, "1003")
	value, ok = r.Columns["cf:q"].Values[1002]
	c.Assert(ok, IsTrue)
	c.Assert(string(value), Equals, "1002")

	g = NewGet([]byte("99999"))
	g.AddColumn([]byte("cf"), []byte("q"))
	g.Versions = 5
	r, err = s.cli.Get("scan_test", g)
	c.Assert(r.Columns["cf:q"].Values, HasLen, 3)
	value, ok = r.Columns["cf:q"].Values[1003]
	c.Assert(ok, IsTrue)
	c.Assert(string(value), Equals, "1003")
	value, ok = r.Columns["cf:q"].Values[1002]
	c.Assert(ok, IsTrue)
	c.Assert(string(value), Equals, "1002")
	value, ok = r.Columns["cf:q"].Values[1001]
	c.Assert(ok, IsTrue)
	c.Assert(string(value), Equals, "1001")

	g = NewGet([]byte("99999"))
	g.AddColumn([]byte("cf"), []byte("q"))
	g.Versions = 3
	g.AddTimeRange(999, 1002)
	r, err = s.cli.Get("scan_test", g)
	c.Assert(r.Columns["cf:q"].Values, HasLen, 1)
	c.Assert(string(r.SortedColumns[0].Value), Equals, "1001")
	c.Assert(r.SortedColumns[0].Ts, Equals, uint64(1001))

	// Test version scan.
	// Range -> [9999, 999999)
	scan := NewScan([]byte("scan_test"), 100, s.cli)
	scan.StartRow = []byte("9999")
	scan.StopRow = []byte("999999")

	r = scan.Next()
	c.Assert(r.SortedColumns, HasLen, 1)
	c.Assert(string(r.SortedColumns[0].Value), Equals, "9999")

	r = scan.Next()
	c.Assert(r.SortedColumns, HasLen, 1)
	c.Assert(string(r.SortedColumns[0].Value), Equals, "1003")

	r = scan.Next()
	c.Assert(r, IsNil)
	scan.Close()

	// Range -> [9999, 999999)
	// Version -> [1000, 1002)
	scan = NewScan([]byte("scan_test"), 100, s.cli)
	scan.StartRow = []byte("9999")
	scan.StopRow = []byte("999999")
	scan.AddTimeRange(1000, 1002)

	r = scan.Next()
	c.Assert(r.SortedColumns, HasLen, 1)
	c.Assert(string(r.SortedColumns[0].Value), Equals, "1001")

	r = scan.Next()
	c.Assert(r, IsNil)
	scan.Close()
}

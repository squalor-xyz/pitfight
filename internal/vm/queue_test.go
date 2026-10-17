// SPDX-License-Identifier: AGPL-3.0-or-later
package vm

import (
	"slices"
	"testing"
)

func TestSpawnCap(t *testing.T) {
	c, _ := NewCore(8)
	for i := 0; i < 8; i++ {
		c.Write(int64(i), parseCell(t, 8, "SPL.B $3, $0"), 0)
	}
	q, _ := c.NewQueue(8, 0)
	for i := 1; i <= 9; i++ {
		events := c.Step(q, 1, nil)
		want := min(i+1, 8)
		if q.Len() != want {
			t.Fatalf("step %d length %d want %d", i, q.Len(), want)
		}
		if (len(events) == 1) != (i < 8) {
			t.Fatalf("step %d spawn events %v", i, events)
		}
	}
}
func TestDeath(t *testing.T) {
	for _, tc := range stepCases {
		if tc.queue != "[]" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			c, q, _ := setupStep(t, tc)
			c.Step(q, 1, nil)
			if !q.Dead() {
				t.Fatal("bot should be dead")
			}
			if got := c.Step(q, 1, nil); len(got) != 0 {
				t.Fatalf("dead bot emitted %v", got)
			}
		})
	}
}
func TestQueueFIFO(t *testing.T) {
	c, _ := NewCore(8)
	c.Write(0, parseCell(t, 8, "SPL.B $3, $0"), 0)
	q, _ := c.NewQueue(4, 0, 6)
	c.Step(q, 1, nil)
	if !slices.Equal(q.Addresses(), []int32{6, 1, 3}) {
		t.Fatal(q.Addresses())
	}
	c.Step(q, 1, nil)
	if !slices.Equal(q.Addresses(), []int32{1, 3}) {
		t.Fatal(q.Addresses())
	}
	a := q.Addresses()
	a[0] = 7
	if q.Addresses()[0] != 1 {
		t.Fatal("addresses alias queue")
	}
}
func TestQueueValidation(t *testing.T) {
	c, _ := NewCore(8)
	for _, n := range []int{0, -1, MaxCoreSize + 1} {
		if _, e := c.NewQueue(n); e == nil {
			t.Fatalf("accepted cap %d", n)
		}
	}
	if _, e := c.NewQueue(1, 0, 1); e == nil {
		t.Fatal("accepted overfull queue")
	}
	q, e := c.NewQueue(2, -1, 9)
	if e != nil || !slices.Equal(q.Addresses(), []int32{7, 1}) {
		t.Fatalf("queue %v %v", q, e)
	}
	empty, _ := c.NewQueue(1)
	if !empty.Dead() {
		t.Fatal("empty queue alive")
	}
}

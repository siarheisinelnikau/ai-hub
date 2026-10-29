package metrics

import (
	"reflect"
	"testing"
)

func TestRingGrowsToMax(t *testing.T) {
	r := NewRing(3)
	r.Append("a")
	r.Append("b")
	if got := r.Items(); !reflect.DeepEqual(got, []interface{}{"a", "b"}) {
		t.Fatalf("Items = %v", got)
	}
	if r.Len() != 2 {
		t.Fatalf("Len = %d", r.Len())
	}
}

func TestRingEvictsOldest(t *testing.T) {
	r := NewRing(3)
	for _, v := range []string{"a", "b", "c", "d"} {
		r.Append(v)
	}
	if got := r.Items(); !reflect.DeepEqual(got, []interface{}{"b", "c", "d"}) {
		t.Fatalf("Items = %v, want [b c d]", got)
	}
	if r.Len() != 3 {
		t.Fatalf("Len = %d, want 3", r.Len())
	}
	last, ok := r.Last()
	if !ok || last != "d" {
		t.Fatalf("Last = %v, %v", last, ok)
	}
}

func TestRingFullWrapOrder(t *testing.T) {
	r := NewRing(2)
	for _, v := range []int{1, 2, 3} {
		r.Append(v)
	}
	if got := r.Items(); !reflect.DeepEqual(got, []interface{}{2, 3}) {
		t.Fatalf("Items = %v, want [2 3]", got)
	}
	last, ok := r.Last()
	if !ok || last != 3 {
		t.Fatalf("Last = %v, %v", last, ok)
	}
}

func TestRingDisabled(t *testing.T) {
	r := NewRing(0)
	r.Append("x")
	if got := r.Items(); len(got) != 0 {
		t.Fatalf("Items = %v, want empty", got)
	}
	if _, ok := r.Last(); ok {
		t.Fatal("Last on empty ring = ok, want false")
	}
}

func TestRingEmpty(t *testing.T) {
	r := NewRing(5)
	if got := r.Items(); len(got) != 0 || got == nil {
		t.Fatalf("Items = %v (nil=%v), want non-nil empty", got, got == nil)
	}
}

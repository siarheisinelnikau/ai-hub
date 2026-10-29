// Package metrics parses model-backend Prometheus metrics, mirroring app.py.
package metrics

// Ring is a fixed-size ring buffer (mirrors Python deque(maxlen=STATS_MAX)).
type Ring struct {
	max   int
	items []interface{}
	head  int
	full  bool
}

// NewRing creates a ring that keeps at most max items (0 = disabled).
func NewRing(max int) *Ring { return &Ring{max: max} }

// Append adds v, evicting the oldest item when full.
func (r *Ring) Append(v interface{}) {
	if r.max <= 0 {
		return
	}
	if len(r.items) < r.max {
		r.items = append(r.items, v)
		if len(r.items) == r.max {
			r.full = true
		}
		return
	}
	r.items[r.head] = v
	r.head = (r.head + 1) % r.max
}

// Items returns the buffer oldest→newest (Python deque order). Never nil.
func (r *Ring) Items() []interface{} {
	if r.max == 0 || len(r.items) == 0 {
		return []interface{}{}
	}
	n := len(r.items)
	out := make([]interface{}, 0, n)
	if !r.full {
		out = append(out, r.items...)
	} else {
		for i := 0; i < n; i++ {
			out = append(out, r.items[(r.head+i)%n])
		}
	}
	return out
}

// Last returns the most recent item.
func (r *Ring) Last() (interface{}, bool) {
	if len(r.items) == 0 {
		return nil, false
	}
	if !r.full {
		return r.items[len(r.items)-1], true
	}
	return r.items[(r.head+r.max-1)%r.max], true
}

// Len returns the number of stored items.
func (r *Ring) Len() int { return len(r.items) }

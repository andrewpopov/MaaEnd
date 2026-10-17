package autocollect

import "testing"

func TestRouteNoticesOncePerTaskRouteAndAccount(t *testing.T) {
	var n routeNotices
	for _, c := range []struct {
		task int64
		key  string
		want bool
	}{
		{1, "account-a:route-1:cooldown", true},
		{1, "account-a:route-1:cooldown", false},
		{1, "account-a:route-2:cooldown", true},
		{1, "account-b:route-1:cooldown", true},
		{2, "account-a:route-1:cooldown", true},
		{2, "account-a:route-1:cooldown", false},
	} {
		if got := n.first(c.task, c.key); got != c.want {
			t.Fatalf("task %d %s: got %v want %v", c.task, c.key, got, c.want)
		}
	}
	if len(n.seen) != 1 {
		t.Fatal("previous task's notice keys must be released")
	}
}

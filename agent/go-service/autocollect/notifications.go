package autocollect

import "sync"

// routeNotices avoids repeating cooldown notices on every dispatch-loop pass.
// It retains only the active task's keys and never retains framework handles.
type routeNotices struct {
	mu     sync.Mutex
	taskID int64
	seen   map[string]bool
}

func (n *routeNotices) first(taskID int64, key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.seen == nil || n.taskID != taskID {
		n.taskID = taskID
		n.seen = make(map[string]bool)
	}
	if n.seen[key] {
		return false
	}
	n.seen[key] = true
	return true
}

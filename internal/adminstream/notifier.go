package adminstream

import "sync"

const (
	VerticalConversation = "conversation"
	VerticalMemory       = "memory"
	VerticalEmbeddings   = "embeddings"
)

type Event struct {
	Vertical string
}

type Notifier struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func NewNotifier() *Notifier {
	return &Notifier{subscribers: make(map[chan Event]struct{})}
}

func (n *Notifier) Subscribe(buffer int) chan Event {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan Event, buffer)
	if n == nil {
		close(ch)
		return ch
	}
	n.mu.Lock()
	n.subscribers[ch] = struct{}{}
	n.mu.Unlock()
	return ch
}

func (n *Notifier) Unsubscribe(ch chan Event) {
	if n == nil || ch == nil {
		return
	}
	n.mu.Lock()
	if _, ok := n.subscribers[ch]; ok {
		delete(n.subscribers, ch)
		close(ch)
	}
	n.mu.Unlock()
}

func (n *Notifier) Publish(vertical string) {
	if n == nil {
		return
	}
	event := Event{Vertical: vertical}
	n.mu.RLock()
	defer n.mu.RUnlock()
	for ch := range n.subscribers {
		select {
		case ch <- event:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- event:
			default:
			}
		}
	}
}

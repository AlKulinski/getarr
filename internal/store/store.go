package store

import (
	"sync"
	"time"
)

// Status of a download request.
type Status string

const (
	StatusPending   Status = "pending"
	StatusAdded     Status = "added"
	StatusSearching Status = "searching"
	StatusError     Status = "error"
)

// Request records a single dl.* request.
type Request struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	Status    Status    `json:"status"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Store struct {
	mu       sync.RWMutex
	requests []Request
}

func New() *Store {
	return &Store{requests: make([]Request, 0)}
}

func (s *Store) Add(r Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append([]Request{r}, s.requests...)
	if len(s.requests) > 100 {
		s.requests = s.requests[:100]
	}
}

func (s *Store) Update(id string, status Status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.requests {
		if s.requests[i].ID == id {
			s.requests[i].Status = status
			s.requests[i].Message = message
			s.requests[i].UpdatedAt = time.Now()
			return
		}
	}
}

func (s *Store) List() []Request {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

package queue

import (
	"fmt"
	"log"
	"sync"
)

type AutoMergeQRepo struct {
	mux     sync.Mutex
	repo    fileRepository
	qHandle map[string]*AutoMergeQueueHandle
}

func NewAutoMergeQRepo(root string) *AutoMergeQRepo {
	repo := newFileRepository(root)
	if repo == nil {
		return nil
	}

	return &AutoMergeQRepo{
		mux:     sync.Mutex{},
		repo:    *repo,
		qHandle: make(map[string]*AutoMergeQueueHandle),
	}
}

func (s *AutoMergeQRepo) Get(owner string, name string) *AutoMergeQueueHandle {
	if ok := s.repo.validatePath(owner, name); !ok {
		return nil
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	k := owner + "/" + name
	h, ok := s.qHandle[k]
	if !ok {
		h = &AutoMergeQueueHandle{
			owner:  owner,
			name:   name,
			parent: s,
		}
		s.qHandle[k] = h
	}

	return h
}

func (s *AutoMergeQRepo) save(owner string, name string, v *AutoMergeQueue) {
	if ok := s.repo.save(owner, name, v); !ok {
		log.Printf("error: cannot save the queue information for %v/%v\n", owner, name)
	}
}

type AutoMergeQueueHandle struct {
	mux sync.Mutex

	owner  string
	name   string
	parent *AutoMergeQRepo
}

func (s *AutoMergeQueueHandle) Lock() {
	s.mux.Lock()
}
func (s *AutoMergeQueueHandle) Unlock() {
	s.mux.Unlock()
}

func (s *AutoMergeQueueHandle) Load() *AutoMergeQueue {
	owner := s.owner
	name := s.name

	ok, result := s.parent.repo.load(owner, name)
	if !ok || result == nil {
		result = &AutoMergeQueue{}
		s.parent.save(owner, name, result)
	}

	result.ownerHandle = s

	return result
}

func (s *AutoMergeQueueHandle) LoadAsRawByte() []byte {
	owner := s.owner
	name := s.name
	ok, b := s.parent.repo.loadAsByte(owner, name)
	if !ok {
		return nil
	}

	return b
}

type AutoMergeQueue struct {
	ownerHandle *AutoMergeQueueHandle

	q       []*AutoMergeQueueItem
	current *AutoMergeQueueItem
}

func (s *AutoMergeQueue) Save() {
	s.ownerHandle.parent.save(s.ownerHandle.owner, s.ownerHandle.name, s)
}

func (s *AutoMergeQueue) Push(item *AutoMergeQueueItem) bool {
	// Prevent to push a dupulicated item.
	for _, elm := range s.q {
		if elm.PullRequest == item.PullRequest {
			return false
		}
	}

	s.q = append(s.q, item)
	return true
}

func (s *AutoMergeQueue) TakeNext() (ok bool, item *AutoMergeQueueItem) {
	if len(s.q) == 0 {
		return true, nil
	}

	front, q := s.q[0], s.q[1:]
	s.q = q

	if front == nil {
		log.Println("error: the front of auto merge queue is nil")
		return
	}

	return true, front
}

func (s *AutoMergeQueue) Front() *AutoMergeQueueItem {
	if len(s.q) == 0 {
		return nil
	}

	return s.q[0]
}

func (s *AutoMergeQueue) IsAwaiting(pr int) (ok bool, item *AutoMergeQueueItem) {
	for _, item := range s.q {
		if item.PullRequest == pr {
			return true, item
		}
	}
	return
}

func (s *AutoMergeQueue) RemoveAwaiting(pr int) (found bool) {
	active := s.GetActive()
	if (active != nil) && (active.PullRequest == pr) {
		log.Printf("debug: the current active is %v\n", pr)
		s.RemoveActive()
		return true
	}

	n := make([]*AutoMergeQueueItem, 0, len(s.q)-1) // create small slice with huristic buffer size.
	for _, item := range s.q {
		if item.PullRequest == pr {
			found = true
		} else {
			n = append(n, item)
		}
	}

	s.q = n
	return found
}

func (s *AutoMergeQueue) GetActive() *AutoMergeQueueItem {
	return s.current
}

func (s *AutoMergeQueue) SetActive(item *AutoMergeQueueItem) error {
	if s.HasActive() {
		return fmt.Errorf("warn: active item has been already set!")
	}

	s.current = item
	return nil
}

func (s *AutoMergeQueue) RemoveActive() {
	s.current = nil
}

func (s *AutoMergeQueue) HasActive() bool {
	return s.current != nil
}

type AutoMergeQueueItem struct {
	// The number of the pull request.
	PullRequest int `json:"pull_request"`
	// The head sha of the pull request when it has been accepted.
	PrHead string `json:"pr_head_sha"`
	// The head sha of the branch which trying to merge into the upstream
	AutoBranchHead *string `json:"auto_head_sha"`
}

package amqp

import (
	"container/list"
	"sync"
)

type Pool[T any] struct {
	mu   sync.Mutex
	list *list.List
}

func NewPool[T any]() *Pool[T] {
	return &Pool[T]{
		list: list.New(),
	}
}

func (p *Pool[T]) Push(c *T) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.list.PushBack(c)
}

func (p *Pool[T]) Pull() *T {
	p.mu.Lock()
	defer p.mu.Unlock()

	e := p.list.Front()
	if e == nil {
		return nil
	}
	p.list.Remove(e)

	return e.Value.(*T)
}

func (p *Pool[T]) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.list.Len()
}

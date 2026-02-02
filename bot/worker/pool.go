package worker

import (
	"context"
	"errors"
	"sync"
)

var ErrPoolClosed = errors.New("worker pool closed")

// Pool provides bounded concurrency execution.
type Pool struct {
	tasks    chan func()
	wg       sync.WaitGroup
	shutdown chan struct{}
	mu       sync.Mutex
	closed   bool
	size     int
}

// New creates a worker pool with the given size.
func New(size int) *Pool {
	if size <= 0 {
		size = 1
	}

	queueSize := size * 8
	if queueSize < 8 {
		queueSize = 8
	}

	p := &Pool{
		tasks:    make(chan func(), queueSize),
		shutdown: make(chan struct{}),
		size:     size,
	}

	for i := 0; i < size; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for task := range p.tasks {
				if task != nil {
					task()
				}
			}
		}()
	}

	return p
}

// Submit enqueues a task for execution.
func (p *Pool) Submit(task func()) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPoolClosed
	}
	p.mu.Unlock()

	select {
	case <-p.shutdown:
		return ErrPoolClosed
	case p.tasks <- task:
		return nil
	}
}

// SubmitWait enqueues a task and waits for it to complete.
func (p *Pool) SubmitWait(task func() error) error {
	if task == nil {
		return nil
	}

	result := make(chan error, 1)
	err := p.Submit(func() {
		result <- task()
	})
	if err != nil {
		return err
	}

	return <-result
}

// Shutdown waits for in-flight tasks until context is done.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.shutdown)
		close(p.tasks)
	}
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

// StopNow closes the pool without waiting for tasks to finish.
func (p *Pool) StopNow() {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.shutdown)
		close(p.tasks)
	}
	p.mu.Unlock()
}

// Size returns the worker count.
func (p *Pool) Size() int {
	return p.size
}

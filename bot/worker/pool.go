package worker

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
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
	onPanic  func(recovered any, stack []byte)
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
			for {
				select {
				case <-p.shutdown:
					return
				case task, ok := <-p.tasks:
					if !ok {
						return
					}
					if task != nil {
						func() {
							defer func() {
								if r := recover(); r != nil {
									p.handlePanic(r, debug.Stack())
								}
							}()
							task()
						}()
					}
				}
			}
		}()
	}

	return p
}

// SetPanicHandler sets an optional callback for recovered panics in worker tasks.
// The callback receives the recovered value and stack trace bytes.
func (p *Pool) SetPanicHandler(handler func(recovered any, stack []byte)) {
	p.mu.Lock()
	p.onPanic = handler
	p.mu.Unlock()
}

func (p *Pool) handlePanic(recovered any, stack []byte) {
	p.mu.Lock()
	handler := p.onPanic
	p.mu.Unlock()
	if handler != nil {
		handler(recovered, stack)
	}
}

// Submit enqueues a task for execution.
func (p *Pool) Submit(task func()) error {
	select {
	case <-p.shutdown:
		return ErrPoolClosed
	default:
	}

	select {
	case <-p.shutdown:
		return ErrPoolClosed
	case p.tasks <- task:
		return nil
	}
}

// SubmitWait enqueues a task and waits for it to complete.
func (p *Pool) SubmitWait(task func() error) error {
	return p.SubmitWaitContext(context.Background(), task)
}

// SubmitWaitContext enqueues a task and waits for completion or context cancellation.
func (p *Pool) SubmitWaitContext(ctx context.Context, task func() error) error {
	if task == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result := make(chan error, 1)
	err := p.Submit(func() {
		defer func() {
			if r := recover(); r != nil {
				p.handlePanic(r, debug.Stack())
				result <- fmt.Errorf("task panic: %v", r)
			}
		}()
		result <- task()
	})
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err = <-result:
		return err
	}
}

// Shutdown waits for in-flight tasks until context is done.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.shutdown)
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
	}
	p.mu.Unlock()
}

// Size returns the worker count.
func (p *Pool) Size() int {
	return p.size
}

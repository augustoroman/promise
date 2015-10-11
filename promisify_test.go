package promise

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func panicIfCalled(val interface{}) interface{} { panic("oops") }

type incrementor chan int

func (i incrementor) process(val interface{}) interface{} {
	v := val.(int)
	i <- v
	return v + 1
}

func TestPromiseFulfilled(t *testing.T) {
	defer time.AfterFunc(time.Second, t.FailNow).Stop() // limit test to 1 second running time.

	done1, done2, done3 := make(incrementor, 1), make(incrementor, 1), make(incrementor, 1)
	var a Promise
	a2 := a.Then(done1.process, panicIfCalled)
	a3 := a2.Then(done2.process, panicIfCalled)
	a3.Then(done3.process, panicIfCalled)

	// shouldn't break anything.
	a.Then(nil, nil)
	a2.Then(nil, nil)

	// Validate that nothing happens while it's pending.
	select {
	case <-done1:
		t.Fatal("Wasn't supposed to receive yet!")
	case <-time.After(10 * time.Millisecond):
		// yay!
	}

	// Resolve the promise and trigger the downstream dependencies.
	assert.Equal(t, a.Resolve(1), 1)
	assert.Equal(t, <-done1, 1)
	assert.Equal(t, <-done2, 2)
	assert.Equal(t, <-done3, 3)

	// Can't resolve more than once:
	assert.Panics(t, func() { a.Resolve(2) })
	assert.Panics(t, func() { a2.Reject(3) })

	// Subsequent calls to then are immediately executed.
	a.Then(done1.process, panicIfCalled)
	assert.Equal(t, <-done1, 1)
}

func TestPromiseRejected(t *testing.T) {
	defer time.AfterFunc(time.Second, t.FailNow).Stop() // limit test to 1 second running time.

	done1, done2, done3 := make(incrementor, 1), make(incrementor, 1), make(incrementor, 1)
	var a Promise
	a2 := a.Then(panicIfCalled, done1.process)
	a3 := a2.Then(panicIfCalled, done2.process)
	a3.Then(panicIfCalled, done3.process)

	// Validate that nothing happens while it's pending.
	select {
	case <-done1:
		t.Fatal("Wasn't supposed to receive yet!")
	case <-time.After(10 * time.Millisecond):
		// yay!
	}

	// Resolve the promise and trigger the downstream dependencies.
	assert.Equal(t, a.Reject(1), 1)
	assert.Equal(t, <-done1, 1)
	assert.Equal(t, <-done2, 2)
	assert.Equal(t, <-done3, 3)

	// Can't resolve more than once:
	assert.Panics(t, func() { a.Resolve(2) })
	assert.Panics(t, func() { a2.Reject(3) })

	// Subsequent calls to then are immediately queued.
	a.Then(panicIfCalled, done1.process)
	assert.Equal(t, <-done1, 1)
}

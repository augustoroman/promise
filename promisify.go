// Package promise provides support for returning Promises in gopherjs.
//
// The simplest usage is to use the Promisify() function to convert a
// (potentially-blocking) function call into a promise.  This allows easily
// converting a typical synchronous (idiomatic) Go API into a promise-based
// (idiomatic) JS api.
//
// For example:
//
//     func main() {
//       js.Global.Set("whoami", promise.Promisify(whoami))
//
//       // or as part of a structed object:
//       js.Global.Set("api", map[string]interface{}{
//         "whoami": promise.Promisify(whoami),
//       })
//     }
//
//     // This is a blocking function -- it doesn't return until the XHR
//     // completes or fails.
//     func whoami() (User, error) {
//       if resp, err := http.Get("/api/whoami"); err != nil {
//         return nil, err
//       }
//       return parseUserJson(resp)
//     }
//
// Promisify allows JS to call the underlying function via reflection and
// automatically detects an 'error' return type, using the following rules, in
// order:
//
//   * If the function panics, the promise is rejected with the panic value.
//   * If the last return is of type 'error', then the promise is rejected if
//     the returned error is non-nil.
//   * The promise is resolved with the remaining return values, according to
//     how many there are:
//       0:  resolved with nil
//       1:  resolved with that value
//       2+: resolved with a slice of the values
//
// If you want to manage the promise directly, use Promise:
//
//     func whoamiPromise() *js.Object {
//       var p promise.Promise
//       go func() {
//       	if user, err := whoami(); err == nil {
//       		p.Resolve(user)
//       	} else {
//       		p.Reject(err)
//       	}
//       }
//       return p.Js()
//     }
//
//
// Known Issues
//
// This package still has some rough edges:
//
//    * Does not adopt promise state when a promise is returned from a handler.
//      E.g.:
//        func Op1() Promise {...}
//        func Op2() Promise {...}
//        // Fails because when Op2 returns a promise, log is immediately called
//        // instead of waiting for the Op2 promise to be fulfilled.
//        Op1().Then(Op2, nil).Then(log, nil)
//      To fix this, we need to be able to inspect the result of the success
//      function and determine if it's a Promise (has a .then method), and if
//      so we need to trigger downstream promises off of that instead of
//      directly passing the result to the downstream promises.
//    * Does not do JS object type detection on .then() args.  The promises
//      spec suggests we should handle arbitrary arguments.
//      E.g:
//        somePromise.then(function(){...}, 123) should be equivalent to
//        somePromise.then(function(){...})
//    * Promisify() doesn't not auto-convert JS to strongly-typed Go types.
//      E.g.:
//        type Foo string
//        func something(f Foo) {...}
//      cannot be called from JS as:
//        something("asdf")
//      Instead, something must have signature:
//        func something(f string) { ... }
//
package promise

import (
	"fmt"
	"reflect"

	"github.com/gopherjs/gopherjs/js"
)

// Callbacks are provided to promises and called when the promise is fulfilled
// (with the fulfilled value) or rejected (with the error).  The return of the
// callback is passed to dependencies.
type Callback func(value interface{}) interface{}

// state of the promise: pending, fulfilled, rejected
type state int

const (
	pending state = iota
	fulfilled
	rejected
)

func (s state) String() string {
	switch s {
	case pending:
		return "pending"
	case fulfilled:
		return "fulfilled"
	case rejected:
		return "rejected"
	default:
		panic(fmt.Errorf("Unknown state: %d", int(s)))
	}
}

func undefined(v interface{}) interface{} { return v }

func safe(c Callback) Callback {
	if c != nil {
		return c
	}
	return undefined
}

// Promise represents most of an implementation of the JS Promise/A+ spec
// (https://promisesaplus.com/).
//
// Typical usage is:
//
//   func ExportedToJavascript(arg1 string, arg2 int, ...) *Promise {
//     var p Promise
//     go func() {
//       result, err := computeResult(arg1, arg2, ...)
//       if err == nil {
//         p.Resolve(result)
//       } else {
//         p.Reject(err)
//       }
//     }()
//     return p.Js()
//   }
//
// This structure can be automatically implemented by Promisify(...), for
// example:
//   Promisify(computeResult)
//
type Promise struct {
	state state
	value interface{}

	success, failure []Callback
}

// Then registers success and failure to be called if the promise is fulfilled
// or rejected respectively.  It returns a new promise that will be resolved or
// rejected with the result of the success or failure callbacks.
//
// Note that if success or failure return a promise, the promise itself is
// passed along as the value rather than adopting the returned promise's state.
func (p *Promise) Then(success, failure Callback) *Promise {
	var child Promise
	success, failure = child.wrap(success, failure)
	p.success = append(p.success, success)
	p.failure = append(p.failure, failure)
	p.flush()
	return &child
}

// wrap returns a new pair of callbacks that will not only call the provided
// callbacks on fulfillment or rejection, but will also resolve or reject this
// promise with the return values of those callbacks.
func (p *Promise) wrap(success, failure Callback) (Callback, Callback) {
	return func(val interface{}) interface{} {
			defer func() {
				if x := recover(); x != nil {
					p.Reject(x)
				}
			}()
			return p.Resolve(safe(success)(val))
		},
		func(val interface{}) interface{} { return p.Reject(safe(failure)(val)) }
}

func (p *Promise) commit(s state, val interface{}, callbacks []Callback) {
	if p.state != pending {
		panic(fmt.Errorf("Cannot change p promise that isn't pending: %s", p.state))
	}
	p.value = val
	p.state = s
}

func (p *Promise) flush() {
	if p.state == pending {
		return
	}

	if p.state == fulfilled {
		go sendSoon(p.value, p.success)
	} else if p.state == rejected {
		go sendSoon(p.value, p.failure)
	}
	p.success = nil
	p.failure = nil
}

// This is explicitly not part of the Promise object so we don't mutate state.
// In JS, this is asynchronously scheduled in the next process tick.  In Go,
// this is run concurrently.  So we explicitly accept the arguments and hold
// them here, they should not be modified after this goroutine is started.
func sendSoon(val interface{}, callbacks []Callback) {
	for _, cb := range callbacks {
		if cb != nil {
			cb(val)
		}
	}
}

// Resolve this promise with the provided value.  Either Resolve or Reject may
// be called at most once on a promise instance.
func (p *Promise) Resolve(value interface{}) interface{} {
	p.commit(fulfilled, value, p.success)
	p.flush()
	return value
}

// Reject this promise with the specified errror.  Either Resolve or Reject may
// be called at most once on a promise instance.
func (p *Promise) Reject(err interface{}) interface{} {
	p.commit(rejected, err, p.failure)
	p.flush()
	return err
}

func jsCallback(f *js.Object) Callback {
	if f == nil || f == js.Undefined {
		return nil
	}
	return func(val interface{}) interface{} { return f.Invoke(val) }
}

// Js creates a JS wrapper object for this promise that includes the 'then'
// method required by the Promises/A+ spec.
func (p *Promise) Js() *js.Object {
	o := js.MakeWrapper(p)
	o.Set("then", func(success, failure *js.Object) *js.Object {
		return p.Then(jsCallback(success), jsCallback(failure)).Js()
	})
	return o
}

// Promisify takes any Go function and converts it to a function that runs
// asynchronously and returns a Promise.
//
// Note: Currently this does not convert javascript types to Go types even if
// they are structurally equivalent.  It therefore works only with plain data
// types or values explicitly created by Go code (passed back to java).
func Promisify(fn interface{}) interface{} {
	f := reflect.ValueOf(fn)
	return func(args ...interface{}) *js.Object {
		var p Promise
		go func() {
			// TODO(aroman) Attempt to convert all args to the parameter type.
			results := f.Call(reflectAll(args...))
			value, err := splitResults(results, hasLastError(f.Type()))
			if err == nil {
				p.Resolve(value)
			} else {
				p.Reject(err.Error())
			}
		}()
		return p.Js()
	}
}

var errorType = reflect.ValueOf((*error)(nil)).Type().Elem()

func reflectAll(args ...interface{}) []reflect.Value {
	reflected := make([]reflect.Value, len(args))
	for i := range args {
		reflected[i] = reflect.ValueOf(args[i])
	}
	return reflected
}

func unReflectAll(results []reflect.Value) []interface{} {
	outs := make([]interface{}, len(results))
	for i := range results {
		outs[i] = results[i].Interface()
	}
	return outs
}

func desliceOne(vals []interface{}) interface{} {
	if len(vals) == 0 {
		return nil
	} else if len(vals) == 1 {
		return vals[0]
	}
	return vals
}

func splitResults(results []reflect.Value, lastError bool) (interface{}, error) {
	N := len(results)
	var err error
	if lastError && N > 0 {
		var errval reflect.Value
		results, errval = results[:N-1], results[N-1]
		if errval.IsValid() && !errval.IsNil() {
			err = errval.Interface().(error)
		}
	}
	return desliceOne(unReflectAll(results)), err
}

func hasLastError(t reflect.Type) bool {
	N := t.NumOut()
	if N == 0 {
		return false
	}
	return t.Out(N-1) == errorType
}

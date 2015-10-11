# promise [![Documentation](https://img.shields.io/badge/godoc-reference-blue.svg?style=flat-square)](https://godoc.org/github.com/augustoroman/promise)

Package promise provides support for returning Promises in gopherjs.
The simplest usage is to use the Promisify() function to convert a
(potentially-blocking) function call into a promise.  This allows easily
converting a typical synchronous (idiomatic) Go API into a promise-based
(idiomatic) JS api.

For example:

    func main() {
      js.Global.Set("whoami", Promisify(whoami))
      // or as part of a structed object:
      js.Global.Set("api", map[string]interface{}{
        "whoami": Promisify(whoami),
      })
    }
    // This is a blocking function -- it doesn't return until the XHR
    // completes or fails.
    func whoami() (User, error) {
      if resp, err := http.Get("/api/whoami"); err != nil {
        return nil, err
      }
      return parseUserJson(resp)
    }

Promisify allows JS to call the underlying function via reflection and
automatically detects an 'error' return type, using the following rules, in
order:
  * If the function panics, the promise is rejected with the panic value.
  * If the last return is of type 'error', then the promise is rejected if
    the returned error is non-nil.
  * The promise is resolved with the remaining return values, according to
    how many there are:

        0:  resolved with nil
        1:  resolved with that value
        2+: resolved with a slice of the values

If you want to manage the promise directly, use Promise:

    func whoamiPromise() *js.Object {
      var p promise.Promise
      go func() {
      	if user, err := whoami(); err == nil {
      		p.Resolve(user)
      	} else {
      		p.Reject(err)
      	}
      }
      return p.Js()
    }

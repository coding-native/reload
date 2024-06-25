// Package reload exposes the middleware Handle(), which can be used to trigger a reload
// in the browser whenever a file is changed.
//
// Reload doesn't require any external tools and is can be
// integrated into any project that uses the standard net/http interface.
//
// Typically, integrating this package looks like this:
//
// 1. Create a new Reloader and insert the middleware to your handler chain:
//
//	// handler can be anything that implements http.Handler,
//	// like chi.Router, echo.Echo or gin.Engine
//	var handler http.Handler = http.DefaultServeMux
//
//	if isDevelopment {
//	   // Call `New()` with a list of directories to recursively watch
//	   reloader := reload.New("ui/")
//
//	   // Optionally, define a callback to
//	   // invalidate any caches
//	   reloader.OnReload = func() {
//	      app.parseTemplates()
//	   }
//
//	   // Use the Handle() method as a middleware
//	   handler = reloader.Handle(handler)
//	}
//
//	http.ListenAndServe(addr, handler)
//
// 2. Run your application, make changes to files in the specified directories, and see the updated page instantly!
// The package also exposes `ServeWS`, `InjectScript`, `Wait` and `WatchDirectories`,
// which can be used to embed the script in the templates directly.
//
// See the full example at https://github.com/aarol/reload/blob/main/example/main.go
package reload

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// incremented each time a breaking change is made
const wsCurrentVersion = "1"

type Reloader struct {
	// OnReload will be called after a file changes, but before the browser reloads.
	OnReload func()
	// directories to recursively watch
	directories []string
	// Endpoint defines what path the WebSocket connection is formed over.
	// It is set to "/reload_ws" by default.
	Endpoint string
	// When set to true, prevents the middleware from sending a "Cache-Control=no-cache" header on each request.
	//
	// Some handlers, like http.FileServer send Last-Modified headers, which prevent the browser from refetching changed files correctly.
	//
	// To prevent confusion, caching is disabled by default.
	// It is also possible to enable this option, and use a middleware like Chi's NoCache (https://github.com/go-chi/chi/blob/master/middleware/nocache.go)
	AllowCaching bool

	Log *log.Logger

	// Used to upgrade connections to Websocket connections
	Upgrader websocket.Upgrader

	// Used to trigger a reload on all websocket connections at once
	cond           *sync.Cond
	startedWatcher bool
}

// New returns a new Reloader with the provided directories.
func New(directories ...string) *Reloader {
	return &Reloader{
		directories:  directories,
		Endpoint:     "/reload_ws",
		Log:          log.New(os.Stdout, "Reload: ", log.Lmsgprefix|log.Ltime),
		Upgrader:     websocket.Upgrader{},
		AllowCaching: false,

		startedWatcher: false,
		cond:           sync.NewCond(&sync.Mutex{}),
	}
}

// Handle starts the reload middleware, watching the specified directories and injecting the script into HTML responses.
func (reload *Reloader) Handle(next http.Handler) http.Handler {
	// Only init the watcher once
	if !reload.startedWatcher {
		go reload.WatchDirectories()
		reload.startedWatcher = true
	}
	scriptToInject := InjectedScript(reload.Endpoint)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Endpoint == "/reload_ws" by default
		if r.URL.Path == reload.Endpoint {
			reload.ServeWS(w, r)
			return
		}
		// set headers first so that they're sent with the initial response
		if reload.AllowCaching {
			w.Header().Set("Cache-Control", "no-cache")
		}

		body := &bytes.Buffer{}
		wrap := newWrapResponseWriter(w, r.ProtoMajor)
		// copy body so that we can sniff the content type
		wrap.Tee(body)

		next.ServeHTTP(wrap, r)
		contentType := w.Header().Get("Content-Type")

		if contentType == "" {
			contentType = http.DetectContentType(body.Bytes())
		}

		if strings.HasPrefix(contentType, "text/html") {
			// just append the script to the end of the document
			// this is invalid HTML, but browsers will accept it anyways
			// should be fine for development purposes
			w.Write([]byte(scriptToInject))
		}
	})
}

// ServeWS is the default websocket endpoint.
// Implementing your own is easy enough if you
// don't want to use 'gorilla/websocket'
func (reload *Reloader) ServeWS(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("v")
	if version != wsCurrentVersion {
		reload.Log.Printf(
			"Injected script version is out of date (v%s < v%s)\n",
			version,
			wsCurrentVersion,
		)
	}

	conn, err := reload.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		reload.Log.Printf("ServeWS error: %s\n", err)
		return
	}

	// Block here until next reload event
	reload.Wait()

	conn.WriteMessage(websocket.TextMessage, []byte("reload"))
	conn.Close()
}

func (reload *Reloader) Wait() {
	reload.cond.L.Lock()
	reload.cond.Wait()
	reload.cond.L.Unlock()
}

// Returns the javascript that will be injected on each HTML page.
func InjectedScript(endpoint string) string {
	return fmt.Sprintf(`
<script>
	function retry() {
	  setTimeout(() => listen(true), 1000)
	}
	function listen(isRetry) {
    let protocol = location.protocol === "https:" ? "wss://" : "ws://"
	  let ws = new WebSocket(protocol + location.host + "%s?v=%s")
	  if(isRetry) {
	    ws.onopen = () => window.location.reload()
	  }
	  ws.onmessage = function(msg) {
	    if(msg.data === "reload") {
	      window.location.reload()
	    }
	  }
	  ws.onclose = retry
	}
	listen(false)
</script>`, endpoint, wsCurrentVersion)
}

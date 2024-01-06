# Reload

![Tests](https://github.com/aarol/reload/actions/workflows/test.yml/badge.svg)

Reload is a Go library, which enables "hot reloading" of web server assets and templates, reloading the browser instantly via Websockets. The strength of Reload lies in it's simple API and easy integration to any Go projects.

## Installation

`go get github.com/aarol/reload`

## Usage

1. Create a new Reloader and insert the middleware to your handler chain:

   ```go
   // handler can be anything that implements http.Handler,
   // like chi.Router, echo.Echo or gin.Engine
   var handler http.Handler = http.DefaultServeMux

   if isDevelopment {
      // Call `New()` with a list of directories to recursively watch
      reloader := reload.New("ui/")
      
      // Optionally, define a callback to
      // invalidate any caches
      reloader.OnReload = func() {
         app.parseTemplates()
      }

      // Use the Handle() method as a middleware
      handler = reloader.Handle(handler)
   }

   http.ListenAndServe(addr, handler)
   ```

2. Run your application, make changes to files in the specified directories, and see the updated page instantly!

See the full example at <https://github.com/aarol/reload/blob/main/example/main.go>

## How it works

When added to the top of the middleware chain, `(*Reloader).Handle()` will inject a small `<script/>` at the end of any HTML file sent by your application. This script will instruct the browser to open a WebSocket connection back to your server, which will be also handled by the middleware.

The injected script is at the bottom of [this file](https://github.com/aarol/reload/blob/main/reload.go).

You can also do the injection yourself, as the package also exposes the methods `(*Reloader).ServeWS`, `(*Reloader).Wait` and `(*Reloader).WatchDirectories`, which are all used by the `(*Reloader).Handle` middleware.

> Currently, injecting the script is done by appending to the end of the document, even after the \</html\> tag. This makes the library code _much_ simpler, but may break older/less forgiving browsers.

## Caveats

- Reload works with everything that the server sends to the client (HTML,CSS,JS etc.), but it cannot restart the server itself, since it's just a middleware running on the server.

  To reload the entire server, you can use another file watcher on top, like [watchexec](https://github.com/watchexec/watchexec):

  `watchexec -r --exts .go -- go run .`

- Reload will not work for embedded assets, since all go:embed files are baked into the executable at build time.

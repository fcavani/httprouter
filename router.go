// Copyright 2013 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

// Package httprouter is a trie based high performance HTTP request router.
//
// A trivial example is:
//
//  package main
//
//  import (
//      "fmt"
//      "github.com/fcavani/httprouter"
//      "net/http"
//      "log"
//  )
//
//  func Index(w http.ResponseWriter, r *http.Request) {
//      fmt.Fprint(w, "Welcome!\n")
//  }
//
//  func Hello(w http.ResponseWriter, r *http.Request) {
//      fmt.Fprintf(w, "hello, %s!\n", Parameters(r).ByName("name"))
//  }
//
//  func main() {
//      router := httprouter.New()
//      router.GET("/", Index)
//      router.GET("/hello/:name", Hello)
//
//      log.Fatal(http.ListenAndServe(":8080", router))
//  }
//
// The router matches incoming requests by the request method and the path.
// If a handle is registered for this path and method, the router delegates the
// request to that function.
// For the methods GET, POST, PUT, PATCH and DELETE shortcut functions exist to
// register handles, for all other methods router.Handle can be used.
//
// The registered path, against which the router matches incoming requests, can
// contain two types of parameters:
//  Syntax    Type
//  :name     named parameter
//  *name     catch-all parameter
//
// Named parameters are dynamic path segments. They match anything until the
// next '/' or the path end:
//  Path: /blog/:category/:post
//
//  Requests:
//   /blog/go/request-routers            match: category="go", post="request-routers"
//   /blog/go/request-routers/           no match, but the router would redirect
//   /blog/go/                           no match
//   /blog/go/request-routers/comments   no match
//
// Catch-all parameters match anything until the path end, including the
// directory index (the '/' before the catch-all). Since they match anything
// until the end, catch-all parameters must always be the final path element.
//  Path: /files/*filepath
//
//  Requests:
//   /files/                             match: filepath="/"
//   /files/LICENSE                      match: filepath="/LICENSE"
//   /files/templates/article.html       match: filepath="/templates/article.html"
//   /files                              no match, but the router would redirect
//
// The value of parameters is saved as a slice of the Param struct, consisting
// each of a key and a value. The slice is stored in the requests context and
// is accessible by the function Parameters(r), where r is the pointer to the
// http.Request.
// There are two ways to retrieve the value of a parameter:
//  // by the name of the parameter
//  user := Parameters(r).ByName("user") // defined by :user or *user
//
//  // by the index of the parameter. This way you can also get the name (key)
//  thirdKey   := Parameters(r)[2].Key   // the name of the 3rd parameter
//  thirdValue := Parameters(r)[2].Value // the value of the 3rd parameter
package httprouter

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/fcavani/slog"
)

// Handle is a function that can be registered to a route to handle HTTP
// requests. Like http.HandlerFunc, but has a third parameter for the values of
// wildcards (variables).
//type Handle func(http.ResponseWriter, *http.Request, Params)

// Param is a single URL parameter, consisting of a key and a value.
type Param struct {
	Key   string
	Value string
}

// Params is a Param-slice, as returned by the router.
// The slice is ordered, the first URL parameter is also the first slice value.
// It is therefore safe to read values by the index.
type Params []Param

// ByName returns the value of the first Param which key matches the given name.
// If no matching Param is found, an empty string is returned.
func (ps Params) ByName(name string) string {
	for i := range ps {
		if ps[i].Key == name {
			return ps[i].Value
		}
	}
	return ""
}

type paramsKey struct{}

// ParamsKey is the request context key under which URL params are stored.
var ParamsKey = paramsKey{}

// ParamsFromContext pulls the URL parameters from a request context,
// or returns nil if none are present.
func ParamsFromContext(ctx context.Context) Params {
	p, _ := ctx.Value(ParamsKey).(Params)
	return p
}

// Router is a http.Handler which can be used to dispatch requests to different
// handler functions via configurable routes
type Router struct {
	trees map[string]*node

	paramsPool sync.Pool
	maxParams  uint16

	// Enables automatic redirection if the current route can't be matched but a
	// handler for the path with (without) the trailing slash exists.
	// For example if /foo/ is requested but a route only exists for /foo, the
	// client is redirected to /foo with http status code 301 for GET requests
	// and 308 for all other request methods.
	RedirectTrailingSlash bool

	// If enabled, the router tries to fix the current request path, if no
	// handle is registered for it.
	// First superfluous path elements like ../ or // are removed.
	// Afterwards the router does a case-insensitive lookup of the cleaned path.
	// If a handle can be found for this route, the router makes a redirection
	// to the corrected path with status code 301 for GET requests and 308 for
	// all other request methods.
	// For example /FOO and /..//Foo could be redirected to /foo.
	// RedirectTrailingSlash is independent of this option.
	RedirectFixedPath bool

	// If enabled, the router checks if another method is allowed for the
	// current route, if the current request can not be routed.
	// If this is the case, the request is answered with 'Method Not Allowed'
	// and HTTP status code 405.
	// If no other Method is allowed, the request is delegated to the NotFound
	// handler.
	HandleMethodNotAllowed bool

	// If enabled, the router automatically replies to OPTIONS requests.
	// Custom OPTIONS handlers take priority over automatic replies.
	HandleOPTIONS bool

	// An optional http.Handler that is called on automatic OPTIONS requests.
	// The handler is only called if HandleOPTIONS is true and no OPTIONS
	// handler for the specific path was set.
	// The "Allowed" header is set before calling the handler.
	GlobalOPTIONS http.Handler

	// Cached value of global (*) allowed methods
	globalAllowed string

	// Configurable http.Handler which is called when no matching route is
	// found. If it is not set, http.NotFound is used.
	NotFound http.Handler

	// Configurable http.Handler which is called when a request
	// cannot be routed and HandleMethodNotAllowed is true.
	// If it is not set, http.Error with http.StatusMethodNotAllowed is used.
	// The "Allow" header with allowed request methods is set before the handler
	// is called.
	MethodNotAllowed http.Handler

	// Function to handle panics recovered from http handlers.
	// It should be used to generate a error page and return the http error code
	// 500 (Internal Server Error).
	// The handler can be used to keep your server from crashing because of
	// unrecovered panics.
	PanicHandler func(http.ResponseWriter, *http.Request, interface{})

	// Context is a function to insert a new context just after the http request
	// context and before the router params.
	Context func(context.Context) (context.Context, context.CancelFunc)

	// Languages supporteds by the router.
	SupportedLangs map[string]struct{}
	// Fallback language for the case where there is no
	// lang in the url or the lang selected is not available
	// for that resource.
	DefaultLang string
}

// Make sure the Router conforms with the http.Handler interface
var _ http.Handler = New()

// New returns a new initialized Router.
// Path auto-correction, including trailing slashes, is enabled by default.
func New() *Router {
	return &Router{
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      true,
		HandleMethodNotAllowed: true,
		HandleOPTIONS:          true,
		Context: func(ctx context.Context) (context.Context, context.CancelFunc) {
			return ctx, nil
		},
		SupportedLangs: map[string]struct{}{
			"en": struct{}{},
		},
		DefaultLang: "",
	}
}

// HandlerPaths iter over all handlers path. If f returns
// false HandlerPaths stops iterate.
func (r *Router) HandlerPaths(params bool, f func(method, path string, h http.HandlerFunc) bool) {
	for m, n := range r.trees {
		if !iter(params, m, "", n, f) {
			return
		}
	}
}

func (r *Router) getParams() *Params {
	ps := r.paramsPool.Get().(*Params)
	*ps = (*ps)[0:0] // reset slice
	return ps
}

func (r *Router) putParams(ps *Params) {
	if ps != nil {
		r.paramsPool.Put(ps)
	}
}

// GET is a shortcut for router.Handle(http.MethodGet, path, i18n, handle)
func (r *Router) GET(path string, i18n bool, handle http.HandlerFunc) {
	r.Handle(http.MethodGet, path, i18n, handle)
}

// HEAD is a shortcut for router.Handle(http.MethodHead, path, i18n, handle)
func (r *Router) HEAD(path string, i18n bool, handle http.HandlerFunc) {
	r.Handle(http.MethodHead, path, i18n, handle)
}

// OPTIONS is a shortcut for router.Handle(http.MethodOptions, path, i18n, handle)
func (r *Router) OPTIONS(path string, handle http.HandlerFunc) {
	r.Handle(http.MethodOptions, path, false, handle)
}

// POST is a shortcut for router.Handle(http.MethodPost, path, i18n, handle)
func (r *Router) POST(path string, i18n bool, handle http.HandlerFunc) {
	r.Handle(http.MethodPost, path, i18n, handle)
}

// PUT is a shortcut for router.Handle(http.MethodPut, path, i18n, handle)
func (r *Router) PUT(path string, i18n bool, handle http.HandlerFunc) {
	r.Handle(http.MethodPut, path, i18n, handle)
}

// PATCH is a shortcut for router.Handle(http.MethodPatch, path, i18n, handle)
func (r *Router) PATCH(path string, handle http.HandlerFunc) {
	r.Handle(http.MethodPatch, path, false, handle)
}

// DELETE is a shortcut for router.Handle(http.MethodDelete, path, i18n, handle)
func (r *Router) DELETE(path string, handle http.HandlerFunc) {
	r.Handle(http.MethodDelete, path, false, handle)
}

// Handle registers a new request handle with the given path and method.
//
// For GET, POST, PUT, PATCH and DELETE requests the respective shortcut
// functions can be used.
//
// This function is intended for bulk loading and to allow the usage of less
// frequently used, non-standardized or custom methods (e.g. for internal
// communication with a proxy).
func (r *Router) Handle(method, path string, i18n bool, handle http.HandlerFunc) {
	if method == "" {
		panic("method must not be empty")
	}
	if len(path) < 1 || path[0] != '/' {
		panic("path must begin with '/' in path '" + path + "'")
	}
	if handle == nil {
		panic("handle must not be nil")
	}

	if r.trees == nil {
		r.trees = make(map[string]*node)
	}

	root := r.trees[method]
	if root == nil {
		root = new(node)
		r.trees[method] = root

		r.globalAllowed = r.allowed("*", "")
	}

	root.addRoute(path, i18n, handle)

	// Update maxParams
	if pc := countParams(path); pc > r.maxParams {
		r.maxParams = pc
	}

	// Lazy-init paramsPool alloc func
	if r.paramsPool.New == nil && r.maxParams > 0 {
		r.paramsPool.New = func() interface{} {
			ps := make(Params, 0, r.maxParams)
			return &ps
		}
	}
}

// Handler is an adapter which allows the usage of an http.Handler as a
// request handle.
func (r *Router) Handler(method, path string, i18n bool, handler http.Handler) {
	r.Handle(method, path, i18n,
		func(w http.ResponseWriter, req *http.Request) {
			handler.ServeHTTP(w, req)
		},
	)
}

// HandlerFunc is an adapter which allows the usage of an http.HandlerFunc as a
// request handle.
func (r *Router) HandlerFunc(method, path string, i18n bool, handler http.HandlerFunc) {
	r.Handler(method, path, i18n, handler)
}

// ServeFiles serves files from the given file system root.
// The path must end with "/*filepath", files are then served from the local
// path /defined/root/dir/*filepath.
// For example if root is "/etc" and *filepath is "passwd", the local file
// "/etc/passwd" would be served.
// Internally a http.FileServer is used, therefore http.NotFound is used instead
// of the Router's NotFound handler.
// To use the operating system's file system implementation,
// use http.Dir:
//     router.ServeFiles("/src/*filepath", http.Dir("/var/www"))
func (r *Router) ServeFiles(path string, root http.FileSystem) {
	if len(path) < 10 || path[len(path)-10:] != "/*filepath" {
		panic("path must end with /*filepath in path '" + path + "'")
	}

	fileServer := http.FileServer(root)

	r.GET(path, false, func(w http.ResponseWriter, req *http.Request) {
		req.URL.Path = Parameters(req).ByName("filepath")
		fileServer.ServeHTTP(w, req)
	})
}

func (r *Router) recv(w http.ResponseWriter, req *http.Request) {
	if rcv := recover(); rcv != nil {
		r.PanicHandler(w, req, rcv)
	}
}

// Lookup allows the manual lookup of a method + path combo.
// This is e.g. useful to build a framework around this router.
// If the path was found, it returns the handle function and the path parameter
// values. Otherwise the third return value indicates whether a redirection to
// the same path with an extra / without the trailing slash should be performed.
func (r *Router) Lookup(method, path string) (http.HandlerFunc, Params, bool) {
	trailSlash := false
	if path[len(path)-1] == '/' {
		trailSlash = true
	}
	psplit := splitPath(path)
	if r.DefaultLang != "" {
		if len(psplit) > 0 {
			candidate := psplit[0]
			_, found := r.SupportedLangs[candidate]
			if found {
				if len(psplit) > 1 {
					path = "/" + filepath.Join(psplit[1:]...)
				}
			}
		}
		if trailSlash {
			path += "/"
		}
	}

	if root := r.trees[method]; root != nil {
		handle, ps, _, tsr := root.getValue(path, r.getParams)
		if handle == nil {
			r.putParams(ps)
			return nil, nil, tsr
		}
		if ps == nil {
			return handle, nil, tsr
		}
		return handle, *ps, tsr
	}
	return nil, nil, false
}

func (r *Router) allowed(path, reqMethod string) (allow string) {
	allowed := make([]string, 0, 9)

	if path == "*" { // server-wide
		// empty method is used for internal calls to refresh the cache
		if reqMethod == "" {
			for method := range r.trees {
				if method == http.MethodOptions {
					continue
				}
				// Add request method to list of allowed methods
				allowed = append(allowed, method)
			}
		} else {
			return r.globalAllowed
		}
	} else { // specific path
		for method := range r.trees {
			// Skip the requested method - we already tried this one
			if method == reqMethod || method == http.MethodOptions {
				continue
			}

			handle, _, _, _ := r.trees[method].getValue(path, nil)
			if handle != nil {
				// Add request method to list of allowed methods
				allowed = append(allowed, method)
			}
		}
	}

	if len(allowed) > 0 {
		// Add request method to list of allowed methods
		allowed = append(allowed, http.MethodOptions)

		// Sort allowed methods.
		// sort.Strings(allowed) unfortunately causes unnecessary allocations
		// due to allowed being moved to the heap and interface conversion
		for i, l := 1, len(allowed); i < l; i++ {
			for j := i; j > 0 && allowed[j] < allowed[j-1]; j-- {
				allowed[j], allowed[j-1] = allowed[j-1], allowed[j]
			}
		}

		// return as comma separated list
		return strings.Join(allowed, ", ")
	}
	return
}

// ServeHTTP makes the router implement the http.Handler interface.
func (r *Router) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() {
		log.InfoLevel().Tag("httprouter", "statistics").Printf("Method=%v, Path=%v, Execution=%v", req.Method, req.URL.Path, time.Since(start))
	}()
	w := NewResponseWriter()
	defer func() {
		w.Copy(rw)
	}()

	path := req.URL.Path
	rawpath := path

	if r.PanicHandler != nil {
		defer r.recv(w, req)
	}

	if root := r.trees[req.Method]; root != nil {
		if handle, ps, i18n, tsr := root.getValue(path, r.getParams); handle != nil {
			if r.DefaultLang != "" && i18n == true {
				req, path = r.selectLang(w, req)
			}
			if ps != nil {
				req = req.WithContext(context.WithValue(req.Context(), "Params", *ps))
				handle(w, req)
				r.putParams(ps)
			} else {
				// Put in the context all parameters
				ctx := req.Context()
				if r.Context != nil {
					var cancelCtx context.CancelFunc
					ctx, cancelCtx = r.Context(ctx)
					if cancelCtx != nil {
						defer cancelCtx()
					}
				}

				req = req.WithContext(context.WithValue(req.Context(), "Params", nil))
				s := make(chan struct{})

				go func(w http.ResponseWriter, req *http.Request) {
					if r.PanicHandler != nil {
						defer func() {
							if rcv := recover(); rcv != nil {
								r.PanicHandler(w, req, rcv)
								s <- struct{}{}
							}
						}()
					}
					// Call handler
					handle(w, req)
					s <- struct{}{}
				}(w, req)

				select {
				case <-s:
					// Signal telling that the handle was executed.
					return
				case <-ctx.Done():
					w.Reset()
					switch ctx.Err() {
					case context.Canceled:
						http.Error(w,
							"Context canceled",
							http.StatusInternalServerError,
						)
						log.DebugLevel().Tag("httprouter").Printf("Context send a signal. Handler terminated. Method=%v Path=%v Err: context canceled.", req.Method, req.URL.Path)
					case context.DeadlineExceeded:
						// TODO: Custom error???
						http.Error(w,
							http.StatusText(http.StatusRequestTimeout),
							http.StatusRequestTimeout,
						)
						log.DebugLevel().Tag("httprouter").Printf("Context send a signal. Handler terminated. Method=%v Path=%v Err: deadline exceeded.", req.Method, req.URL.Path)
					default:
						http.Error(w,
							http.StatusText(http.StatusInternalServerError),
							http.StatusInternalServerError,
						)
						log.DebugLevel().Tag("httprouter").Printf("Context send a signal. Handler terminated. Method=%v Path=%v Err: unknown.", req.Method, req.URL.Path)
					}
				}
			}
			return
		} else if req.Method != http.MethodConnect && path != "/" {
			// Moved Permanently, request with GET method
			code := http.StatusMovedPermanently
			if req.Method != http.MethodGet {
				// Permanent Redirect, request with same method
				code = http.StatusPermanentRedirect
			}

			if tsr && r.RedirectTrailingSlash {
				if len(rawpath) > 1 && rawpath[len(rawpath)-1] == '/' {
					req.URL.Path = rawpath[:len(rawpath)-1]
				} else {
					req.URL.Path = rawpath + "/"
				}
				http.Redirect(w, req, req.URL.String(), code)
				return
			}

			// Try to fix the request path
			if r.RedirectFixedPath {
				fixedPath, found := root.findCaseInsensitivePath(
					CleanPath(path),
					r.RedirectTrailingSlash,
				)
				if found {
					req.URL.Path = fixedPath
					http.Redirect(w, req, req.URL.String(), code)
					return
				}
			}
		}
	}

	if req.Method == http.MethodOptions && r.HandleOPTIONS {
		// Handle OPTIONS requests
		if allow := r.allowed(path, http.MethodOptions); allow != "" {
			w.Header().Set("Allow", allow)
			if r.GlobalOPTIONS != nil {
				r.GlobalOPTIONS.ServeHTTP(w, req)
			}
			return
		}
	} else if r.HandleMethodNotAllowed { // Handle 405
		if allow := r.allowed(path, req.Method); allow != "" {
			w.Header().Set("Allow", allow)
			if r.MethodNotAllowed != nil {
				r.MethodNotAllowed.ServeHTTP(w, req)
			} else {
				http.Error(w,
					http.StatusText(http.StatusMethodNotAllowed),
					http.StatusMethodNotAllowed,
				)
			}
			return
		}
	}

	// Handle 404
	if r.NotFound != nil {
		r.NotFound.ServeHTTP(w, req)
	} else {
		http.NotFound(w, req)
	}
}

func (r *Router) selectLang(w http.ResponseWriter, req *http.Request) (*http.Request, string) {
	selectedLang := r.DefaultLang
	path := req.URL.Path

	if lang := r.acceptedLanguage(req); lang != "" {
		selectedLang = lang
	}

	if req.URL.String() != "*" {
		trailSlash := false
		if len(path) > 0 && path[len(path)-1] == '/' {
			trailSlash = true
		}
		psplit := splitPath(path)
		if len(psplit) > 0 {
			candidate := psplit[0]
			// TODO: Search for language of the same family.
			// Example: pt-BR not found, search for another, maybe pt-PT or pt.
			_, found := r.SupportedLangs[candidate]
			if found {
				selectedLang = candidate
				if len(psplit) > 1 {
					path = "/" + filepath.Join(psplit[1:]...)
					if trailSlash {
						path += "/"
					}
				} else if len(psplit) == 1 {
					path = "/"
				}
			} else {
				req = req.WithContext(context.WithValue(req.Context(), "UASelectedLang", selectedLang))
				redirLang(w, req, selectedLang)
				return req, path
			}
		} else {
			req = req.WithContext(context.WithValue(req.Context(), "UASelectedLang", selectedLang))
			redirLang(w, req, selectedLang)
			return req, path
		}
	}

	req = req.WithContext(context.WithValue(req.Context(), "UASelectedLang", selectedLang))

	return req, path
}

// PathExist returns true if a path exist. If the path
//have parameters its checks if the begin of the path
//exist.
func (r *Router) PathExist(name string) bool {
	for _, n := range r.trees {
		if find(name, "", n) {
			return true
		}
	}
	return false
}

// Parameters return the url params from the requests context.
func Parameters(req *http.Request) Params {
	p := req.Context().Value("Params")
	if p != nil {
		return p.(Params)
	}
	return nil
}

//ContentLang return the language negotiated with the client.
func ContentLang(req *http.Request) string {
	lang := req.Context().Value("UASelectedLang")
	if lang != nil {
		return lang.(string)
	}
	return ""
}

func (r *Router) acceptedLanguage(req *http.Request) string {
	params := req.Header.Get("Accept-Language")
	if params == "" {
		return ""
	}
	params = strings.ToLower(params)
	parsed, err := ParseLang(params)
	if err != nil {
		return ""
	}
	param := parsed.FindBest(r.SupportedLangs)
	if param == "" {
		return ""
	}
	return param
}

func redirLang(w http.ResponseWriter, req *http.Request, lang string) {
	code := 301
	if req.Method != "GET" {
		code = 307
	}
	req.URL.Path = "/" + lang + req.URL.Path
	http.Redirect(w, req, req.URL.String(), code)
}

func splitPath(path string) []string {
	p := strings.Split(path, "/")
	psplit := make([]string, 0, len(p))
	for _, elem := range p {
		if elem == "" {
			continue
		}
		psplit = append(psplit, strings.TrimSpace(elem))
	}
	return psplit
}

func find(name, path string, n *node) bool {
	if n.handle != nil {
		if n.nType == catchAll {
			if name == path {
				return true
			}
			return false
		} else if n.nType == param {
			if strings.HasPrefix(path, name) {
				return true
			}
			return false
		}
		if name == path+n.path {
			return true
		}
	}
	for _, child := range n.children {
		p := path + n.path
		if find(name, p, child) {
			return true
		}
	}
	return false
}

func iter(params bool, method, path string, n *node, f func(m, p string, h http.HandlerFunc) bool) bool {
	if n.handle != nil {
		if !params {
			if n.nType == catchAll {
				if !f(method, path, n.handle) {
					return false
				}
				return true
			} else if n.nType == param {
				path = strings.TrimSuffix(path, "/")
				i := strings.LastIndex(path, "/")
				if i >= 1 {
					path = path[:i]
				}
				if !f(method, path, n.handle) {
					return false
				}
				return true
			}
		}
		if !f(method, path+n.path, n.handle) {
			return false
		}
	}
	for _, child := range n.children {
		p := path + n.path
		if !iter(params, method, p, child, f) {
			return false
		}
	}
	return true
}

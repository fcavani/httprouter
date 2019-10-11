// Copyright 2013 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package httprouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	log "github.com/fcavani/slog"
)

func init() {
	log.SetLevel(log.DebugPrio)
}

type mockResponseWriter struct{}

func (m *mockResponseWriter) Header() (h http.Header) {
	return http.Header{}
}

func (m *mockResponseWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (m *mockResponseWriter) WriteString(s string) (n int, err error) {
	return len(s), nil
}

func (m *mockResponseWriter) WriteHeader(int) {}

func TestParams(t *testing.T) {
	ps := Params{
		Param{"param1", "value1"},
		Param{"param2", "value2"},
		Param{"param3", "value3"},
	}
	for i := range ps {
		if val := ps.ByName(ps[i].Key); val != ps[i].Value {
			t.Errorf("Wrong value for %s: Got %s; Want %s", ps[i].Key, val, ps[i].Value)
		}
	}
	if val := ps.ByName("noKey"); val != "" {
		t.Errorf("Expected empty string for not found key; got: %s", val)
	}
}

func TestRouter(t *testing.T) {
	router := New()

	routed := false
	router.Handle(http.MethodGet, "/user/:name", false, func(w http.ResponseWriter, r *http.Request) {
		routed = true
		want := Params{Param{"i18n", "false"}, Param{"name", "gopher"}}
		ps := Parameters(r)
		if !reflect.DeepEqual(ps, want) {
			t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
		}
	})

	w := new(mockResponseWriter)

	req, _ := http.NewRequest(http.MethodGet, "/user/gopher", nil)
	router.ServeHTTP(w, req)

	if !routed {
		t.Fatal("routing failed")
	}
}

type handlerStruct struct {
	handled *bool
}

func (h handlerStruct) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	*h.handled = true
}

func TestRouterAPI(t *testing.T) {
	var get, head, options, post, put, patch, delete, handler, handlerFunc bool
	//handler, handlerFunc bool

	httpHandler := handlerStruct{&handler}

	router := New()
	router.GET("/GET", false, func(w http.ResponseWriter, r *http.Request) {
		get = true
	})
	router.HEAD("/GET", false, func(w http.ResponseWriter, r *http.Request) {
		head = true
	})
	router.OPTIONS("/GET", func(w http.ResponseWriter, r *http.Request) {
		options = true
	})
	router.POST("/POST", false, func(w http.ResponseWriter, r *http.Request) {
		post = true
	})
	router.PUT("/PUT", false, func(w http.ResponseWriter, r *http.Request) {
		put = true
	})
	router.PATCH("/PATCH", func(w http.ResponseWriter, r *http.Request) {
		patch = true
	})
	router.DELETE("/DELETE", func(w http.ResponseWriter, r *http.Request) {
		delete = true
	})
	router.Handler("GET", "/Handler", false, httpHandler)
	router.HandlerFunc("GET", "/HandlerFunc", false, func(w http.ResponseWriter, r *http.Request) {
		handlerFunc = true
	})

	w := new(mockResponseWriter)

	r, _ := http.NewRequest("GET", "/GET", nil)
	router.ServeHTTP(w, r)
	if !get {
		t.Error("routing GET failed")
	}

	r, _ = http.NewRequest("HEAD", "/GET", nil)
	router.ServeHTTP(w, r)
	if !head {
		t.Error("routing HEAD failed")
	}

	r, _ = http.NewRequest("OPTIONS", "/GET", nil)
	router.ServeHTTP(w, r)
	if !options {
		t.Error("routing OPTIONS failed")
	}

	r, _ = http.NewRequest("POST", "/POST", nil)
	router.ServeHTTP(w, r)
	if !post {
		t.Error("routing POST failed")
	}

	r, _ = http.NewRequest("PUT", "/PUT", nil)
	router.ServeHTTP(w, r)
	if !put {
		t.Error("routing PUT failed")
	}

	r, _ = http.NewRequest("PATCH", "/PATCH", nil)
	router.ServeHTTP(w, r)
	if !patch {
		t.Error("routing PATCH failed")
	}

	r, _ = http.NewRequest("DELETE", "/DELETE", nil)
	router.ServeHTTP(w, r)
	if !delete {
		t.Error("routing DELETE failed")
	}

	r, _ = http.NewRequest(http.MethodGet, "/Handler", nil)
	router.ServeHTTP(w, r)
	if !handler {
		t.Error("routing Handler failed")
	}

	r, _ = http.NewRequest(http.MethodGet, "/HandlerFunc", nil)
	router.ServeHTTP(w, r)
	if !handlerFunc {
		t.Error("routing HandlerFunc failed")
	}
}

func TestRouterInvalidInput(t *testing.T) {
	router := New()

	handle := func(_ http.ResponseWriter, _ *http.Request) {}

	recv := catchPanic(func() {
		router.Handle("", "/", false, handle)
	})
	if recv == nil {
		t.Fatal("registering empty method did not panic")
	}

	recv = catchPanic(func() {
		router.GET("", false, handle)
	})
	if recv == nil {
		t.Fatal("registering empty path did not panic")
	}

	recv = catchPanic(func() {
		router.GET("noSlashRoot", false, handle)
	})
	if recv == nil {
		t.Fatal("registering path not beginning with '/' did not panic")
	}

	recv = catchPanic(func() {
		router.GET("/", false, nil)
	})
	if recv == nil {
		t.Fatal("registering nil handler did not panic")
	}
}

func TestRouterChaining(t *testing.T) {
	router1 := New()
	router2 := New()
	router1.NotFound = router2

	fooHit := false
	router1.POST("/foo", false, func(w http.ResponseWriter, req *http.Request) {
		fooHit = true
		w.WriteHeader(http.StatusOK)
	})

	barHit := false
	router2.POST("/bar", false, func(w http.ResponseWriter, req *http.Request) {
		barHit = true
		w.WriteHeader(http.StatusOK)
	})

	r, _ := http.NewRequest("POST", "/foo", nil)
	w := httptest.NewRecorder()
	router1.ServeHTTP(w, r)
	if !(w.Code == http.StatusOK && fooHit) {
		t.Errorf("Regular routing failed with router chaining. Code: %v.", w.Code)
		t.FailNow()
	}

	r, _ = http.NewRequest("POST", "/bar", nil)
	w = httptest.NewRecorder()
	router1.ServeHTTP(w, r)
	if !(w.Code == http.StatusOK && barHit) {
		t.Errorf("Chained routing failed with router chaining. Code: %v.", w.Code)
		t.FailNow()
	}

	r, _ = http.NewRequest("POST", "/qax", nil)
	w = httptest.NewRecorder()
	router1.ServeHTTP(w, r)
	if !(w.Code == http.StatusNotFound) {
		t.Errorf("NotFound behavior failed with router chaining.")
		t.FailNow()
	}
}

func BenchmarkAllowed(b *testing.B) {
	handlerFunc := func(_ http.ResponseWriter, _ *http.Request) {}

	router := New()
	router.POST("/path", false, handlerFunc)
	router.GET("/path", false, handlerFunc)

	b.Run("Global", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("*", http.MethodOptions)
		}
	})
	b.Run("Path", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("/path", http.MethodOptions)
		}
	})
}

func TestRouterOPTIONS(t *testing.T) {
	handlerFunc := func(_ http.ResponseWriter, _ *http.Request) {}

	router := New()

	router.POST("/path", false, handlerFunc)

	// test not allowed
	// * (server)
	r, _ := http.NewRequest(http.MethodOptions, "*", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusOK) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v", w.Code, w.Header())
	} else if allow := w.Header().Get("Allow"); allow != "OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// path
	r, _ = http.NewRequest(http.MethodOptions, "/path", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusOK) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v", w.Code, w.Header())
	} else if allow := w.Header().Get("Allow"); allow != "OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}

	r, _ = http.NewRequest(http.MethodOptions, "/doesnotexist", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusNotFound) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v", w.Code, w.Header())
	}

	// add another method
	router.GET("/path", false, handlerFunc)

	// set a global OPTIONS handler
	router.GlobalOPTIONS = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Adjust status code to 204
		w.WriteHeader(http.StatusNoContent)
	})

	// test again
	// * (server)
	r, _ = http.NewRequest(http.MethodOptions, "*", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusNoContent) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v", w.Code, w.Header())
	} else if allow := w.Header().Get("Allow"); allow != "POST, GET, OPTIONS" && allow != "GET, OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// path
	r, _ = http.NewRequest(http.MethodOptions, "/path", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusNoContent) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v", w.Code, w.Header())
	} else if allow := w.Header().Get("Allow"); allow != "GET, OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// custom handler
	var custom bool
	router.OPTIONS("/path", func(w http.ResponseWriter, r *http.Request) {
		custom = true
	})

	// test again
	// * (server)
	r, _ = http.NewRequest(http.MethodOptions, "*", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusNoContent) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v", w.Code, w.Header())
	} else if allow := w.Header().Get("Allow"); allow != "GET, OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}
	if custom {
		t.Error("custom handler called on *")
	}

	// path
	r, _ = http.NewRequest(http.MethodOptions, "/path", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusOK) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v", w.Code, w.Header())
	}
	if !custom {
		t.Error("custom handler not called")
	}
}

func TestRouterNotAllowed(t *testing.T) {
	handlerFunc := func(_ http.ResponseWriter, _ *http.Request) {}

	router := New()
	router.POST("/path", false, handlerFunc)

	// test not allowed
	r, _ := http.NewRequest("GET", "/path", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusMethodNotAllowed) {
		t.Errorf("NotAllowed handling failed: Code=%d, Header=%v", w.Code, w.Header())
	} else if allow := w.Header().Get("Allow"); allow != "OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// add another method
	router.DELETE("/path", handlerFunc)
	router.OPTIONS("/path", handlerFunc) // must be ignored

	// test again
	r, _ = http.NewRequest("GET", "/path", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusMethodNotAllowed) {
		t.Errorf("NotAllowed handling failed: Code=%d, Header=%v", w.Code, w.Header())
	} else if allow := w.Header().Get("Allow"); allow != "DELETE, OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// test custom handler
	w = httptest.NewRecorder()
	responseText := "custom method"
	router.MethodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte(responseText))
	})
	router.ServeHTTP(w, r)
	if got := w.Body.String(); !(got == responseText) {
		t.Errorf("unexpected response got %q want %q", got, responseText)
	}
	if w.Code != http.StatusTeapot {
		t.Errorf("unexpected response code %d want %d", w.Code, http.StatusTeapot)
	}
	if allow := w.Header().Get("Allow"); allow != "DELETE, OPTIONS, POST" {
		t.Error("unexpected Allow header value: " + allow)
	}
}

func filterMap(m map[string][]string) {
	delete(m, "Location")
}

func isMapEqual(l, r map[string][]string) bool {
	filterMap(l)
	filterMap(r)
	return reflect.DeepEqual(l, r)
}

func TestRouterNotFound(t *testing.T) {
	handlerFunc := func(_ http.ResponseWriter, _ *http.Request) {}

	router := New()
	router.GET("/path", false, handlerFunc)
	router.GET("/dir/", false, handlerFunc)
	router.GET("/", false, handlerFunc)

	testRoutes := []struct {
		route    string
		code     int
		location string
	}{
		{"/path/", http.StatusMovedPermanently, "/path"},   // TSR -/
		{"/dir", http.StatusMovedPermanently, "/dir/"},     // TSR +/
		{"", http.StatusMovedPermanently, "/"},             // TSR +/
		{"/PATH", http.StatusMovedPermanently, "/path"},    // Fixed Case
		{"/DIR/", http.StatusMovedPermanently, "/dir/"},    // Fixed Case
		{"/PATH/", http.StatusMovedPermanently, "/path"},   // Fixed Case -/
		{"/DIR", http.StatusMovedPermanently, "/dir/"},     // Fixed Case +/
		{"/../path", http.StatusMovedPermanently, "/path"}, // CleanPath
		{"/nope", http.StatusNotFound, ""},                 // NotFound
	}
	for _, tr := range testRoutes {
		r, _ := http.NewRequest(http.MethodGet, tr.route, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if !(w.Code == tr.code && (w.Code == http.StatusNotFound || fmt.Sprint(w.Header().Get("Location")) == tr.location)) {
			t.Errorf("NotFound handling route %s failed: Code=%d, Header=%v", tr.route, w.Code, w.Header().Get("Location"))
		}
	}

	// Test custom not found handler
	var notFound bool
	router.NotFound = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
		notFound = true
	})
	r, _ := http.NewRequest("GET", "/nope", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusNotFound && notFound == true) {
		t.Errorf("Custom NotFound handler failed: Code=%d, Header=%v", w.Code, w.Header())
	}

	// Test other method than GET (want 307 instead of 301)
	router.PATCH("/path", handlerFunc)
	r, _ = http.NewRequest(http.MethodPatch, "/path/", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusPermanentRedirect && fmt.Sprint(w.Header()) == "map[Location:[/path]]") {
		t.Errorf("Custom NotFound handler failed: Code=%d, Header=%v", w.Code, w.Header())
	}

	// Test special case where no node for the prefix "/" exists
	router = New()
	router.GET("/a", false, handlerFunc)
	r, _ = http.NewRequest(http.MethodGet, "/", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !(w.Code == http.StatusNotFound) {
		t.Errorf("NotFound handling route / failed: Code=%d", w.Code)
	}
}

func TestRouterPanicHandler(t *testing.T) {
	router := New()
	panicHandled := false

	router.PanicHandler = func(rw http.ResponseWriter, r *http.Request, p interface{}) {
		panicHandled = true
	}

	router.Handle("PUT", "/user/:name", false, func(_ http.ResponseWriter, _ *http.Request) {
		panic("oops!")
	})

	w := new(mockResponseWriter)
	req, _ := http.NewRequest(http.MethodPut, "/user/gopher", nil)

	defer func() {
		if rcv := recover(); rcv != nil {
			t.Fatal("handling panic failed")
		}
	}()

	router.ServeHTTP(w, req)

	if !panicHandled {
		t.Fatal("simulating failed")
	}
}

func TestRouterLookup(t *testing.T) {
	routed := false
	wantHandle := func(_ http.ResponseWriter, _ *http.Request) {
		routed = true
	}
	wantParams := Params{Param{"i18n", "false"}, Param{"name", "gopher"}}

	router := New()

	// try empty router first
	handle, _, tsr := router.Lookup(http.MethodGet, "/nope")
	if handle != nil {
		t.Fatalf("Got handle for unregistered pattern: %v", handle)
	}
	if tsr {
		t.Error("Got wrong TSR recommendation!")
	}

	// insert route and try again
	router.GET("/user/:name", false, wantHandle)

	handle, params, tsr := router.Lookup(http.MethodGet, "/user/gopher")
	if handle == nil {
		t.Fatal("Got no handle!")
	} else {
		handle(nil, nil)
		if !routed {
			t.Fatal("Routing failed!")
		}
	}
	if !reflect.DeepEqual(params, wantParams) {
		t.Fatalf("Wrong parameter values: want %v, got %v", wantParams, params)
	}
	routed = false

	// route without param
	router.GET("/user", false, wantHandle)
	handle, params, _ = router.Lookup(http.MethodGet, "/user")
	if handle == nil {
		t.Fatal("Got no handle!")
	} else {
		handle(nil, nil)
		if !routed {
			t.Fatal("Routing failed!")
		}
	}
	if params != nil {
		t.Fatalf("Wrong parameter values: want %v, got %v", nil, params)
	}

	handle, _, tsr = router.Lookup(http.MethodGet, "/user/gopher/")
	if handle != nil {
		t.Fatalf("Got handle for unregistered pattern: %v", handle)
	}
	if !tsr {
		t.Error("Got no TSR recommendation!")
	}

	handle, _, tsr = router.Lookup(http.MethodGet, "/nope")
	if handle != nil {
		t.Fatalf("Got handle for unregistered pattern: %v", handle)
	}
	if tsr {
		t.Error("Got wrong TSR recommendation!")
	}
}

func TestRouterParamsFromContext(t *testing.T) {
	routed := false

	var nilParams Params
	handlerFuncNil := func(_ http.ResponseWriter, req *http.Request) {
		// get params from request context
		params := ParamsFromContext(req.Context())

		if !reflect.DeepEqual(params, nilParams) {
			t.Fatalf("Wrong parameter values: want %v, got %v", nilParams, params)
		}

		routed = true
	}
	router := New()
	router.HandlerFunc(http.MethodGet, "/user", false, handlerFuncNil)
	router.HandlerFunc(http.MethodGet, "/user/:name", false, handlerFuncNil)

	w := new(mockResponseWriter)
	r, _ := http.NewRequest(http.MethodGet, "/user/gopher", nil)
	router.ServeHTTP(w, r)
	if !routed {
		t.Fatal("Routing failed!")
	}

	routed = false
	r, _ = http.NewRequest(http.MethodGet, "/user", nil)
	router.ServeHTTP(w, r)
	if !routed {
		t.Fatal("Routing failed!")
	}
}

func TestHandlerPaths(t *testing.T) {
	router := New()
	mfs := &mockFileSystem{}
	f := func(_ http.ResponseWriter, _ *http.Request) {}
	mp := map[string]map[string]struct{}{
		"PUT": {"/access/edit": struct{}{}},
		"GET": {
			"/access/edit": struct{}{},
			"/panel":       struct{}{},
			"/static":      struct{}{},
			"/blog":        struct{}{},
			"/files":       struct{}{},
			"/foo":         struct{}{},
		},
	}
	mpparams := map[string]map[string]struct{}{
		"PUT": {"/access/edit": struct{}{}},
		"GET": {
			"/access/edit/*params":  struct{}{},
			"/panel":                struct{}{},
			"/static/*filename":     struct{}{},
			"/blog/:category/:post": struct{}{},
			"/files/*filepath":      struct{}{},
			"/foo/:post":            struct{}{},
		},
	}

	router.PUT("/access/edit", false, f)
	router.GET("/access/edit/*params", false, f)
	router.GET("/panel", false, f)
	router.GET("/static/*filename", false, f)
	router.GET("/blog/:category/:post", false, f)
	router.GET("/foo/:post", false, f)
	router.ServeFiles("/files/*filepath", mfs)

	router.HandlerPaths(false, func(m, p string, h http.HandlerFunc) bool {
		paths, found := mp[m]
		if !found {
			t.Fatal("method no found")
		}
		_, found = paths[p]
		if !found {
			t.Fatal("path no found", p)
		}
		return true
	})
	router.HandlerPaths(true, func(m, p string, h http.HandlerFunc) bool {
		paths, found := mpparams[m]
		if !found {
			t.Fatal("method no found")
		}
		_, found = paths[p]
		if !found {
			t.Fatal("path no found", p)
		}
		return true
	})
}

func TestDeadlineContext(t *testing.T) {
	router := New()

	router.Context = func(in context.Context) (context.Context, context.CancelFunc) {
		newctx, cancel := context.WithDeadline(in, time.Now().Add(time.Millisecond*100))
		return newctx, cancel
	}

	routed := false
	router.Handle("GET", "/deadline", false, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond * 500)
		routed = true
	})

	w := new(mockResponseWriter)
	r, _ := http.NewRequest("GET", "/deadline", nil)
	router.ServeHTTP(w, r)
	if routed {
		t.Fatal("router didn't failed")
	}
}

func TestLangRedir(t *testing.T) {
	router := New()

	router.DefaultLang = "en"
	router.SupportedLangs = map[string]struct{}{
		"pt": struct{}{},
		"en": struct{}{},
	}

	worked := false
	router.Handle("GET", "/lang", true, func(w http.ResponseWriter, r *http.Request) {
		worked = true
	})

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/lang", nil)
	router.ServeHTTP(w, r)
	t.Log(w.Code)
	t.Log(w.Header())

	if w.Code != 301 {
		t.Fatalf("Wrong response http code: %v", w.Code)
	}

	loc := w.Header().Get("Location")
	t.Log(loc)
	w = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", loc, nil)
	router.ServeHTTP(w, r)
	t.Log(w.Code)
	t.Log(w.Header())

	if !worked {
		t.Fatal("router failed")
	}
}

func TestLangRedirHeader(t *testing.T) {
	router := New()

	router.DefaultLang = "en"
	router.SupportedLangs = map[string]struct{}{
		"pt": struct{}{},
		"en": struct{}{},
	}

	contentLang := ""
	router.Handle("GET", "/lang", true, func(w http.ResponseWriter, r *http.Request) {
		contentLang = ContentLang(r)
	})

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/lang", nil)
	r.Header.Add("Accept-Language", "pt")
	router.ServeHTTP(w, r)
	t.Log(w.Code)
	t.Log(w.Header())

	if w.Code != 301 {
		t.Fatalf("Wrong response http code: %v", w.Code)
	}

	loc := w.Header().Get("Location")
	w = httptest.NewRecorder()
	r, _ = http.NewRequest("GET", loc, nil)
	router.ServeHTTP(w, r)

	if contentLang != "pt" {
		t.Fatalf("router failed with content language equals to: %v", contentLang)
	}
}

func TestRoot(t *testing.T) {
	router := New()

	worked := false
	router.Handle("GET", "/", false, func(w http.ResponseWriter, r *http.Request) {
		worked = true
	})

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/", nil)
	router.ServeHTTP(w, r)
	t.Log(w.Code)
	t.Log(w.Header())

	if w.Code != 200 {
		t.Fatalf("Wrong response http code: %v", w.Code)
	}
	router.ServeHTTP(w, r)
	if !worked {
		t.Fatal("router failed")
	}
}

func TestRouterRoot(t *testing.T) {
	router := New()
	recv := catchPanic(func() {
		router.GET("noSlashRoot", false, nil)
	})
	if recv == nil {
		t.Fatal("registering path not beginning with '/' did not panic")
	}
}

type mockFileSystem struct {
	opened bool
}

func (mfs *mockFileSystem) Open(name string) (http.File, error) {
	mfs.opened = true
	return nil, errors.New("this is just a mock")
}

func TestRouterServeFiles(t *testing.T) {
	router := New()
	mfs := &mockFileSystem{}

	recv := catchPanic(func() {
		router.ServeFiles("/noFilepath", mfs)
	})
	if recv == nil {
		t.Fatal("registering path not ending with '*filepath' did not panic")
	}

	router.ServeFiles("/*filepath", mfs)
	w := new(mockResponseWriter)
	r, _ := http.NewRequest("GET", "/favicon.ico", nil)
	router.ServeHTTP(w, r)
	if !mfs.opened {
		t.Error("serving file failed")
	}
}

func TestPathExist(t *testing.T) {
	router := New()
	mfs := &mockFileSystem{}
	f := func(_ http.ResponseWriter, _ *http.Request) {}

	router.PUT("/access/edit", false, f)
	router.GET("/access/edit/*params", false, f)
	router.GET("/panel", false, f)
	router.GET("/static/*filename", false, f)
	router.GET("/blog/:category/:post", false, f)
	router.GET("/foo/:post", false, f)
	router.ServeFiles("/files/*filepath", mfs)

	if !router.PathExist("/access/edit") {
		t.Fatal("PathExist failed")
	}
	if !router.PathExist("/panel") {
		t.Fatal("PathExist failed")
	}
	if !router.PathExist("/static") {
		t.Fatal("PathExist failed")
	}
	if !router.PathExist("/blog") {
		t.Fatal("PathExist failed")
	}
	if !router.PathExist("/static") {
		t.Fatal("PathExist failed")
	}

	if router.PathExist("/blá") {
		t.Fatal("PathExist failed")
	}
	if router.PathExist("/access/edit/review") {
		t.Fatal("PathExist failed")
	}

}

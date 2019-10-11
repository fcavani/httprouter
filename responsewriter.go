// Copyright 2015 Felipe A. Cavani. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package httprouter

import (
	"bytes"
	"net/http"
	"github.com/fcavani/e"
)

// ResponseWriter implements the ResponseWriter interface
type ResponseWriter struct {
	header http.Header
	code   int
	buffer *bytes.Buffer
}

//NewResponseWriter creates a new ResponseWriter
func NewResponseWriter() *ResponseWriter {
	return &ResponseWriter{
		header: make(map[string][]string),
		buffer: bytes.NewBuffer([]byte{}),
	}
}

// Bytes resturn a slice with the current buffer.
func (rw *ResponseWriter) Bytes() []byte {
	return rw.buffer.Bytes()
}

// Len returns the length of the current buffer.
func (rw *ResponseWriter) Len() int {
	return rw.buffer.Len()
}

// Header return the map with the header data.
func (rw *ResponseWriter) Header() http.Header {
	return rw.header
}

// WriteHeader set the http response code.
func (rw *ResponseWriter) WriteHeader(code int) {
	rw.code = code
}

// ResponseCode return the actual response code.
func (rw *ResponseWriter) ResponseCode() int {
	return rw.code
}

// Write the response data.
func (rw *ResponseWriter) Write(buf []byte) (int, error) {
	return rw.buffer.Write(buf)
}

// Read reads the buffer to p slice and return the number of readen bytes our error.
func (rw *ResponseWriter) Read(p []byte) (int, error) {
	return rw.buffer.Read(p)
}

// Copy the data from the ResponseWriter struct to the
// ResponseWriter interface used in the http package.
func (rw *ResponseWriter) Copy(dst http.ResponseWriter) error {
	header := dst.Header()
	for k, v := range rw.header {
		for _, item := range v {
			header.Add(k, item)
		}
	}
	if rw.code != 0 {
		dst.WriteHeader(rw.code)
	}
	l := rw.buffer.Len()
	n, err := dst.Write(rw.buffer.Bytes())
	if err != nil {
		return e.Forward(err)
	}
	if n != l {
		return e.New("didn't wrote all data")
	}
	return nil
}

// Reset the buffer.
func (rw *ResponseWriter) Reset() {
	rw.code = 0
	rw.header = make(map[string][]string)
	rw.buffer = bytes.NewBuffer([]byte{})
}

package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func FuzzRouter_GetPartsInvariants(f *testing.F) {
	r := NewRouter()
	f.Add("/")
	f.Add("/a/b")
	f.Add("/static//js/app.js")
	f.Add("/static/")
	f.Add("")

	f.Fuzz(func(t *testing.T, path string) {
		segs, ok := r.getParts(path)
		if !ok {
			return
		}
		defer r.partsPool.Put(segs)

		if len(segs.indices) != len(segs.parts)+1 {
			t.Fatalf("indices len mismatch: parts=%d indices=%d", len(segs.parts), len(segs.indices))
		}
		if len(segs.indices) > 0 && segs.indices[len(segs.indices)-1] != len(path) {
			t.Fatalf("sentinel mismatch: got %d want %d", segs.indices[len(segs.indices)-1], len(path))
		}

		for i, part := range segs.parts {
			start := segs.indices[i]
			if i > 0 && start <= segs.indices[i-1] {
				t.Fatalf("indices not strictly increasing at %d", i)
			}
			if start < 0 || start+len(part) > len(path) {
				t.Fatalf("part index out of bounds at %d", i)
			}
			if part != path[start:start+len(part)] {
				t.Fatalf("part mismatch at %d", i)
			}
		}
	})
}

func FuzzRouter_ServeHTTP_NoPanic(f *testing.F) {
	r := NewRouter()
	if err := r.GET("/user/:id/files/*filepath", func(w http.ResponseWriter, req *http.Request) {
		id, _ := Param(w, "id")
		fp, _ := Param(w, "filepath")
		w.Write([]byte(id + ":" + fp))
	}); err != nil {
		panic(err)
	}
	if err := r.GET("/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		w.Write([]byte(fp))
	}); err != nil {
		panic(err)
	}
	if err := r.GET("/files/new", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("STATIC"))
	}); err != nil {
		panic(err)
	}

	f.Add("/user/42/files/photo.jpg")
	f.Add("/static/js/app.js")
	f.Add("/files/new")
	f.Add("/notfound")
	f.Add("/")

	f.Fuzz(func(t *testing.T, path string) {
		if path == "" {
			path = "/"
		}
		req, err := http.NewRequest("GET", path, nil)
		if err != nil {
			return
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 && w.Code != 404 && w.Code != 301 && w.Code != 308 {
			t.Fatalf("unexpected status code: %d", w.Code)
		}
	})
}

func FuzzRouter_FrozenParity(f *testing.F) {
	r := NewRouter()
	r.IgnoreCase = true
	r.StrictSlash = true

	if err := r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := Param(w, "id")
		w.Write([]byte("user:" + id))
	}); err != nil {
		panic(err)
	}
	if err := r.GET("/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		w.Write([]byte("static:" + fp))
	}); err != nil {
		panic(err)
	}
	if err := r.POST("/users", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}); err != nil {
		panic(err)
	}

	fr, err := r.Freeze()
	if err != nil {
		panic(err)
	}

	f.Add("/users/42", uint8(0))
	f.Add("/static/css/app.css", uint8(1))
	f.Add("/missing", uint8(2))
	f.Add("/users/42/", uint8(3))
	f.Add("/UsErS/AbC", uint8(4))

	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodHead,
		"PURGE",
	}

	f.Fuzz(func(t *testing.T, path string, methodSeed uint8) {
		if path == "" {
			path = "/"
		}
		if path[0] != '/' {
			path = "/" + path
		}
		if len(path) > MaxPathLength*2 {
			return
		}

		method := methods[int(methodSeed)%len(methods)]
		req1, err := http.NewRequest(method, path, nil)
		if err != nil {
			return
		}
		req2, err := http.NewRequest(method, path, nil)
		if err != nil {
			return
		}

		rec1 := httptest.NewRecorder()
		r.ServeHTTP(rec1, req1)

		rec2 := httptest.NewRecorder()
		fr.ServeHTTP(rec2, req2)

		if rec1.Code != rec2.Code {
			t.Fatalf("status mismatch: router=%d frozen=%d", rec1.Code, rec2.Code)
		}
		if rec1.Body.String() != rec2.Body.String() {
			t.Fatalf("body mismatch: router=%q frozen=%q", rec1.Body.String(), rec2.Body.String())
		}
		if rec1.Header().Get("Allow") != rec2.Header().Get("Allow") {
			t.Fatalf("allow header mismatch: router=%q frozen=%q", rec1.Header().Get("Allow"), rec2.Header().Get("Allow"))
		}
		if rec1.Header().Get("Location") != rec2.Header().Get("Location") {
			t.Fatalf("location mismatch: router=%q frozen=%q", rec1.Header().Get("Location"), rec2.Header().Get("Location"))
		}
	})
}

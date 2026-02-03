package middleware

import (
	"net/http"
	"os"
	"path"
	"strings"
)

const defaultIndexFile = "index.html"

// Static serves files from root when the request path matches prefix.
// For safety, directory listing is disabled; directories must contain index.html.
func Static(prefix, root string) func(http.Handler) http.Handler {
	prefix = normalizeStaticPrefix(prefix)
	fs := http.Dir(root)
	fileServer := http.FileServer(noDirListingFS{fs: fs, index: defaultIndexFile})
	if prefix != "/" {
		fileServer = http.StripPrefix(prefix, fileServer)
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			return nil
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			if !matchPathPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}
}

type noDirListingFS struct {
	fs    http.FileSystem
	index string
}

func (n noDirListingFS) Open(name string) (http.File, error) {
	name = path.Clean("/" + name)
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		indexPath := path.Join(name, n.index)
		index, err := n.fs.Open(indexPath)
		if err != nil {
			_ = f.Close()
			return nil, os.ErrNotExist
		}
		_ = index.Close()
	}
	return f, nil
}

func normalizeStaticPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimSuffix(prefix, "/")
}

func matchPathPrefix(p, prefix string) bool {
	if prefix == "/" {
		return true
	}
	if !strings.HasPrefix(p, prefix) {
		return false
	}
	if len(p) == len(prefix) {
		return true
	}
	return p[len(prefix)] == '/'
}

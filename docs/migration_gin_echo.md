# Migration From Gin/Echo (Minimal)

This guide shows the smallest practical migration steps to move existing handlers to Wand while keeping `net/http` semantics.

## 1) Route Registration

### Gin

```go
r := gin.New()
r.GET("/users/:id", func(c *gin.Context) {
	c.String(http.StatusOK, c.Param("id"))
})
```

### Echo

```go
e := echo.New()
e.GET("/users/:id", func(c echo.Context) error {
	return c.String(http.StatusOK, c.Param("id"))
})
```

### Wand

```go
r := router.NewRouter()
_ = r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request) {
	id, _ := router.Param(w, "id")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(id))
})
```

## 2) Middleware Porting

If you already have `net/http` middleware, it works directly in Wand.

```go
_ = r.Use(
	middleware.Recovery,
	middleware.RequestID,
	func(next http.Handler) http.Handler { return middleware.Timeout(5*time.Second, next) },
)
```

## 3) Route Groups

```go
api := r.Group("/api")
v1 := api.Group("/v1")

_ = v1.GET("/health", func(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
})
```

## 4) Host-based Routing

```go
apiHost := r.Host("api.example.com")
_ = apiHost.GET("/health", func(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
})
```

## 5) Migration Checklist

1. Replace framework-specific context usage with `http.ResponseWriter` and `*http.Request`.
2. Replace route-param access with `router.Param(w, "name")`.
3. Convert middleware to `func(http.Handler) http.Handler`.
4. Verify proxy normalization strategy (`URL.Path` vs `URL.RawPath`) and configure `UseRawPath` only if needed.
5. Run `go test ./...` and benchmark critical routes with `go test ./router -run=^$ -bench=. -benchmem`.

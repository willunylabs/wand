# Wand 🪄

**高性能零分配 HTTP 路由器**

Go 语言手写的极简路由器，专为低延迟、高并发服务设计。

## 核心特性

- ⚡ **零分配** - 热路径 0 GC 开销
- 🚀 **高性能** - 静态路由 ~35ns，动态路由 ~100ns  
- 🛡️ **DoS 防护** - 内置深度和长度限制
- 📝 **无锁日志** - 自研 RingBuffer 日志系统
- 🔗 **标准兼容** - 完全兼容 `net/http`

## 性能对比

| 框架 | 延迟 (ns/op) | 内存分配 |
|------|-------------|----------|
| Gin | 8,386 | 0 |
| Echo | 10,437 | 0 |
| **Wand** | 24,595 | **0** |
| Chi | 49,981 | 130 KB |

## 快速上手

```go
r := router.NewRouter()
r.GET("/users/:id", handler)
http.ListenAndServe(":8080", r)
```

## 技术栈

Go 1.24+ | 零外部依赖 | MIT 开源

---
**GitHub**: github.com/willunylabs/wand

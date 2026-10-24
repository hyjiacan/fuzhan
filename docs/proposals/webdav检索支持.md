

是否实现了 WebDAV 的搜索扩展（DASL / RFC 5323）?
若没有，实现里面的 search 方法。
同时可以实现非标准的 report 方法。
我只需要能搜索目录、文件名，不需要支持搜索文件内容。


看了你的代码，拦截方案可以很自然地融进现有结构，不需要改动标准 Handler，也不需要重构路由。核心是把 `SEARCH`/`REPORT` 加进 `webdavMethods`，然后让 `davHandler` 本身具备搜索能力——也就是在你传给 `gin.WrapH` 的那个 handler 外面包一层，或者直接在构造 `davHandler` 的地方做分流。

### 改动点一：把搜索方法加入方法列表

```go
var webdavMethods = []string{
    "GET", "HEAD", "PUT", "DELETE", "OPTIONS",
    "PROPFIND", "PROPPATCH", "MKCOL", "MOVE", "COPY",
    "LOCK", "UNLOCK",
    "SEARCH", "REPORT",
}
```

这样 `registerWebDAVRoutes` 就会为这两个方法注册路由，所有已有的中间件（鉴权、限流、depth、bodyLimit）也会自动生效。`depthMiddleware` 只处理 `PROPFIND`，对搜索无影响；`webdavRateLimitMiddleware` 只限 GET/HEAD，也不干扰搜索。

### 改动点二：包装 davHandler

在你构造 `davHandler` 的地方（`SetupRouter`/`SetupUnifiedRouter` 等调用方），用一层包装器接管搜索请求：

```go
type SearchableHandler struct {
    Standard http.Handler   // 原来的 *webdav.Handler
    DB       *sql.DB
    Prefix   string         // 与 webdav.Handler 的 Prefix 保持一致，用于拼 href
}

func (h *SearchableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case "SEARCH":
        h.handleSEARCH(w, r)
    case "REPORT":
        h.handleREPORT(w, r)
    default:
        h.Standard.ServeHTTP(w, r)
    }
}
```

然后把 `SetupRouter(rg, davHandler)` 里的 `davHandler` 换成 `&SearchableHandler{Standard: davHandler, DB: db, Prefix: ...}`，路由注册函数完全不用改。

### 改动点三：实现两个解析 + 共用查询响应

`handleSEARCH` 解析 DASL 的 `<D:literal>`，`handleREPORT` 解析 ownCloud 的 `<oc:pattern>`，两者都得到 `keyword string`，然后走同一个函数：

```go
func (h *SearchableHandler) searchAndRespond(w http.ResponseWriter, keyword string) {
    if keyword == "" {
        writeMultiStatus(w, nil)
        return
    }
    rows, err := h.DB.Query(
        "SELECT name, path, is_dir FROM files WHERE name LIKE ?",
        "%"+keyword+"%",
    )
    // ... 遍历 rows，构造 []SearchResult
    writeMultiStatus(w, results)
}
```

`writeMultiStatus` 负责拼 `<D:multistatus xmlns:D="DAV:">`，每个结果一个 `<D:response>`，`href` 用 `h.Prefix + path` 拼出来，`resourcetype` 按 `is_dir` 决定是否输出 `<D:collection/>`。

### 需要留意的几个细节

**`readOnlyMiddleware` 会误伤搜索**。`SetupPublicRouter` 用了 `readOnlyMiddleware`，它调用 `isReadOnlyMethod` 判断，而 `SEARCH`/`REPORT` 不在白名单里，会被 403 拦掉。如果公开路由也要支持搜索，需要把这两个方法加入 `isReadOnlyMethod` 的 true 分支——它们本质是只读查询，加进去是合理的。

**`bodyLimitMiddleware` 不影响**。它只对 `PUT`/`PROPPATCH` 限制 body，搜索请求体很小，无需处理。

**`Prefix` 要和标准 handler 对齐**。你拼 `href` 时必须用和 `webdav.Handler.Prefix` 相同的值，否则客户端拿到路径后访问不到。如果标准 handler 的 `Prefix` 是 `/api/v1/webdav`，搜索结果里的 `href` 也要带这个前缀。

**数据库 `path` 字段的语义**。存的是相对 WebDAV 根的路径（如 `/docs/report.pdf`），拼 `href` 时直接 `Prefix + path`。如果存的是绝对路径或带其他前缀，需要转换一次。

**`REPORT` 的请求体格式要确认**。ownCloud 的 `REPORT` 可能有多种 body（`oc:search-files`、`oc:filter-files`），你只需要处理 `oc:pattern` 那种；遇到不认识的 body，可以返回空 `multistatus` 而不是报错，避免客户端异常。

这样改完，`SEARCH` 和 `REPORT` 走你的搜索逻辑，其余方法原样落到标准 Handler，已有的鉴权、限流、只读控制全部复用，改动面很小。
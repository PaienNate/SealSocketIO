package socketws

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Config 配置类，能进行大量配置
type Config struct {
	Filter                          func(*gin.Context) bool
	HandshakeTimeout                time.Duration
	Subprotocols                    []string
	Origins                         []string
	ReadBufferSize, WriteBufferSize int
	WriteBufferPool                 websocket.BufferPool
	EnableCompression               bool
	RecoverHandler                  func(*Conn)
}

func defaultRecover(c *Conn) {
	if err := recover(); err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("panic: %v\n%s\n", err, debug.Stack()))
		if err := c.WriteJSON(map[string]interface{}{"error": err}); err != nil {
			_, _ = os.Stderr.WriteString(fmt.Sprintf("could not write error response: %v\n", err))
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func New(handler func(*Conn), config ...Config) gin.HandlerFunc {
	// 初始化配置对象
	var cfg Config
	if len(config) > 0 {
		cfg = config[0]
	}

	if len(cfg.Origins) == 0 {
		cfg.Origins = []string{"*"}
	}

	if cfg.ReadBufferSize == 0 {
		cfg.ReadBufferSize = 1024
	}

	if cfg.WriteBufferSize == 0 {
		cfg.WriteBufferSize = 1024
	}
	if cfg.RecoverHandler == nil {
		cfg.RecoverHandler = defaultRecover
	}
	upgrader = websocket.Upgrader{
		HandshakeTimeout:  cfg.HandshakeTimeout,  // 握手超时时间
		ReadBufferSize:    cfg.ReadBufferSize,    // 读缓冲区大小
		WriteBufferSize:   cfg.WriteBufferSize,   // 写缓冲区大小
		WriteBufferPool:   cfg.WriteBufferPool,   // 写缓冲池
		Subprotocols:      cfg.Subprotocols,      // 支持的子协议
		EnableCompression: cfg.EnableCompression, // 是否启用压缩
		CheckOrigin: func(r *http.Request) bool {
			// 检查请求的来源是否在允许的来源列表中
			if cfg.Origins[0] == "*" {
				return true // 如果允许所有来源，则直接返回 true
			}
			origin := r.Header.Get("Origin") // 获取请求的 Origin 头
			for i := range cfg.Origins {
				if cfg.Origins[i] == origin {
					return true // 如果找到匹配的来源，则返回 true
				}
			}
			return false // 如果没有找到匹配的来源，则返回 false
		},
	}

	// 返回 Gin 中间件处理函数
	return func(c *gin.Context) {
		// 应用过滤器，如果过滤器存在且返回 false，则跳过后续处理
		if cfg.Filter != nil && !cfg.Filter(c) {
			c.Next() // 继续处理下一个中间件
			return
		}

		// 获取一个新的连接对象
		conn := acquireConn()

		// 设置本地变量（使用 Gin 的 Keys）
		for k, v := range c.Keys {
			conn.locals[k] = v // 将 Gin 上下文中的本地变量复制到连接对象中
		}

		// 设置路由参数
		for _, param := range c.Params {
			conn.params[param.Key] = param.Value // 将 Gin 上下文中的路由参数复制到连接对象中
		}

		// 设置查询参数
		for k, v := range c.Request.URL.Query() {
			if len(v) > 0 {
				conn.queries[k] = v[0] // 将查询参数复制到连接对象中
			}
		}

		// 设置 Cookie
		for _, cookie := range c.Request.Cookies() {
			conn.cookies[cookie.Name] = cookie.Value // 将 Cookie 复制到连接对象中
		}

		// 设置请求头
		for k, v := range c.Request.Header {
			if len(v) > 0 {
				conn.headers[k] = v[0] // 将请求头复制到连接对象中
			}
		}

		// 设置客户端 IP 地址
		conn.ip = c.ClientIP() // 获取客户端的 IP 地址并设置到连接对象中

		// 升级 HTTP 连接为 WebSocket 连接
		wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.AbortWithStatus(426) // 如果升级失败，返回 426 升级所需状态码
			return
		}

		// 设置连接对象的 WebSocket 连接
		conn.Conn = wsConn
		defer releaseConn(conn)        // 在处理完成后释放连接对象
		defer cfg.RecoverHandler(conn) // 在处理完成后调用恢复处理器

		// 调用处理函数，处理新的 WebSocket 连接
		handler(conn)
	}
}

// Conn 是对 gorilla/websocket.Conn 的封装
// 该结构体在标准的 websocket.Conn 结构体基础上增加了额外的功能和信息。
type Conn struct {
	// 嵌入的 gorilla/websocket.Conn，以便直接访问其方法和属性。
	*websocket.Conn

	// locals 存储与该连接关联的本地变量。
	locals map[string]interface{}

	// params 存储与该连接关联的 URL 参数。
	params map[string]string

	// cookies 存储该连接的 Cookie 信息。
	cookies map[string]string

	// headers 存储该连接的 HTTP 头信息。
	headers map[string]string

	// queries 存储该连接的查询参数信息。
	queries map[string]string

	// ip 存储该连接的客户端 IP 地址信息。
	ip string
}

// Conn pool
var poolConn = sync.Pool{
	New: func() interface{} {
		return new(Conn)
	},
}

// Acquire Conn from pool
func acquireConn() *Conn {
	conn := poolConn.Get().(*Conn)
	conn.locals = make(map[string]interface{})
	conn.params = make(map[string]string)
	conn.queries = make(map[string]string)
	conn.cookies = make(map[string]string)
	conn.headers = make(map[string]string)
	return conn
}

// Return Conn to pool
func releaseConn(conn *Conn) {
	conn.Conn = nil
	poolConn.Put(conn)
}

// Locals makes it possible to pass interface{} values under string keys scoped to the request
// and therefore available to all following routes that match the request.
func (conn *Conn) Locals(key string, value ...interface{}) interface{} {
	if len(value) == 0 {
		return conn.locals[key]
	}
	conn.locals[key] = value[0]
	return value[0]
}

// Params is used to get the route parameters.
// Defaults to empty string "" if the param doesn't exist.
// If a default value is given, it will return that value if the param doesn't exist.
func (conn *Conn) Params(key string, defaultValue ...string) string {
	v, ok := conn.params[key]
	if !ok && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return v
}

// Query returns the query string parameter in the url.
// Defaults to empty string "" if the query doesn't exist.
// If a default value is given, it will return that value if the query doesn't exist.
func (conn *Conn) Query(key string, defaultValue ...string) string {
	v, ok := conn.queries[key]
	if !ok && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return v
}

// Cookies is used for getting a cookie value by key
// Defaults to empty string "" if the cookie doesn't exist.
// If a default value is given, it will return that value if the cookie doesn't exist.
func (conn *Conn) Cookies(key string, defaultValue ...string) string {
	v, ok := conn.cookies[key]
	if !ok && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return v
}

// Headers is used for getting a header value by key
// Defaults to empty string "" if the header doesn't exist.
// If a default value is given, it will return that value if the header doesn't exist.
func (conn *Conn) Headers(key string, defaultValue ...string) string {
	v, ok := conn.headers[key]
	if !ok && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return v
}

// IP returns the client's network address
func (conn *Conn) IP() string {
	return conn.ip
}

// IsWebSocketUpgrade returns true if the client requested upgrade to the
// WebSocket protocol.
func IsWebSocketUpgrade(c *gin.Context) bool {
	return websocket.IsWebSocketUpgrade(c.Request)
}

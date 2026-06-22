package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"guiforcores/bridge"
	"guiforcores/bridge/runtime"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

//go:embed all:frontend/dist
var assets embed.FS

var (
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

func main() {
	var (
		portFlag       string
		unixSocketFlag string
		baseUrlFlag    string
		workPathFlag   string
	)

	flag.StringVar(&portFlag, "port", "", "Web服务端口")
	flag.StringVar(&unixSocketFlag, "unix-socket", "", "Unix Socket 文件路径")
	flag.StringVar(&baseUrlFlag, "baseurl", "", "过滤URL前缀")
	flag.StringVar(&workPathFlag, "workpath", "", "自定义工作路径")
	flag.Parse()

	// 初始化 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 设置路由处理器并应用 BaseURL 路径过滤
	var handler http.Handler = r
	if baseUrlFlag != "" {
		if !strings.HasPrefix(baseUrlFlag, "/") {
			baseUrlFlag = "/" + baseUrlFlag
		}
		baseUrlFlag = strings.TrimSuffix(baseUrlFlag, "/")
		handler = &pathPrefixStripper{
			prefix:  baseUrlFlag,
			handler: r,
		}
	}

	// Initialize Bridge App
	app := bridge.CreateApp(assets, workPathFlag)
	app.Ctx = context.Background()

	// 监听终止信号以优雅清理 mihomo 进程
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("[App] Received signal: %v, cleaning up...", sig)

		mihomoPidPath := filepath.Join(bridge.Env.BasePath, "data/mihomo/pid.txt")
		if pidBytes, err := os.ReadFile(mihomoPidPath); err == nil {
			pidStr := strings.TrimSpace(string(pidBytes))
			if pid, err := strconv.Atoi(pidStr); err == nil {
				if proc, err := os.FindProcess(pid); err == nil {
					_ = proc.Kill()
					log.Printf("[App] Successfully killed mihomo process (PID: %d)", pid)
				}
			}
		}

		os.Exit(0)
	}()

	// 初始化定时任务管理器
	bridge.InitTaskManager(app)

	// Handle shim runtime OnEmit -> Broadcast to all websocket clients
	runtime.OnEmit = func(event string, data ...any) {
		broadcastWS(event, data)
	}

	runtime.OnQuit = func() {
		log.Println("[App] Exit requested")
		os.Exit(0)
	}

	// 1. Static Files Web Hosting
	fe, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatalf("Failed to load embedded frontend files: %v", err)
	}

	// Serve Static Files with SPA Fallback
	staticFS := http.FS(fe)
	fileServer := http.FileServer(staticFS)
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
			return
		}

		// If resource does not exist in embed, fallback to index.html
		f, err := fe.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		f.Close()

		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	// 2. WebSocket events stream
	r.GET("/ws/events", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("WS Upgrade error: %v", err)
			return
		}

		clientsMu.Lock()
		clients[conn] = true
		clientsMu.Unlock()

		defer func() {
			clientsMu.Lock()
			delete(clients, conn)
			clientsMu.Unlock()
			conn.Close()
		}()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			// Handle EventsEmit from client
			var clientEvent struct {
				Event string `json:"event"`
				Data  []any  `json:"data"`
			}
			if err := json.Unmarshal(message, &clientEvent); err == nil {
				runtime.EventsEmit(app.Ctx, clientEvent.Event, clientEvent.Data...)
			}
		}
	})

	// 2.5 WebSocket Reverse Proxy to Mihomo
	r.GET("/ws/kernel/*any", func(c *gin.Context) {
		path := c.Param("any")
		rawQuery := c.Request.URL.RawQuery
		targetWSUrl := fmt.Sprintf("ws://127.0.0.1:20113%s", path)
		if rawQuery != "" {
			targetWSUrl = fmt.Sprintf("%s?%s", targetWSUrl, rawQuery)
		}

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		clientConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[WS Proxy] Upgrade error: %v", err)
			return
		}
		defer clientConn.Close()

		dialer := websocket.DefaultDialer
		backendConn, _, err := dialer.Dial(targetWSUrl, nil)
		if err != nil {
			log.Printf("[WS Proxy] Dial backend error: %v", err)
			return
		}
		defer backendConn.Close()

		errChan := make(chan error, 2)
		go func() {
			for {
				messageType, message, err := clientConn.ReadMessage()
				if err != nil {
					errChan <- err
					return
				}
				err = backendConn.WriteMessage(messageType, message)
				if err != nil {
					errChan <- err
					return
				}
			}
		}()

		go func() {
			for {
				messageType, message, err := backendConn.ReadMessage()
				if err != nil {
					errChan <- err
					return
				}
				err = clientConn.WriteMessage(messageType, message)
				if err != nil {
					errChan <- err
					return
				}
			}
		}()

		<-errChan
	})

	// 3. API Routes (Directly Exposed)
	api := r.Group("/api")
	{
		// Kernel HTTP APIs Proxy
		api.Any("/kernel/*any", func(c *gin.Context) {
			path := c.Param("any")
			targetUrl, err := url.Parse("http://127.0.0.1:20113")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			proxy := httputil.NewSingleHostReverseProxy(targetUrl)
			c.Request.URL.Path = path
			proxy.ServeHTTP(c.Writer, c.Request)
		})

		// IO APIs
		api.POST("/io/write", func(c *gin.Context) {
			var req struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				Options struct {
					Mode  string `json:"Mode"`
					Range string `json:"Range"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			res := app.WriteFile(req.Path, req.Content, bridge.IOOptions{
				Mode:  req.Options.Mode,
				Range: req.Options.Range,
			})
			c.JSON(http.StatusOK, res)
		})

		api.POST("/io/read", func(c *gin.Context) {
			var req struct {
				Path    string `json:"path"`
				Options struct {
					Mode  string `json:"Mode"`
					Range string `json:"Range"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			res := app.ReadFile(req.Path, bridge.IOOptions{
				Mode:  req.Options.Mode,
				Range: req.Options.Range,
			})
			c.JSON(http.StatusOK, res)
		})

		api.POST("/io/move", func(c *gin.Context) {
			var req struct {
				Source string `json:"source"`
				Target string `json:"target"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.MoveFile(req.Source, req.Target))
		})

		api.POST("/io/remove", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.RemoveFile(req.Path))
		})

		api.POST("/io/copy", func(c *gin.Context) {
			var req struct {
				Source string `json:"source"`
				Target string `json:"target"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.CopyFile(req.Source, req.Target))
		})

		api.POST("/io/exists", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.FileExists(req.Path))
		})

		api.POST("/io/absolute", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.AbsolutePath(req.Path))
		})

		api.POST("/io/mkdir", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.MakeDir(req.Path))
		})

		api.POST("/io/readdir", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.ReadDir(req.Path))
		})

		api.POST("/io/opendir", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.OpenDir(req.Path))
		})

		api.POST("/io/openuri", func(c *gin.Context) {
			var req struct {
				Uri string `json:"uri"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.OpenURI(req.Uri))
		})

		api.POST("/io/unzip", func(c *gin.Context) {
			var req struct {
				Path   string `json:"path"`
				Output string `json:"output"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.UnzipZIPFile(req.Path, req.Output))
		})

		api.POST("/io/unzipgz", func(c *gin.Context) {
			var req struct {
				Path   string `json:"path"`
				Output string `json:"output"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.UnzipGZFile(req.Path, req.Output))
		})

		api.POST("/io/unziptargz", func(c *gin.Context) {
			var req struct {
				Path   string `json:"path"`
				Output string `json:"output"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.UnzipTarGZFile(req.Path, req.Output))
		})

		// Exec APIs
		api.POST("/exec/run", func(c *gin.Context) {
			var req struct {
				Path    string   `json:"path"`
				Args    []string `json:"args"`
				Options struct {
					WorkingDirectory string            `json:"WorkingDirectory"`
					Convert          bool              `json:"Convert"`
					Env              map[string]string `json:"Env"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.Exec(req.Path, req.Args, bridge.ExecOptions{
				WorkingDirectory: req.Options.WorkingDirectory,
				Convert:          req.Options.Convert,
				Env:              req.Options.Env,
			}))
		})

		api.POST("/exec/run-bg", func(c *gin.Context) {
			var req struct {
				Path     string   `json:"path"`
				Args     []string `json:"args"`
				OutEvent string   `json:"outEvent"`
				EndEvent string   `json:"endEvent"`
				Options  struct {
					WorkingDirectory  string            `json:"WorkingDirectory"`
					PidFile           string            `json:"PidFile"`
					LogFile           string            `json:"LogFile"`
					Convert           bool              `json:"Convert"`
					StopOutputKeyword string            `json:"StopOutputKeyword"`
					Env               map[string]string `json:"Env"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.ExecBackground(req.Path, req.Args, req.OutEvent, req.EndEvent, bridge.ExecOptions{
				WorkingDirectory:  req.Options.WorkingDirectory,
				PidFile:           req.Options.PidFile,
				LogFile:           req.Options.LogFile,
				Convert:           req.Options.Convert,
				StopOutputKeyword: req.Options.StopOutputKeyword,
				Env:               req.Options.Env,
			}))
		})

		api.POST("/exec/probe", func(c *gin.Context) {
			var req struct {
				Url    string `json:"url"`
				Secret string `json:"secret"`
			}
			_ = c.ShouldBindJSON(&req)

			client := http.Client{
				Timeout: 1 * time.Second,
			}
			httpReq, err := http.NewRequest("GET", req.Url, nil)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{"flag": false, "data": err.Error()})
				return
			}
			if req.Secret != "" {
				httpReq.Header.Set("Authorization", "Bearer "+req.Secret)
			}
			resp, err := client.Do(httpReq)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{"flag": false, "data": err.Error()})
				return
			}
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				c.JSON(http.StatusOK, gin.H{"flag": true, "data": "Success"})
			} else {
				c.JSON(http.StatusOK, gin.H{"flag": false, "data": fmt.Sprintf("HTTP Status: %d", resp.StatusCode)})
			}
		})

		api.POST("/exec/info", func(c *gin.Context) {
			var req struct {
				Pid int32 `json:"pid"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.ProcessInfo(req.Pid))
		})

		api.POST("/exec/memory", func(c *gin.Context) {
			var req struct {
				Pid int32 `json:"pid"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.ProcessMemory(req.Pid))
		})

		api.POST("/exec/kill", func(c *gin.Context) {
			var req struct {
				Pid     int `json:"pid"`
				Timeout int `json:"timeout"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.KillProcess(req.Pid, req.Timeout))
		})

		// Net APIs
		api.POST("/net/request", func(c *gin.Context) {
			var req struct {
				Method  string            `json:"method"`
				Url     string            `json:"url"`
				Headers map[string]string `json:"headers"`
				Body    string            `json:"body"`
				Options struct {
					Proxy     string `json:"Proxy"`
					Insecure  bool   `json:"Insecure"`
					Redirect  bool   `json:"Redirect"`
					Timeout   int    `json:"Timeout"`
					CancelId  string `json:"CancelId"`
					FileField string `json:"FileField"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.Requests(req.Method, req.Url, req.Headers, req.Body, bridge.RequestOptions{
				Proxy:     req.Options.Proxy,
				Insecure:  req.Options.Insecure,
				Redirect:  req.Options.Redirect,
				Timeout:   req.Options.Timeout,
				CancelId:  req.Options.CancelId,
				FileField: req.Options.FileField,
			}))
		})

		api.POST("/net/download", func(c *gin.Context) {
			var req struct {
				Method  string            `json:"method"`
				Url     string            `json:"url"`
				Path    string            `json:"path"`
				Headers map[string]string `json:"headers"`
				Event   string            `json:"event"`
				Options struct {
					Proxy     string `json:"Proxy"`
					Insecure  bool   `json:"Insecure"`
					Redirect  bool   `json:"Redirect"`
					Timeout   int    `json:"Timeout"`
					CancelId  string `json:"CancelId"`
					FileField string `json:"FileField"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.Download(req.Method, req.Url, req.Path, req.Headers, req.Event, bridge.RequestOptions{
				Proxy:     req.Options.Proxy,
				Insecure:  req.Options.Insecure,
				Redirect:  req.Options.Redirect,
				Timeout:   req.Options.Timeout,
				CancelId:  req.Options.CancelId,
				FileField: req.Options.FileField,
			}))
		})

		api.POST("/net/upload", func(c *gin.Context) {
			var req struct {
				Method  string            `json:"method"`
				Url     string            `json:"url"`
				Path    string            `json:"path"`
				Headers map[string]string `json:"headers"`
				Event   string            `json:"event"`
				Options struct {
					Proxy     string `json:"Proxy"`
					Insecure  bool   `json:"Insecure"`
					Redirect  bool   `json:"Redirect"`
					Timeout   int    `json:"Timeout"`
					CancelId  string `json:"CancelId"`
					FileField string `json:"FileField"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.Upload(req.Method, req.Url, req.Path, req.Headers, req.Event, bridge.RequestOptions{
				Proxy:     req.Options.Proxy,
				Insecure:  req.Options.Insecure,
				Redirect:  req.Options.Redirect,
				Timeout:   req.Options.Timeout,
				CancelId:  req.Options.CancelId,
				FileField: req.Options.FileField,
			}))
		})

		api.POST("/net/ping", func(c *gin.Context) {
			var req struct {
				Address string `json:"address"`
				Options struct {
					Mode    string `json:"Mode"`
					Timeout int    `json:"Timeout"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.TcpPing(req.Address, bridge.NetOptions{
				Mode:    req.Options.Mode,
				Timeout: req.Options.Timeout,
			}))
		})

		api.POST("/net/tcprequest", func(c *gin.Context) {
			var req struct {
				Address string `json:"address"`
				Payload string `json:"payload"`
				Options struct {
					Mode    string `json:"Mode"`
					Timeout int    `json:"Timeout"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.TcpRequest(req.Address, req.Payload, bridge.NetOptions{
				Mode:    req.Options.Mode,
				Timeout: req.Options.Timeout,
			}))
		})

		api.POST("/net/udprequest", func(c *gin.Context) {
			var req struct {
				Address string `json:"address"`
				Payload string `json:"payload"`
				Options struct {
					Mode    string `json:"Mode"`
					Timeout int    `json:"Timeout"`
				} `json:"options"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.UdpRequest(req.Address, req.Payload, bridge.NetOptions{
				Mode:    req.Options.Mode,
				Timeout: req.Options.Timeout,
			}))
		})

		// App APIs
		api.POST("/app/restart", func(c *gin.Context) {
			c.JSON(http.StatusOK, app.RestartApp())
		})

		api.POST("/app/exit", func(c *gin.Context) {
			log.Println("[App] Exit request received, ignored on Web server mode")
			c.JSON(http.StatusOK, gin.H{"flag": false, "data": "Exit app is disabled on Web mode"})
		})

		api.POST("/app/env", func(c *gin.Context) {
			var req struct {
				Key string `json:"key"`
			}
			_ = c.ShouldBindJSON(&req)
			envVal := app.GetEnv(req.Key)
			c.JSON(http.StatusOK, gin.H{"flag": true, "data": envVal})
		})

		api.POST("/app/isstartup", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"flag": true, "data": fmt.Sprintf("%v", app.IsStartup())})
		})

		api.POST("/app/interfaces", func(c *gin.Context) {
			c.JSON(http.StatusOK, app.GetInterfaces())
		})

		// MMDB APIs
		api.POST("/mmdb/open", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
				Id   string `json:"id"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.OpenMMDB(req.Path, req.Id))
		})

		api.POST("/mmdb/close", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
				Id   string `json:"id"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.CloseMMDB(req.Path, req.Id))
		})

		api.POST("/mmdb/query", func(c *gin.Context) {
			var req struct {
				Path string `json:"path"`
				Ip   string `json:"ip"`
				Type string `json:"type"`
			}
			_ = c.ShouldBindJSON(&req)
			c.JSON(http.StatusOK, app.QueryMMDB(req.Path, req.Ip, req.Type))
		})

		// Task APIs
		api.POST("/task/reload", func(c *gin.Context) {
			mgr := bridge.GetTaskManager()
			if mgr != nil {
				if err := mgr.ReloadTasks(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"flag": false, "data": err.Error()})
					return
				}
			}
			c.JSON(http.StatusOK, gin.H{"flag": true, "data": "Success"})
		})

		api.POST("/task/run", func(c *gin.Context) {
			var req struct {
				ID string `json:"id"`
			}
			_ = c.ShouldBindJSON(&req)
			mgr := bridge.GetTaskManager()
			if mgr != nil {
				go mgr.RunTask(req.ID)
			}
			c.JSON(http.StatusOK, gin.H{"flag": true, "data": "Success"})
		})

	}

	// 启动服务
	if unixSocketFlag != "" {
		log.Printf("[Server] Starting panel at unix-socket: %s", unixSocketFlag)
		if err := os.Remove(unixSocketFlag); err != nil && !os.IsNotExist(err) {
			log.Printf("[Server] Failed to remove existing socket file: %v", err)
		}
		listener, err := net.Listen("unix", unixSocketFlag)
		if err != nil {
			log.Fatalf("Failed to listen on unix socket: %v", err)
		}
		defer func() {
			listener.Close()
			_ = os.Remove(unixSocketFlag)
		}()
		oldOnQuit := runtime.OnQuit
		runtime.OnQuit = func() {
			log.Println("[App] Exit requested, cleaning up socket file...")
			_ = os.Remove(unixSocketFlag)
			if oldOnQuit != nil {
				oldOnQuit()
			} else {
				os.Exit(0)
			}
		}
		if err := http.Serve(listener, handler); err != nil {
			log.Fatalf("Server failed to run: %v", err)
		}
	} else {
		addr := portFlag
		if addr == "" {
			addr = os.Getenv("PORT")
		}
		if addr == "" {
			addr = "8080"
		}
		if !strings.Contains(addr, ":") {
			addr = "0.0.0.0:" + addr
		}
		log.Printf("[Server] Starting panel at http://%s", addr)
		if err := http.ListenAndServe(addr, handler); err != nil {
			log.Fatalf("Server failed to run: %v", err)
		}
	}
}

// broadcastWS sends event payload to all active websocket connections
func broadcastWS(event string, data []any) {
	msg := struct {
		Event string `json:"event"`
		Data  []any  `json:"data"`
	}{
		Event: event,
		Data:  data,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}

	clientsMu.Lock()
	defer clientsMu.Unlock()
	for client := range clients {
		_ = client.WriteMessage(websocket.TextMessage, payload)
	}
}

// pathPrefixStripper 剥离请求的 BaseURL 前缀
type pathPrefixStripper struct {
	prefix  string
	handler http.Handler
}

func (s *pathPrefixStripper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, s.prefix) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, s.prefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		if r.URL.RawPath != "" && strings.HasPrefix(r.URL.RawPath, s.prefix) {
			r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, s.prefix)
			if r.URL.RawPath == "" {
				r.URL.RawPath = "/"
			}
		}
	}
	s.handler.ServeHTTP(w, r)
}

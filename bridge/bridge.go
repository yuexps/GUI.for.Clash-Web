package bridge

import (
	"embed"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	sysruntime "runtime"

	"guiforcores/bridge/runtime"
	"gopkg.in/yaml.v3"
)

var Config = &AppConfig{}

var Env = &EnvResult{
	IsStartup:    true,
	PreventExit:  true,
	FromTaskSch:  false,
	WebviewPath:  "",
	AppName:      "",
	AppVersion:   "v1.25.1",
	BasePath:     "",
	OS:           sysruntime.GOOS,
	ARCH:         sysruntime.GOARCH,
	IsPrivileged: false,
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

func CreateApp(fs embed.FS, customWorkPath string) *App {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}

	if customWorkPath != "" {
		absPath, err := filepath.Abs(customWorkPath)
		if err == nil {
			Env.BasePath = filepath.ToSlash(absPath)
		} else {
			Env.BasePath = filepath.ToSlash(customWorkPath)
		}
	} else {
		Env.BasePath = filepath.ToSlash(filepath.Dir(exePath))
	}
	Env.AppName = filepath.Base(exePath)

	if priv, err := IsPrivileged(); err == nil {
		Env.IsPrivileged = priv
	}

	app := NewApp()

	loadConfig()

	return app
}

func (a *App) IsStartup() bool {
	if Env.IsStartup {
		Env.IsStartup = false
		return true
	}
	return false
}

func (a *App) ExitApp() {
	log.Printf("ExitApp")
	Env.PreventExit = false
	runtime.Quit(a.Ctx)
}

func (a *App) RestartApp() FlagResult {
	log.Printf("RestartApp")
	exePath := resolvePath(Env.AppName)

	cmd := exec.Command(exePath)
	SetCmdWindowHidden(cmd)

	if err := cmd.Start(); err != nil {
		return FlagResult{false, err.Error()}
	}

	a.ExitApp()

	return FlagResult{true, "Success"}
}

func (a *App) GetEnv(key string) any {
	log.Printf("GetEnv: %s", key)
	if key != "" {
		return os.Getenv(key)
	}
	return EnvResult{
		AppName:      Env.AppName,
		AppVersion:   Env.AppVersion,
		BasePath:     Env.BasePath,
		OS:           Env.OS,
		ARCH:         Env.ARCH,
		IsPrivileged: Env.IsPrivileged,
	}
}

func (a *App) GetInterfaces() FlagResult {
	log.Printf("GetInterfaces")

	interfaces, err := net.Interfaces()
	if err != nil {
		return FlagResult{false, err.Error()}
	}

	var interfaceNames []string

	for _, inter := range interfaces {
		interfaceNames = append(interfaceNames, inter.Name)
	}

	return FlagResult{true, strings.Join(interfaceNames, "|")}
}

func (a *App) ShowMainWindow() {
	log.Printf("ShowMainWindow")
	runtime.WindowShow(a.Ctx)
}

func loadConfig() {
	b, err := os.ReadFile(resolvePath("data/user.yaml"))
	if err == nil {
		if err := yaml.Unmarshal(b, &Config); err != nil {
			log.Printf("Failed to parse user config: %v", err)
		}
	}
}

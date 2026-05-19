package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows/registry"
)

// ---------- 配置结构 ----------
type Config struct {
	FileBrowserExe  string `json:"filebrowser_exe"`
	FileBrowserArgs string `json:"filebrowser_args"`
	LogFile         string `json:"log_file"`
}

var defaultConfig = Config{
	FileBrowserExe:  "filebrowser.exe",
	FileBrowserArgs: "-a 0.0.0.0 -p 8080",
	LogFile:         "filebrowser.log",
}

const (
	configFile        = "config.json"
	registryRunKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	registryValueName = "FileBrowserGuardian"
)

//go:embed icon.ico
var iconData []byte

var (
	cmdMutex  sync.Mutex
	cmd       *exec.Cmd
	isRunning bool

	currentConfig Config

	mStatus    *systray.MenuItem
	mStartStop *systray.MenuItem
	mRestart   *systray.MenuItem
	mReload    *systray.MenuItem
	mEditConf  *systray.MenuItem
	mOpenPage  *systray.MenuItem
	mAutoStart *systray.MenuItem
)

func main() {
	loadConfig()
	go autoStartService()
	systray.Run(onReady, onExit)
}

// ---------- 配置管理 ----------
func loadConfig() {
	cfgPath := resolvePath(configFile)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		saveConfig(defaultConfig)
		currentConfig = defaultConfig
		return
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		currentConfig = defaultConfig
		return
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		currentConfig = defaultConfig
		return
	}

	if cfg.FileBrowserExe == "" {
		cfg.FileBrowserExe = defaultConfig.FileBrowserExe
	}
	if cfg.FileBrowserArgs == "" {
		cfg.FileBrowserArgs = defaultConfig.FileBrowserArgs
	}
	if cfg.LogFile == "" {
		cfg.LogFile = defaultConfig.LogFile
	}

	currentConfig = cfg
}

func saveConfig(cfg Config) {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	cfgPath := resolvePath(configFile)
	os.WriteFile(cfgPath, data, 0644)
}

func reloadConfigAndRestart() {
	loadConfig()
	restartFileBrowser()
}

// ---------- 地址解析 ----------
func getWebURL() string {
	args := currentConfig.FileBrowserArgs
	ip := "localhost"
	port := "8080"

	// 提取 -a 参数
	aRe := regexp.MustCompile(`-a\s+(\S+)`)
	if match := aRe.FindStringSubmatch(args); len(match) == 2 {
		ip = match[1]
		if ip == "0.0.0.0" {
			ip = "localhost"
		}
	}

	// 提取 -p 参数
	pRe := regexp.MustCompile(`-p\s+(\S+)`)
	if match := pRe.FindStringSubmatch(args); len(match) == 2 {
		port = match[1]
	}

	return "http://" + ip + ":" + port
}

// ---------- 服务控制 ----------
func autoStartService() {
	time.Sleep(500 * time.Millisecond)
	startFileBrowser()
}

func startFileBrowser() bool {
	cmdMutex.Lock()
	defer cmdMutex.Unlock()

	if isRunning {
		return true
	}

	exePath := resolvePath(currentConfig.FileBrowserExe)
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		updateStatus("文件未找到: " + exePath)
		return false
	}

	logPath := resolvePath(currentConfig.LogFile)
	logWriter, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		updateStatus("日志文件打开失败")
		return false
	}

	cmd = exec.Command(exePath, strings.Split(currentConfig.FileBrowserArgs, " ")...)
	// 在可执行文件所在目录启动子进程，确保相对路径和工作目录行为与 filebrowser.exe 本身一致
	cmd.Dir = filepath.Dir(exePath)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		updateStatus("启动失败: " + err.Error())
		return false
	}

	isRunning = true
	updateStatus("服务运行中")
	refreshMenuTitles()

	go func() {
		err := cmd.Wait()
		logWriter.Close()
		cmdMutex.Lock()
		isRunning = false
		cmdMutex.Unlock()
		if err != nil {
			logToFile("filebrowser 退出: " + err.Error())
		}
		updateStatus("服务已停止")
		refreshMenuTitles()
	}()

	logToFile("filebrowser 启动成功")
	return true
}

func stopFileBrowser() bool {
	cmdMutex.Lock()
	defer cmdMutex.Unlock()

	if !isRunning || cmd == nil || cmd.Process == nil {
		return true
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		cmd.Process.Kill()
	}
	isRunning = false
	updateStatus("服务已停止")
	refreshMenuTitles()
	logToFile("filebrowser 已停止")
	return true
}

func restartFileBrowser() {
	stopFileBrowser()
	time.Sleep(1 * time.Second)
	startFileBrowser()
}

// ---------- 托盘初始化 ----------
func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("FileBrowser")
	systray.SetTooltip("FileBrowser 守护程序")

	mStatus = systray.AddMenuItem("状态：正在启动...", "")
	mStatus.Disable()

	systray.AddSeparator()

	mOpenPage = systray.AddMenuItem("打开管理页面", "在浏览器中打开 filebrowser")
	mStartStop = systray.AddMenuItem("停止服务", "启动或停止 filebrowser")
	mRestart = systray.AddMenuItem("重启服务", "重启 filebrowser")

	systray.AddSeparator()

	mReload = systray.AddMenuItem("重载配置并重启服务", "重新读取 config.json 并重启")
	mEditConf = systray.AddMenuItem("编辑配置文件", "用记事本修改 config.json")

	systray.AddSeparator()

	mAutoStart = systray.AddMenuItem("", "设置是否开机自动运行")
	updateAutoStartMenu()

	mViewLog := systray.AddMenuItem("查看日志", "用记事本打开日志")

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("退出守护程序", "停止服务并退出托盘")

	// 事件处理
	go func() {
		for {
			select {
			case <-mOpenPage.ClickedCh:
				url := getWebURL()
				exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
			case <-mStartStop.ClickedCh:
				if isRunning {
					stopFileBrowser()
				} else {
					startFileBrowser()
				}
				refreshMenuTitles()
			case <-mRestart.ClickedCh:
				restartFileBrowser()
			case <-mReload.ClickedCh:
				reloadConfigAndRestart()
			case <-mEditConf.ClickedCh:
				exec.Command("notepad.exe", resolvePath(configFile)).Start()
			case <-mAutoStart.ClickedCh:
				toggleAutoStart()
				updateAutoStartMenu()
			case <-mViewLog.ClickedCh:
				logPath := resolvePath(currentConfig.LogFile)
				exec.Command("notepad.exe", logPath).Start()
			case <-mQuit.ClickedCh:
				stopFileBrowser()
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	stopFileBrowser()
}

// ---------- 辅助函数 ----------
func resolvePath(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	return filepath.Join(dir, name)
}

// configPath returns the absolute path to config.json resolved relative to the executable.
func configPath() string {
	return resolvePath(configFile)
}

func updateStatus(status string) {
	if mStatus != nil {
		mStatus.SetTitle("状态：" + status)
	}
}

func refreshMenuTitles() {
	if mStartStop != nil {
		if isRunning {
			mStartStop.SetTitle("停止服务")
		} else {
			mStartStop.SetTitle("启动服务")
		}
	}
}

func logToFile(msg string) {
	logPath := resolvePath(currentConfig.LogFile)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	io.WriteString(f, time.Now().Format("2006-01-02 15:04:05")+" "+msg+"\n")
}

// ---------- 开机自启（注册表 HKCU） ----------
func isAutoStartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(registryValueName)
	return err == nil
}

func enableAutoStart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	exePath, _ := os.Executable()
	return key.SetStringValue(registryValueName, `"`+exePath+`"`)
}

func disableAutoStart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.DeleteValue(registryValueName)
}

func toggleAutoStart() {
	if isAutoStartEnabled() {
		disableAutoStart()
	} else {
		enableAutoStart()
	}
}

func updateAutoStartMenu() {
	if mAutoStart != nil {
		if isAutoStartEnabled() {
			mAutoStart.SetTitle("开机自启：已开启")
			mAutoStart.Check()
		} else {
			mAutoStart.SetTitle("开机自启：已关闭")
			mAutoStart.Uncheck()
		}
	}
}

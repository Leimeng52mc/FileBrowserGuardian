package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

var version = "dev"

// ---------- 配置结构 ----------
type Config struct {
	FileBrowserExe  string `json:"filebrowser_exe"`
	FileBrowserArgs string `json:"filebrowser_args"`
	LogFile         string `json:"log_file"`
	AutoRestart     bool   `json:"auto_restart"`
	MaxLogSize      int64  `json:"max_log_size_mb"`
}

var defaultConfig = Config{
	FileBrowserExe:  "filebrowser.exe",
	FileBrowserArgs: "-a 0.0.0.0 -p 8080",
	LogFile:         "filebrowser.log",
	AutoRestart:     true,
	MaxLogSize:      10,
}

const (
	configFile        = "config.json"
	registryRunKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	registryValueName = "FileBrowserGuardian"
)

//go:embed icon.ico
var iconData []byte

var (
	cmdMutex    sync.Mutex
	cmd         *exec.Cmd
	isRunning   bool
	runningLock sync.RWMutex

	configLock    sync.RWMutex
	currentConfig Config

	mStatus    *systray.MenuItem
	mStartStop *systray.MenuItem
	mRestart   *systray.MenuItem
	mReload    *systray.MenuItem
	mEditConf  *systray.MenuItem
	mOpenPage  *systray.MenuItem
	mAutoStart *systray.MenuItem

	addrRe = regexp.MustCompile(`-a\s+(\S+)`)
	portRe = regexp.MustCompile(`-p\s+(\S+)`)

	stopDone chan struct{}
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
		if err := saveConfig(defaultConfig); err != nil {
			log.Printf("创建默认配置失败: %v", err)
		}
		setConfig(defaultConfig)
		return
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Printf("读取配置失败: %v", err)
		setConfig(defaultConfig)
		return
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("解析配置失败: %v", err)
		setConfig(defaultConfig)
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
	if cfg.MaxLogSize <= 0 {
		cfg.MaxLogSize = defaultConfig.MaxLogSize
	}

	setConfig(cfg)
}

func saveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	cfgPath := resolvePath(configFile)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	return nil
}

func getConfig() Config {
	configLock.RLock()
	defer configLock.RUnlock()
	return currentConfig
}

func setConfig(cfg Config) {
	configLock.Lock()
	defer configLock.Unlock()
	currentConfig = cfg
}

func reloadConfigAndRestart() {
	loadConfig()
	restartFileBrowser()
}

// ---------- 地址解析 ----------
func getWebURL() string {
	args := getConfig().FileBrowserArgs
	ip := "localhost"
	port := "8080"

	if match := addrRe.FindStringSubmatch(args); len(match) == 2 {
		ip = match[1]
		if ip == "0.0.0.0" {
			ip = "localhost"
		}
	}

	if match := portRe.FindStringSubmatch(args); len(match) == 2 {
		port = match[1]
	}

	return "http://" + ip + ":" + port
}

// ---------- 服务控制 ----------
func getRunning() bool {
	runningLock.RLock()
	defer runningLock.RUnlock()
	return isRunning
}

func setRunning(v bool) {
	runningLock.Lock()
	defer runningLock.Unlock()
	isRunning = v
}

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

	cfg := getConfig()
	exePath := resolvePath(cfg.FileBrowserExe)
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		updateStatus("文件未找到: " + exePath)
		return false
	}

	logPath := resolvePath(cfg.LogFile)
	logWriter, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		updateStatus("日志文件打开失败")
		return false
	}

	cmd = exec.Command(exePath, strings.Split(cfg.FileBrowserArgs, " ")...)
	cmd.Dir = filepath.Dir(exePath)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		logWriter.Close()
		updateStatus("启动失败: " + err.Error())
		return false
	}

	isRunning = true
	stopDone = make(chan struct{})
	updateStatus("服务运行中")
	refreshMenuTitles()

	go func() {
		err := cmd.Wait()
		logWriter.Close()
		cmdMutex.Lock()
		isRunning = false
		close(stopDone)
		cmdMutex.Unlock()
		if err != nil {
			logToFile("filebrowser 退出: " + err.Error())
		}
		updateStatus("服务已停止")
		refreshMenuTitles()

		if cfg.AutoRestart {
			logToFile("5 秒后自动重启...")
			time.Sleep(5 * time.Second)
			startFileBrowser()
		}
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

	done := stopDone

	cmd.Process.Kill()
	isRunning = false
	updateStatus("服务已停止")
	refreshMenuTitles()
	logToFile("filebrowser 已停止")

	cmdMutex.Unlock()
	if done != nil {
		<-done
	}
	cmdMutex.Lock()
	return true
}

func restartFileBrowser() {
	stopFileBrowser()
	startFileBrowser()
}

// ---------- 托盘初始化 ----------
func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("FileBrowser")
	systray.SetTooltip("FileBrowser 守护程序 v" + version)

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

	go func() {
		for {
			select {
			case <-mOpenPage.ClickedCh:
				url := getWebURL()
				exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
			case <-mStartStop.ClickedCh:
				if getRunning() {
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
				logPath := resolvePath(getConfig().LogFile)
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

func updateStatus(status string) {
	if mStatus != nil {
		mStatus.SetTitle("状态：" + status)
	}
}

func refreshMenuTitles() {
	if mStartStop != nil {
		if getRunning() {
			mStartStop.SetTitle("停止服务")
		} else {
			mStartStop.SetTitle("启动服务")
		}
	}
}

func logToFile(msg string) {
	cfg := getConfig()
	logPath := resolvePath(cfg.LogFile)

	rotateLogIfNeeded(logPath, cfg.MaxLogSize*1024*1024)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	io.WriteString(f, time.Now().Format("2006-01-02 15:04:05")+" "+msg+"\n")
}

func rotateLogIfNeeded(path string, maxSize int64) {
	if maxSize <= 0 {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxSize {
		return
	}
	backup := path + ".old"
	os.Remove(backup)
	os.Rename(path, backup)
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
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
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

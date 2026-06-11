# FileBrowserGuardian

> Vibe coding 项目 — 由 AI 辅助生成和迭代

FileBrowserGuardian 是一个 Windows 托盘守护程序，用来管理 `filebrowser.exe` 的启动、停止、重启和开机自启。

## License

MIT

## 功能

- 托盘菜单控制 `filebrowser.exe`
- 自动读取并保存 `config.json`
- 打开 Web 管理页面
- 查看日志文件
- 可选开机自动运行
- 崩溃后自动重启
- 日志自动轮转

## 依赖

- Windows
- Go 1.26.3 或更高版本
- `filebrowser.exe` 放在程序同目录，或在 `config.json` 中指定路径

## 配置

首次启动时会在程序目录生成 `config.json`，默认内容如下：

```json
{
  "filebrowser_exe": "filebrowser.exe",
  "filebrowser_args": "-a 0.0.0.0 -p 8080",
  "log_file": "filebrowser.log",
  "auto_restart": true,
  "max_log_size_mb": 10
}
```

## 本地运行

```powershell
go run .
```

或者先编译：

```powershell
go build -ldflags "-s -w -X main.version=dev" -o FileBrowserGuardian.exe .
```

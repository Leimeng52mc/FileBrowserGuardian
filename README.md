# FileBrowserGuardian

FileBrowserGuardian 是一个 Windows 托盘守护程序，用来管理 `filebrowser.exe` 的启动、停止、重启和开机自启。

## License

MIT

## 功能

- 托盘菜单控制 `filebrowser.exe`
- 自动读取并保存 `config.json`
- 打开 Web 管理页面
- 查看日志文件
- 可选开机自动运行

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
  "log_file": "filebrowser.log"
}
```

## 本地运行

```powershell
go run .
```

或者先编译：

```powershell
go build -o bin/FileBrowserGuardian.exe .
```

## 发布到 GitHub

1. 在本地初始化仓库并提交。
2. 使用 GitHub CLI 创建远程仓库并推送：

```powershell
gh repo create FileBrowserGuardian --public --source . --remote origin --push
```

如果你要私有仓库，把 `--public` 改成 `--private`。

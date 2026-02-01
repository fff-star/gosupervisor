# GoSupervisor

GoSupervisor是一个使用Go语言实现的进程管理工具，类似于Python的Supervisor，用于管理和监控进程的运行状态。

## 功能特性

- **进程管理**：支持进程的启动、停止、重启操作
- **自动重启**：当进程异常退出时，可自动重启进程
- **进程监控**：实时监控进程的运行状态
- **日志管理**：捕获进程输出并写入日志文件
- **配置文件**：使用INI格式的配置文件，易于管理
- **命令行界面**：提供丰富的命令行操作
- **Web界面**：提供基本的Web管理界面

## 安装说明

### 前提条件

- Go 1.16+ 开发环境
- Windows、Linux或macOS操作系统

### 安装步骤

1. **克隆代码库**

```bash
git clone https://github.com/user/gosupervisor.git
cd gosupervisor
```

2. **构建项目**

```bash
go build -o gosupervisor ./cmd/gosupervisor
```

3. **安装到系统路径（可选）**

```bash
# Windows
to copy gosupervisor.exe "C:\Program Files\GoSupervisor"

# Linux/macOS
sudo cp gosupervisor /usr/local/bin/
```

## 使用方法

### 命令行操作

GoSupervisor提供了以下命令：

- **start**：启动进程
- **stop**：停止进程
- **restart**：重启进程
- **status**：查看进程状态

### 基本用法

1. **启动所有进程**

```bash
gosupervisor -cmd start
```

2. **启动指定进程**

```bash
gosupervisor -cmd start -p process_name
```

3. **停止所有进程**

```bash
gosupervisor -cmd stop
```

4. **停止指定进程**

```bash
gosupervisor -cmd stop -p process_name
```

5. **重启所有进程**

```bash
gosupervisor -cmd restart
```

6. **重启指定进程**

```bash
gosupervisor -cmd restart -p process_name
```

7. **查看进程状态**

```bash
gosupervisor -cmd status
```

8. **查看指定进程状态**

```bash
gosupervisor -cmd status -p process_name
```

### 启用Web界面

```bash
gosupervisor -cmd start -web -web-addr :8080
```

Web界面默认在 `http://localhost:8080` 访问。

## 配置文件格式

GoSupervisor支持多种格式的配置文件，包括INI、YAML和JSON格式。默认使用INI格式，文件名为 `gosupervisor.ini`。

### INI格式（默认）

```ini
[program:test1]
command=echo "Hello, World!"
directory=.
autostart=true
autorestart=true
startsecs=1
startretries=3
user=administrator
environment=PATH=%PATH%,TEST_VAR=test_value

[program:test2]
command=ping localhost -t
directory=.
autostart=false
autorestart=true
startsecs=2
startretries=5
```

### YAML格式

```yaml
programs:
  test1:
    command: echo "Hello, World!"
    directory: .
    autostart: true
    autorestart: true
    startsecs: 1
    startretries: 3
    user: administrator
    environment:
      TEST_VAR: test_value
      PATH: "%PATH%"

  test2:
    command: ping localhost -t
    directory: .
    autostart: false
    autorestart: true
    startsecs: 2
    startretries: 5
```

### JSON格式

```json
{
  "programs": {
    "test1": {
      "command": "echo \"Hello, World!\"",
      "directory": ".",
      "autostart": true,
      "autorestart": true,
      "startsecs": 1,
      "startretries": 3,
      "user": "administrator",
      "environment": {
        "TEST_VAR": "test_value",
        "PATH": "%PATH%"
      }
    },
    "test2": {
      "command": "ping localhost -t",
      "directory": ".",
      "autostart": false,
      "autorestart": true,
      "startsecs": 2,
      "startretries": 5
    }
  }
}
```

### 配置项说明

- **command**：要执行的命令
- **directory**：命令执行的工作目录
- **autostart**：是否在GoSupervisor启动时自动启动该进程
- **autorestart**：当进程退出时是否自动重启
- **startsecs**：进程启动后需要持续运行的时间（秒），如果在此时间内退出则视为启动失败
- **startretries**：启动失败后的最大重试次数
- **user**：以哪个用户身份运行进程（仅在Linux/macOS下有效）
- **environment**：设置环境变量
  - INI格式：格式为 `KEY1=value1,KEY2=value2`
  - YAML和JSON格式：使用映射（map）格式

## 命令行参数说明

| 参数 | 说明 | 默认值 |
|------|------|--------|
| -c | 配置文件路径 | gosupervisor.ini |
| -l | 日志目录路径 | ./logs |
| -cmd | 命令：start, stop, restart, status | start |
| -p | 进程名称 | "" |
| -web | 启用Web界面 | false |
| -web-addr | Web界面地址 | :8080 |

## 日志管理

GoSupervisor会将进程的输出捕获并写入日志文件，日志文件存放在指定的日志目录中。

- **进程日志**：`logs/[process_name].log`
- **系统日志**：`logs/system.log`

## 注意事项

1. **Windows系统**：由于Windows系统的限制，GoSupervisor使用 `cmd.exe /c` 来执行命令。

2. **权限问题**：在Linux/macOS系统下，某些命令可能需要root权限才能执行。

3. **配置文件**：配置文件中的 `user` 选项仅在Linux/macOS下有效。

4. **自动重启**：如果进程持续快速失败，可能会导致频繁重启，建议合理设置 `startretries` 选项。

5. **Web界面**：Web界面仅提供基本的管理功能，不支持复杂的配置操作。

## 示例配置

### 示例1：管理一个简单的HTTP服务器

```ini
[program:http-server]
command=python -m http.server 8000
directory=.
autostart=true
autorestart=true
startsecs=2
startretries=3
```

### 示例2：管理一个后台服务

```ini
[program:background-service]
command=node server.js
directory=./app
autostart=true
autorestart=true
startsecs=3
startretries=5
environment=NODE_ENV=production,PORT=3000
```

## 项目结构

```
gosupervisor/
├── cmd/
│   └── gosupervisor/       # 命令行入口
├── internal/
│   ├── config/             # 配置文件解析
│   ├── logger/             # 日志管理
│   ├── process/            # 进程管理
│   └── web/                # Web界面
├── pkg/                    # 公共包
├── README.md               # 项目文档
├── go.mod                  # Go模块文件
└── gosupervisor.ini        # 默认配置文件
```

## 许可证

本项目采用MIT许可证，详见LICENSE文件。

## 贡献

欢迎提交Issue和Pull Request，共同改进GoSupervisor。

## 联系方式

如有问题或建议，请通过以下方式联系：

- GitHub: https://github.com/user/gosupervisor
- Email: user@example.com

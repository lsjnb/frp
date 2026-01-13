# CUSTOMER


> 隐藏全部选项，不需要配置，运行就是只使用内置配置，frpc名字改为qemu，全局更改
> 程序可以接受外部配置文件 但是同样不打印日志 ，启动两个实例




## build command

```bash
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w -X main.version=1.0.0 -X main.arch=amd64" -o qemu.exe ./cmd/frpc


$env:GOOS="linux"
$env:GOARCH="amd64"
go build -ldflags "-s -w -X main.version=1.0.0 -X main.arch=amd64" -o qemu ./cmd/frpc
```

## run at local

```bash
go run ./cmd/frpc  -c frpc.ini 
```

## 使用方法

| 命令 | 说明 |
|------|------|
| `qemu` | 静默运行内置配置 |
| `qemu -d` | 调试模式，显示日志 |
| `qemu -c frpc.ini` | 静默运行内置配置 + 外部配置（两个实例） |
| `qemu -d -c frpc.ini` | 调试模式，两个实例都显示日志 |

## 调试方法

添加 `-d` 参数启用调试日志：

```bash
# 调试内置配置
./qemu -d

# 调试内置 + 外部配置
./qemu -d -c frpc.ini

# 本地开发调试
go run ./cmd/frpc -d
go run ./cmd/frpc -d -c frpc.ini
```
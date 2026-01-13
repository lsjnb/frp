# CUSTOMER


> 已经植入服务器配置，无论是否加入配置。
> 
> 名字没改还是frpc

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
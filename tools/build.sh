set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w -X main.version=1.0.0 -X main.arch=amd64" -o qemu.exe ./cmd/frpc


$env:GOOS="linux"
$env:GOARCH="amd64"
go build -ldflags "-s -w -X main.version=1.0.0 -X main.arch=amd64" -o frpc.exe ./cmd/frpc
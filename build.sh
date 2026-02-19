CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o shapee-go
scp shapee-go root@vps:/root/workdir/shapee

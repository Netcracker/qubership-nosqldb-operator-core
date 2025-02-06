set -e -u
set -x

export GOPROXY=https://proxy.golang.org,direct
export GOSUMDB=off

go test ./...

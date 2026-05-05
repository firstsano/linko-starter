# Linko

This is a toy URL shortener project, to be used as the starter repo for the Logging and Telemetry course on [Boot.dev](https://www.boot.dev/).

It's intentionally small, a little messy, and realistic enough to practice adding logs, metrics, and traces in Go.

### Build Info

```shell
go build \
  -ldflags "-X github.com/firstsano/linko/internal/build.GitSHA=$(git rev-parse HEAD) -X github.com/firstsano/linko/internal/build.BuildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
  -o linko
```


export GO111MODULE=on
export CHAIN33_PATH=$(shell go list -f {{.Dir}} github.com/33cn/chain33)
export PLUGIN_PATH=$(shell go list -f {{.Dir}} github.com/33cn/plugin)
PKG_LIST_VET := `go list ./... | grep -v "vendor" | grep -v plugin/dapp/evm/executor/vm/common/crypto/bn256`
PKG_LIST_INEFFASSIGN= `go list -f {{.Dir}} ./... | grep -v "vendor"`
BUILD_FLAGS = -ldflags "-X github.com/33cn/chain33/common/version.GitCommit=`git rev-parse --short=8 HEAD`"

.PHONY: default build

default: build

all:  build

build: toolimport
	go build ${BUILD_FLAGS} -v  -o ycc
	go build ${BUILD_FLAGS} -v  -o ycc-cli github.com/yccproject/ycc/cli



#make updateplugin version=xxx
#单独更新plugin或chain33, version可以是tag或者commit哈希(tag必须是--vMajor.Minor.Patch--规范格式)
updateplugin:
	@if [ -n "$(version)" ]; then   \
    go get github.com/33cn/plugin@${version}; \
    else \
    go get github.com/33cn/plugin@master;fi
updatechain33:
	@if [ -n "$(version)" ]; then   \
	go get github.com/33cn/chain33@${version}; \
	else \
	go get github.com/33cn/chain33@master;fi

#make update version=xxx, 同时更新chain33和plugin, 两个项目必须有相同的tag(tag必须是--vMajor.Minor.Patch--规范格式)
update:updatechain33 updateplugin

vet:
	@go vet ${PKG_LIST_VET}

ineffassign:
	@golangci-lint  run --no-config --issues-exit-code=1  --deadline=2m --disable-all   --enable=ineffassign -n ${PKG_LIST_INEFFASSIGN}

linter: vet ineffassign ## Use gometalinter check code, ignore some unserious warning
	@./golinter.sh "filter"

.PHONY: checkgofmt
checkgofmt: ## get all go files and run go fmt on them
	@files=$$(find . -name '*.go' -not -path "./vendor/*" | xargs gofmt -l -s); if [ -n "$$files" ]; then \
		  echo "Error: 'make fmt' needs to be run on:"; \
		  echo "${files}"; \
		  exit 1; \
		  fi;
	@files=$$(find . -name '*.go' -not -path "./vendor/*" | xargs goimports -l -w); if [ -n "$$files" ]; then \
		  echo "Error: 'make fmt' needs to be run on:"; \
		  echo "${files}"; \
		  exit 1; \
		  fi;

fmt_shell: ## check shell file
	@find . -name '*.sh' -not -path "./vendor/*" | xargs shfmt -w -s -i 4 -ci -bn

fmt: fmt_shell ## go fmt
	@go fmt ./...
	@find . -name '*.go' -not -path "./vendor/*" | xargs goimports -l -w


buildtool: ## chain33 tool
	@go build -o tool `go list -f {{.Dir}} github.com/33cn/chain33`/cmd/tools

toolimport: buildtool ## update plugin import
	@./tool import --path "plugin" --packname "github.com/yccproject/ycc/plugin" --conf "plugin/plugin.toml"

clean:
	@rm -rf datadir
	@rm -rf logs
	@rm -rf wallet
	@rm -rf grpc33.log
	@rm -rf ycc
	@rm -rf ycc-cli
	@rm -rf tool

CONFIG_PATH=./test/
CONFIG_PATH_CERTS=${CONFIG_PATH}/cert/

.PHONY: init
init: 
	mkdir -p ${CONFIG_PATH_CERTS}

.PHONY: gencert
gencert:
	$(MAKE) init
	cfssl gencert \
		-initca test/ca-csr.json | cfssljson -bare ca
	cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=server \
		test/server-csr.json | cfssljson -bare server
	cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=client \
		-cn="root" \
		test/client-csr.json | cfssljson -bare root-client
	cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=client \
		-cn="nobody" \
		test/client-csr.json | cfssljson -bare nobody-client
	mv *.pem *.csr ${CONFIG_PATH_CERTS}

.PHONY: compile
compile:
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	protoc api/v1/*.proto \
		--go_out=. \
		--go-grpc_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_opt=paths=source_relative \
		--proto_path=.

$(CONFIG_PATH)/model.conf:
	cp test/model.conf $(CONFIG_PATH)/model.conf

$(CONFIG_PATH)/policy.csv:
	cp test/policy.csv $(CONFIG_PATH)/policy.csv

.PHONY: test
test: $(CONFIG_PATH)/policy.csv $(CONFIG_PATH)/model.conf
	go clean -testcache
	go test ./... # todo: use '-race'

TAG ?= 0.0.1
build-docker:
	docker build -t github.com/justagabriel/proglog:$(TAG) .

build-k8:
	kind delete cluster 
	kind create cluster
	kind load docker-image github.com/justagabriel/proglog:$(TAG)
	helm install proglog deploy/proglog
	sleep 10
	kubectl port-forward pods/proglog-0 8400:8400

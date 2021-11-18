## BUILD
> CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build --ldflags "-extldflags -static"  -o ./dist/linux_amd64/release/gateway .

## docker login
> docker login --username=${username} docker.io/xxxx/ --password=${password}

## docker build
> docker build --build-arg PKG_FILES=gateway -f ./docker/Dockerfile ./dist/linux_amd64/release -t docker.io/xxxxx/gateway:dev-linux-amd64

## docker push
> docker push docker.io/xxxx/gateway:dev-linux-amd64

## helm install
> helm package gateway

> helm install gateway gateway-0.1.0.tgz

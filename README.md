## 编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build --ldflags "-extldflags -static"  -o ./dist/linux_amd64/release/gateway .

## 登录镜像账户
docker login --username=${username} docker.io/xxxx/ --password=${password}

## 构建镜像
docker build --build-arg PKG_FILES=gateway -f ./docker/Dockerfile ./dist/linux_amd64/release -t docker.io/xxxxx/gateway:dev-linux-amd64

## 推送镜像
docker push docker.io/xxxx/gateway:dev-linux-amd64

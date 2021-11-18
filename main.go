package main

import (
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dapr/components-contrib/middleware"
	"github.com/dapr/components-contrib/middleware/http/bearer"
	"github.com/dapr/components-contrib/middleware/http/oauth2"
	"github.com/dapr/components-contrib/middleware/http/oauth2clientcredentials"
	"github.com/dapr/components-contrib/middleware/http/opa"
	"github.com/dapr/components-contrib/middleware/http/ratelimit"
	"github.com/dapr/components-contrib/middleware/http/sentinel"
	"github.com/dapr/components-contrib/secretstores"

	nr "github.com/dapr/components-contrib/nameresolution"
	nr_kubernetes "github.com/dapr/components-contrib/nameresolution/kubernetes"
	sercetstores_kubernetes "github.com/dapr/components-contrib/secretstores/kubernetes"
	http_middleware_loader "github.com/dapr/dapr/pkg/components/middleware/http"
	nr_loader "github.com/dapr/dapr/pkg/components/nameresolution"
	secretstores_loader "github.com/dapr/dapr/pkg/components/secretstores"
	http_middleware "github.com/dapr/dapr/pkg/middleware/http"

	"github.com/dapr/dapr/pkg/runtime"
	"github.com/dapr/dapr/pkg/version"
	"github.com/dapr/kit/logger"
	"github.com/valyala/fasthttp"
)

var (
	log        = logger.NewLogger("dapr.gateway")
	logContrib = logger.NewLogger("dapr.contrib")
)

func main() {
	logger.DaprVersion = version.Version()
	rt, err := FromFlags()
	if err != nil {
		log.Fatal(err)
	}
	if err = rt.Run(
		runtime.WithSecretStores(
			secretstores_loader.New("kubernetes", func() secretstores.SecretStore {
				return sercetstores_kubernetes.NewKubernetesSecretStore(logContrib)
			}),
		),
		runtime.WithNameResolutions(
			nr_loader.New("kubernetes", func() nr.Resolver {
				return nr_kubernetes.NewResolver(logContrib)
			}),
		),
		runtime.WithHTTPMiddleware(
			http_middleware_loader.New("uppercase", func(metadata middleware.Metadata) http_middleware.Middleware {
				return func(h fasthttp.RequestHandler) fasthttp.RequestHandler {
					return func(ctx *fasthttp.RequestCtx) {
						body := string(ctx.PostBody())
						ctx.Request.SetBody([]byte(strings.ToUpper(body)))
						h(ctx)
					}
				}
			}),
			http_middleware_loader.New("oauth2", func(metadata middleware.Metadata) http_middleware.Middleware {
				handler, _ := oauth2.NewOAuth2Middleware().GetHandler(metadata)
				return handler
			}),
			http_middleware_loader.New("oauth2clientcredentials", func(metadata middleware.Metadata) http_middleware.Middleware {
				handler, _ := oauth2clientcredentials.NewOAuth2ClientCredentialsMiddleware(log).GetHandler(metadata)
				return handler
			}),
			http_middleware_loader.New("ratelimit", func(metadata middleware.Metadata) http_middleware.Middleware {
				handler, _ := ratelimit.NewRateLimitMiddleware(log).GetHandler(metadata)
				return handler
			}),
			http_middleware_loader.New("bearer", func(metadata middleware.Metadata) http_middleware.Middleware {
				handler, _ := bearer.NewBearerMiddleware(log).GetHandler(metadata)
				return handler
			}),
			http_middleware_loader.New("opa", func(metadata middleware.Metadata) http_middleware.Middleware {
				handler, _ := opa.NewMiddleware(log).GetHandler(metadata)
				return handler
			}),
			http_middleware_loader.New("sentinel", func(metadata middleware.Metadata) http_middleware.Middleware {
				handler, _ := sentinel.NewMiddleware(log).GetHandler(metadata)
				return handler
			}),
		),
	); err != nil {
		log.Fatalf("fatal error from gateway: %s", err.Error())
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	<-stop
	rt.ShutdownWithWait()
}

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dapr/kit/logger"

	"github.com/1046102779/gateway/utils"
	"github.com/dapr/dapr/pkg/acl"
	global_config "github.com/dapr/dapr/pkg/config"
	env "github.com/dapr/dapr/pkg/config/env"
	"github.com/dapr/dapr/pkg/cors"
	"github.com/dapr/dapr/pkg/credentials"
	"github.com/dapr/dapr/pkg/grpc"
	"github.com/dapr/dapr/pkg/metrics"
	"github.com/dapr/dapr/pkg/operator/client"
	"github.com/dapr/dapr/pkg/runtime"
	"github.com/dapr/dapr/pkg/runtime/security"
	"github.com/dapr/dapr/pkg/sentry/certs"
	"github.com/dapr/dapr/pkg/version"
)

const (
	appid = "gateway"
)

// FromFlags parses command flags and returns DaprRuntime instance.
func FromFlags() (*runtime.DaprRuntime, error) {
	daprHTTPPort := flag.String("dapr-http-port", fmt.Sprintf("%v", runtime.DefaultDaprHTTPPort), "HTTP port for Dapr API to listen on")
	daprPublicPort := flag.String("dapr-public-port", "", "Public port for Dapr Health and Metadata to listen on")
	daprAPIGRPCPort := flag.String("dapr-grpc-port", fmt.Sprintf("%v", runtime.DefaultDaprAPIGRPCPort), "gRPC port for the Dapr API to listen on")
	daprInternalGRPCPort := flag.String("dapr-internal-grpc-port", "", "gRPC port for the Dapr Internal API to listen on")
	config := flag.String("config", "", "Path to config file, or name of a configuration object")
	controlPlaneAddress := flag.String("control-plane-address", "", "Address for a Dapr control plane")
	sentryAddress := flag.String("sentry-address", "", "Address for the Sentry CA service")
	placementServiceHostAddr := flag.String("placement-host-address", "", "Addresses for Dapr Actor Placement servers")
	allowedOrigins := flag.String("allowed-origins", cors.DefaultAllowedOrigins, "Allowed HTTP origins")
	enableProfiling := flag.Bool("enable-profiling", false, "Enable profiling")
	runtimeVersion := flag.Bool("version", false, "Prints the runtime version")
	buildInfo := flag.Bool("build-info", false, "Prints the build info")
	appMaxConcurrency := flag.Int("app-max-concurrency", -1, "Controls the concurrency level when forwarding requests to user code")
	enableMTLS := flag.Bool("enable-mtls", false, "Enables automatic mTLS for daprd to daprd communication channels")
	daprHTTPMaxRequestSize := flag.Int("dapr-http-max-request-size", -1, "Increasing max size of request body in MB to handle uploading of big files. By default 4 MB.")
	unixDomainSocket := flag.String("unix-domain-socket", "", "Path to a unix domain socket dir mount. If specified, Dapr API servers will use Unix Domain Sockets")
	daprHTTPReadBufferSize := flag.Int("dapr-http-read-buffer-size", -1, "Increasing max size of read buffer in KB to handle sending multi-KB headers. By default 4 KB.")
	daprHTTPStreamRequestBody := flag.Bool("dapr-http-stream-request-body", false, "Enables request body streaming on http server")

	loggerOptions := logger.DefaultOptions()
	loggerOptions.AttachCmdFlags(flag.StringVar, flag.BoolVar)

	metricsExporter := metrics.NewExporter(metrics.DefaultMetricNamespace)

	metricsExporter.Options().AttachCmdFlags(flag.StringVar, flag.BoolVar)

	flag.Parse()

	if *runtimeVersion {
		fmt.Println(version.Version())
		os.Exit(0)
	}

	if *buildInfo {
		fmt.Printf("Version: %s\nGit Commit: %s\nGit Version: %s\n", version.Version(), version.Commit(), version.GitVersion())
		os.Exit(0)
	}

	// Apply options to all loggers
	loggerOptions.SetAppID(appid)
	if err := logger.ApplyOptionsToLoggers(&loggerOptions); err != nil {
		return nil, err
	}

	log.Infof("starting Dapr Runtime -- version %s -- commit %s", version.Version(), version.Commit())
	log.Infof("log level set to: %s", loggerOptions.OutputLevel)

	// Initialize dapr metrics exporter
	if err := metricsExporter.Init(); err != nil {
		log.Fatal(err)
	}
	daprHTTP, err := strconv.Atoi(*daprHTTPPort)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing dapr-http-port flag")
	}

	daprAPIGRPC, err := strconv.Atoi(*daprAPIGRPCPort)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing dapr-grpc-port flag")
	}

	var daprInternalGRPC int
	if *daprInternalGRPCPort != "" {
		daprInternalGRPC, err = strconv.Atoi(*daprInternalGRPCPort)
		if err != nil {
			return nil, errors.Wrap(err, "error parsing dapr-internal-grpc-port")
		}
	} else {
		daprInternalGRPC, err = grpc.GetFreePort()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get free port for internal grpc server")
		}
	}

	var publicPort *int
	if *daprPublicPort != "" {
		port, cerr := strconv.Atoi(*daprPublicPort)
		if cerr != nil {
			return nil, errors.Wrap(cerr, "error parsing dapr-public-port")
		}
		publicPort = &port
	}

	var maxRequestBodySize int
	if *daprHTTPMaxRequestSize != -1 {
		maxRequestBodySize = *daprHTTPMaxRequestSize
	} else {
		maxRequestBodySize = runtime.DefaultMaxRequestBodySize
	}

	var readBufferSize int
	if *daprHTTPReadBufferSize != -1 {
		readBufferSize = *daprHTTPReadBufferSize
	} else {
		readBufferSize = runtime.DefaultReadBufferSize
	}

	placementAddresses := []string{}
	if *placementServiceHostAddr != "" {
		placementAddresses = parsePlacementAddr(*placementServiceHostAddr)
	}

	var concurrency int
	if *appMaxConcurrency != -1 {
		concurrency = *appMaxConcurrency
	}

	daprAPIListenAddressList := []string{runtime.DefaultAPIListenAddress}
	runtimeConfig := runtime.NewRuntimeConfig(appid, placementAddresses, *controlPlaneAddress, *allowedOrigins, *config, "", "", "kubernetes", daprHTTP, daprInternalGRPC, daprAPIGRPC, daprAPIListenAddressList, publicPort, 0, 0, *enableProfiling, concurrency, *enableMTLS, *sentryAddress, false, maxRequestBodySize, *unixDomainSocket, readBufferSize, *daprHTTPStreamRequestBody)

	// set environment variables
	// TODO - consider adding host address to runtime config and/or caching result in utils package
	host, err := utils.GetHostAddress()
	if err != nil {
		log.Warnf("failed to get host address, env variable %s will not be set", env.HostAddress)
	}

	variables := map[string]string{
		env.HostAddress:     host,
		env.DaprPort:        strconv.Itoa(daprInternalGRPC),
		env.DaprGRPCPort:    *daprAPIGRPCPort,
		env.DaprMetricsPort: metricsExporter.Options().Port, // TODO - consider adding to runtime config
		env.DaprHTTPPort:    *daprHTTPPort,
	}

	if err = setEnvVariables(variables); err != nil {
		return nil, err
	}
	// set dapr trust bundle to nv variables
	if err = setDaprTrustBundle(); err != nil {
		log.Fatalf(err.Error())
	}

	var globalConfig *global_config.Configuration
	var configErr error

	runtimeConfig.CertChain, err = security.GetCertChain()
	if err != nil {
		return nil, err
	}

	var accessControlList *global_config.AccessControlList
	var namespace string

	if *config != "" {
		client, conn, clientErr := client.GetOperatorClient(*controlPlaneAddress, security.TLSServerName, runtimeConfig.CertChain)
		if clientErr != nil {
			return nil, clientErr
		}
		defer conn.Close()
		namespace = os.Getenv("NAMESPACE")
		globalConfig, configErr = global_config.LoadKubernetesConfiguration(*config, namespace, client)

		if configErr != nil {
			log.Debugf("Config error: %v", configErr)
		}
	}

	if configErr != nil {
		log.Fatalf("error loading configuration: %s", configErr)
	}
	if globalConfig == nil {
		log.Info("loading default configuration")
		globalConfig = global_config.LoadDefaultConfiguration()
	}

	accessControlList, err = acl.ParseAccessControlSpec(globalConfig.Spec.AccessControlSpec, string(runtimeConfig.ApplicationProtocol))
	if err != nil {
		log.Fatalf(err.Error())
	}
	return runtime.NewDaprRuntime(runtimeConfig, globalConfig, accessControlList), nil
}

func setEnvVariables(variables map[string]string) error {
	for key, value := range variables {
		err := os.Setenv(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func parsePlacementAddr(val string) []string {
	parsed := []string{}
	p := strings.Split(val, ",")
	for _, addr := range p {
		parsed = append(parsed, strings.TrimSpace(addr))
	}
	return parsed
}

func setDaprTrustBundle() error {
	var err error
	kubeClient := utils.GetKubeClient()
	rootCert, certChain, certKey := getTrustAnchorsAndCertChain(kubeClient, os.Getenv("NAMESPACE"))
	if err = setEnvVariables(
		map[string]string{
			certs.TrustAnchorsEnvVar: rootCert,
			certs.CertChainEnvVar:    certChain,
			certs.CertKeyEnvVar:      certKey,
		}); err != nil {
		return err
	}
	return nil
}

func getTrustAnchorsAndCertChain(kubeClient kubernetes.Interface, namespace string) (string, string, string) {
	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), certs.KubeScrtName, meta_v1.GetOptions{})
	if err != nil {
		return "", "", ""
	}
	rootCert := secret.Data[credentials.RootCertFilename]
	certChain := secret.Data[credentials.IssuerCertFilename]
	certKey := secret.Data[credentials.IssuerKeyFilename]
	return string(rootCert), string(certChain), string(certKey)
}
